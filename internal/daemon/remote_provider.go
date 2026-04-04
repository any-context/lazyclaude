package daemon

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/any-context/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/any-context/lazyclaude/internal/core/choice"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/charmbracelet/x/ansi"
)

// Compile-time check: RemoteProvider implements SessionProvider.
var _ SessionProvider = (*RemoteProvider)(nil)

// RemoteProvider adapts a daemon ClientAPI to the SessionProvider interface.
// It maintains a local cache of sessions and buffers tool notifications
// received via SSE.
//
// For latency-sensitive operations (preview capture, scrollback, key sending),
// a forwarded tmux.Client is used directly instead of going through the
// daemon API. Session CRUD, worktree management, and messaging still use
// the daemon API.
type RemoteProvider struct {
	host       string
	conn       ConnectionManager
	tmuxClient tmux.Client // forwarded tmux socket client (nil = use daemon API)

	mu            sync.Mutex
	sessions      []SessionInfo
	notifications []*model.ToolNotification

	cancelSSE context.CancelFunc
	sseDone   chan struct{} // closed when the current SSE goroutine exits
}

// NewRemoteProvider creates a RemoteProvider for the given host.
// The ConnectionManager must already be connected or will be connected
// separately; this constructor does not initiate the connection.
func NewRemoteProvider(host string, conn ConnectionManager) *RemoteProvider {
	return &RemoteProvider{
		host: host,
		conn: conn,
	}
}

// SetTmuxClient sets the forwarded tmux.Client for direct pane operations.
// When set, CapturePreview, CaptureScrollback, HistorySize, and SendChoice
// use the forwarded socket directly instead of the daemon API.
// Must be called before the GUI event loop starts.
func (rp *RemoteProvider) SetTmuxClient(tc tmux.Client) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.tmuxClient = tc
}

// getTmuxClient returns the forwarded tmux.Client, or nil if not set.
func (rp *RemoteProvider) getTmuxClient() tmux.Client {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.tmuxClient
}

// StartSSE begins consuming the SSE notification stream in a background
// goroutine. Call StopSSE to cancel. If the connection is not yet ready,
// this is a no-op and can be retried later.
//
// If a previous SSE goroutine is running, it is stopped and waited on
// before starting the new one, preventing goroutine accumulation.
func (rp *RemoteProvider) StartSSE() error {
	// Stop any previous SSE goroutine and wait for it to exit.
	rp.stopAndWaitSSE()

	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("start SSE: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := client.SubscribeNotifications(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("subscribe notifications: %w", err)
	}

	done := make(chan struct{})

	rp.mu.Lock()
	rp.cancelSSE = cancel
	rp.sseDone = done
	rp.mu.Unlock()

	go func() {
		defer close(done)
		rp.consumeSSE(ch)
	}()

	return nil
}

// StopSSE cancels the SSE subscription goroutine and waits for it to exit.
func (rp *RemoteProvider) StopSSE() {
	rp.stopAndWaitSSE()
}

// stopAndWaitSSE cancels the current SSE goroutine (if any) and blocks
// until it has exited.
func (rp *RemoteProvider) stopAndWaitSSE() {
	rp.mu.Lock()
	cancel := rp.cancelSSE
	done := rp.sseDone
	rp.cancelSSE = nil
	rp.sseDone = nil
	rp.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// consumeSSE processes events from the SSE channel and updates local state.
func (rp *RemoteProvider) consumeSSE(ch <-chan NotificationEvent) {
	for ev := range ch {
		rp.handleSSEEvent(ev)
	}
}

func (rp *RemoteProvider) handleSSEEvent(ev NotificationEvent) {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	switch ev.Type {
	case EventActivity:
		for i := range rp.sessions {
			if rp.sessions[i].ID == ev.SessionID {
				rp.sessions[i].Activity = ev.Activity
				rp.sessions[i].ToolName = ev.ToolName
				break
			}
		}
	case EventToolInfo:
		if ev.ToolNotification != nil {
			rp.notifications = append(rp.notifications, ev.ToolNotification)
		}
	case EventFullSync:
		synced := make([]SessionInfo, len(ev.Sessions))
		copy(synced, ev.Sessions)
		// Tag each session with this provider's host.
		for i := range synced {
			synced[i].Host = rp.host
		}
		rp.sessions = synced
	}
}

// --- SessionLister ---

func (rp *RemoteProvider) HasSession(sessionID string) bool {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	for _, s := range rp.sessions {
		if s.ID == sessionID {
			return true
		}
	}
	return false
}

func (rp *RemoteProvider) Host() string {
	return rp.host
}

func (rp *RemoteProvider) Sessions() ([]SessionInfo, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return nil, fmt.Errorf("sessions: %w", err)
	}
	sessions, err := client.Sessions(context.Background())
	if err != nil {
		return nil, fmt.Errorf("sessions: %w", err)
	}
	// Tag each session with this provider's host and cache.
	tagged := make([]SessionInfo, len(sessions))
	copy(tagged, sessions)
	for i := range tagged {
		tagged[i].Host = rp.host
	}
	rp.mu.Lock()
	rp.sessions = tagged
	rp.mu.Unlock()
	return tagged, nil
}

