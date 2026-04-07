package daemon

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/any-context/lazyclaude/internal/core/model"
)

// Compile-time check: RemoteProvider implements SessionProvider.
var _ SessionProvider = (*RemoteProvider)(nil)

// RemoteProvider adapts a daemon ClientAPI to the SessionProvider interface.
// It maintains a local cache of sessions and buffers tool notifications
// received via SSE.
//
// All operations (including preview capture, scrollback, key sending) go
// through the daemon API. Socket tunnel forwarding was removed because
// remote sshd environments often block Unix domain socket forwarding.
type RemoteProvider struct {
	host string
	conn ConnectionManager

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

// Conn returns the underlying ConnectionManager for direct API access.
func (rp *RemoteProvider) Conn() ConnectionManager {
	return rp.conn
}

// QueryCWD implements CWDQuerier by fetching the working directory from
// the remote daemon via the GET /cwd API.
func (rp *RemoteProvider) QueryCWD(ctx context.Context) (string, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return "", fmt.Errorf("query cwd: %w", err)
	}
	return client.CWD(ctx)
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
	_, err := rp.CreateSession(path)
	return err
}

// CreateSession creates a session on the remote daemon and returns the
// response containing the ID and tmux window name. Used by the mirror
// window creation flow which needs the session ID to construct the
// mirror window's SSH attach command.
func (rp *RemoteProvider) CreateSession(path string) (*SessionCreateResponse, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	resp, err := client.CreateSession(context.Background(), SessionCreateRequest{
		Path:        path,
		SessionType: "plain",
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return resp, nil
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

// --- SessionActioner ---

// AttachSession attaches to a remote session via SSH -t tmux attach.
// Attach always uses SSH because the user's terminal must be connected
// to the remote tmux process directly.
func (rp *RemoteProvider) AttachSession(id string) error {
	target := rp.resolveTmuxTarget(id)
	return rp.runSSHInteractive(buildTmuxAttachCommand(target))
}

// resolveTmuxTarget returns the tmux target string for a session on the
// remote host. Used by AttachSession to construct the SSH attach command.
func (rp *RemoteProvider) resolveTmuxTarget(id string) string {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	for _, s := range rp.sessions {
		if s.ID == id && s.TmuxWindow != "" {
			return s.TmuxWindow
		}
	}
	name := "lc-" + id
	if len(id) > 8 {
		name = "lc-" + id[:8]
	}
	return tmuxSessionName + ":" + name
}

// SendChoice is a no-op for remote sessions. With mirror windows, permission
// choices are sent via the local tmux provider to the mirror window.
func (rp *RemoteProvider) SendChoice(_ string, _ int) error {
	return fmt.Errorf("SendChoice not supported on remote provider (use mirror window)")
}

// CapturePreview is a no-op for remote sessions. With mirror windows, preview
// capture is handled by the local tmux provider via the mirror window.
func (rp *RemoteProvider) CapturePreview(_ string, _, _ int) (*PreviewResponse, error) {
	return nil, fmt.Errorf("CapturePreview not supported on remote provider (use mirror window)")
}

// CaptureScrollback is a no-op for remote sessions.
func (rp *RemoteProvider) CaptureScrollback(_ string, _, _, _ int) (*ScrollbackResponse, error) {
	return nil, fmt.Errorf("CaptureScrollback not supported on remote provider (use mirror window)")
}

// HistorySize is a no-op for remote sessions.
func (rp *RemoteProvider) HistorySize(_ string) (int, error) {
	return 0, fmt.Errorf("HistorySize not supported on remote provider (use mirror window)")
}

// LaunchLazygit launches lazygit on the remote host via SSH -t.
// This bypasses the daemon API entirely.
func (rp *RemoteProvider) LaunchLazygit(path string) error {
	remoteCmd := fmt.Sprintf("cd %s && lazygit", PosixQuote(path))
	return rp.runSSHInteractive(remoteCmd)
}

// runSSHInteractive runs an interactive SSH command with stdin/stdout/stderr
// connected to the current terminal.
func (rp *RemoteProvider) runSSHInteractive(remoteCmd string) error {
	sshHost, port := SplitHostPort(rp.host)
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
		PosixQuote(tmuxTarget),
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