// --- SessionMutator ---

func (rp *RemoteProvider) Create(path string) error {
	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	_, err = client.CreateSession(context.Background(), SessionCreateRequest{
		Path:        path,
		SessionType: "plain",
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (rp *RemoteProvider) Delete(id string) error {
	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return client.DeleteSession(context.Background(), id)
}

func (rp *RemoteProvider) Rename(id, newName string) error {
	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return client.RenameSession(context.Background(), id, newName)
}

func (rp *RemoteProvider) PurgeOrphans() (int, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return 0, fmt.Errorf("purge orphans: %w", err)
	}
	return client.PurgeOrphans(context.Background())
}

// --- PreviewProvider ---

// resolveTmuxTarget returns the tmux target string for a session.
// Looks up the session's TmuxWindow in the local cache; falls back to
// constructing from session ID.
func (rp *RemoteProvider) resolveTmuxTarget(id string) string {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	for _, s := range rp.sessions {
		if s.ID == id && s.TmuxWindow != "" {
			return s.TmuxWindow
		}
	}
	// Fallback: construct window name from ID prefix.
	name := "lc-" + id
	if len(id) > 8 {
		name = "lc-" + id[:8]
	}
	return tmuxSessionName + ":" + name
}

func (rp *RemoteProvider) CapturePreview(id string, width, height int) (*PreviewResponse, error) {
	if tc := rp.getTmuxClient(); tc != nil {
		return rp.capturePreviewDirect(tc, id, width, height)
	}
	client, err := rp.conn.Client()
	if err != nil {
		return nil, fmt.Errorf("capture preview: %w", err)
	}
	return client.CapturePreview(context.Background(), id, width, height)
}

// capturePreviewDirect captures pane content via the forwarded tmux socket.
func (rp *RemoteProvider) capturePreviewDirect(tc tmux.Client, id string, width, height int) (*PreviewResponse, error) {
	target := rp.resolveTmuxTarget(id)
	ctx := context.Background()

	if width > 0 && height > 0 {
		_ = tc.ResizeWindow(ctx, target, width, height)
		time.Sleep(20 * time.Millisecond)
	}

	content, err := tc.CapturePaneANSI(ctx, target)
	if err != nil || width <= 0 {
		return &PreviewResponse{Content: content}, err
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}

	var cursorX, cursorY int
	if pos, posErr := tc.ShowMessage(ctx, target, "#{cursor_x},#{cursor_y}"); posErr == nil {
		parts := strings.SplitN(strings.TrimSpace(pos), ",", 2)
		if len(parts) == 2 {
			cursorX, _ = strconv.Atoi(parts[0])
			cursorY, _ = strconv.Atoi(parts[1])
		}
	}

	return &PreviewResponse{
		Content: strings.Join(lines, "\n"),
		CursorX: cursorX,
		CursorY: cursorY,
	}, nil
}

func (rp *RemoteProvider) CaptureScrollback(id string, width, startLine, endLine int) (*ScrollbackResponse, error) {
	if tc := rp.getTmuxClient(); tc != nil {
		return rp.captureScrollbackDirect(tc, id, startLine, endLine)
	}
	client, err := rp.conn.Client()
	if err != nil {
		return nil, fmt.Errorf("capture scrollback: %w", err)
	}
	return client.CaptureScrollback(context.Background(), id, width, startLine, endLine)
}

func (rp *RemoteProvider) captureScrollbackDirect(tc tmux.Client, id string, startLine, endLine int) (*ScrollbackResponse, error) {
	target := rp.resolveTmuxTarget(id)
	content, err := tc.CapturePaneANSIRange(context.Background(), target, startLine, endLine)
	return &ScrollbackResponse{Content: content}, err
}

func (rp *RemoteProvider) HistorySize(id string) (int, error) {
	if tc := rp.getTmuxClient(); tc != nil {
		return rp.historySizeDirect(tc, id)
	}
	client, err := rp.conn.Client()
	if err != nil {
		return 0, fmt.Errorf("history size: %w", err)
	}
	resp, err := client.HistorySize(context.Background(), id)
	if err != nil {
		return 0, err
	}
	return resp.Lines, nil
}

func (rp *RemoteProvider) historySizeDirect(tc tmux.Client, id string) (int, error) {
	target := rp.resolveTmuxTarget(id)
	out, err := tc.ShowMessage(context.Background(), target, "#{history_size}")
	if err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n, nil
}

// --- SessionActioner ---

// SendChoice sends a permission choice to the remote session's tmux pane.
// When tmuxClient is set, sends directly via the forwarded socket.
// Otherwise falls back to the daemon API.
func (rp *RemoteProvider) SendChoice(window string, choiceVal int) error {
	if tc := rp.getTmuxClient(); tc != nil {
		return tmuxadapter.SendToPane(context.Background(), tc, window, choice.Choice(choiceVal))
	}
	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("send choice: %w", err)
	}
	return client.SendChoice(context.Background(), "", window, choiceVal)
}

// AttachSession attaches to a remote session via SSH -t tmux attach.
// Attach always uses SSH because the user's terminal must be connected
// to the remote tmux process directly.
func (rp *RemoteProvider) AttachSession(id string) error {
	target := rp.resolveTmuxTarget(id)
	return rp.runSSHInteractive(buildTmuxAttachCommand(target))
}

// LaunchLazygit launches lazygit on the remote host via SSH -t.
// This bypasses the daemon API entirely.
func (rp *RemoteProvider) LaunchLazygit(path string) error {
	remoteCmd := fmt.Sprintf("cd %s && lazygit", posixQuote(path))
	return rp.runSSHInteractive(remoteCmd)
}

// runSSHInteractive runs an interactive SSH command with stdin/stdout/stderr
// connected to the current terminal.
func (rp *RemoteProvider) runSSHInteractive(remoteCmd string) error {
	sshHost, port := splitHostPort(rp.host)
	args := []string{"-t"}
	if port != "" {
		args = append(args, "-p", port)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(remoteCmd))
	args = append(args, sshHost, fmt.Sprintf("eval \"$(echo %s | base64 -d)\"", encoded))

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// buildTmuxAttachCommand returns the shell command to attach to a tmux
// target on the remote lazyclaude server.
func buildTmuxAttachCommand(tmuxTarget string) string {
	return fmt.Sprintf(
		"tmux -L lazyclaude set-option -t lazyclaude window-size largest 2>/dev/null; "+
			"tmux -L lazyclaude attach-session -t %s",
		posixQuote(tmuxTarget),
	)
}

// --- WorktreeProvider ---

func (rp *RemoteProvider) CreateWorktree(name, prompt, projectRoot string) error {
	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}
	_, err = client.CreateWorktree(context.Background(), WorktreeCreateRequest{
		Name:        name,
		Prompt:      prompt,
		ProjectRoot: projectRoot,
	})
	return err
}

func (rp *RemoteProvider) ResumeWorktree(worktreePath, prompt, projectRoot string) error {
	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("resume worktree: %w", err)
	}
	_, err = client.ResumeWorktree(context.Background(), WorktreeResumeRequest{
		WorktreePath: worktreePath,
		Prompt:       prompt,
		ProjectRoot:  projectRoot,
	})
	return err
}

func (rp *RemoteProvider) ListWorktrees(projectRoot string) ([]WorktreeInfo, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	return client.ListWorktrees(context.Background(), projectRoot)
}

// --- RoleSessionProvider ---

func (rp *RemoteProvider) CreatePMSession(projectRoot string) error {
	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("create PM session: %w", err)
	}
	_, err = client.CreateSession(context.Background(), SessionCreateRequest{
		Path:        projectRoot,
		SessionType: "pm",
	})
	return err
}

func (rp *RemoteProvider) CreateWorkerSession(name, prompt, projectRoot string) error {
	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("create worker session: %w", err)
	}
	_, err = client.CreateSession(context.Background(), SessionCreateRequest{
		Path:        projectRoot,
		SessionType: "worker",
		Name:        name,
		Prompt:      prompt,
	})
	return err
}

// --- ConnectionAware ---

func (rp *RemoteProvider) ConnectionState() ConnectionState {
	return rp.conn.State()
}

// --- Notifications ---

// PendingNotifications returns buffered tool notifications and clears
// the buffer. This bridges the SSE stream to the gui.SessionProvider
// PendingNotifications contract.
func (rp *RemoteProvider) PendingNotifications() []*model.ToolNotification {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if len(rp.notifications) == 0 {
		return nil
	}
	result := rp.notifications
	rp.notifications = nil
	return result
}
