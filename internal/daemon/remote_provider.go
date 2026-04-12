package daemon

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/shell"
)

// Compile-time check: RemoteProvider implements SessionProvider.
var _ SessionProvider = (*RemoteProvider)(nil)

// PostCreateHook is called after a remote session is created via daemon API.
// The hook creates a mirror tmux window and adds the session to the local store.
type PostCreateHook func(host, path string, resp *SessionCreateResponse) error

// RemoteProviderOption configures a RemoteProvider.
type RemoteProviderOption func(*RemoteProvider)

// WithPostCreate sets the hook called after session creation on the remote daemon.
func WithPostCreate(hook PostCreateHook) RemoteProviderOption {
	return func(rp *RemoteProvider) { rp.postCreate = hook }
}

// SSEActivityCallback is called when an SSE activity event is received.
//
// The event carries a best-effort Window (the remapped mirror name
// "rm-xxxx"), but the sessionID is the authoritative key: callers should
// use it to look up the corresponding local mirror session and overwrite
// Window with the mirror's current local tmux window ID ("@42"). This
// keeps the broker's activity key space aligned with the sidebar lookup
// (which keys by Session.TmuxWindow = local tmux window ID).
type SSEActivityCallback func(ev model.Event, sessionID string)

// WithSSEActivity sets the callback for SSE activity events.
// Used to forward remote activity to the local broker for sidebar display.
func WithSSEActivity(cb SSEActivityCallback) RemoteProviderOption {
	return func(rp *RemoteProvider) { rp.onSSEActivity = cb }
}

// SSEToolInfoCallback is invoked when an SSE EventToolInfo arrives.
//
// The callback may mutate the notification in place (e.g. rewrite
// ToolNotification.Window from the remote tmux window ID to the local
// mirror's tmux window ID) before it is buffered for the next
// PendingNotifications call. The sessionID comes from
// NotificationEvent.SessionID emitted by the daemon SSE handler and is
// the authoritative key for looking up the local mirror session.
//
// Concurrency contract: the callback is invoked while the
// RemoteProvider's internal mutex is held. Callback implementations
// MUST NOT re-enter any RemoteProvider method (e.g. PendingNotifications,
// HasSession, Sessions) or deadlock will result. Look-ups against
// external stores (session.Store, etc.) are fine because they have
// their own independent locks.
//
// Mirrors SSEActivityCallback for the ToolInfo code path; see Bug 5
// for the background (remote permission popup action routing fix).
type SSEToolInfoCallback func(n *model.ToolNotification, sessionID string)

// WithSSEToolInfo sets the callback invoked on SSE EventToolInfo events.
// Used by root.go to rewrite ToolNotification.Window to the local mirror
// window ID so the permission popup's Accept/Reject keystrokes reach the
// correct pane.
func WithSSEToolInfo(cb SSEToolInfoCallback) RemoteProviderOption {
	return func(rp *RemoteProvider) { rp.onSSEToolInfo = cb }
}

// RemoteProvider adapts a daemon ClientAPI to the SessionProvider interface.
// It maintains a local cache of sessions and buffers tool notifications
// received via SSE.
//
// All operations (including preview capture, scrollback, key sending) go
// through the daemon API. Socket tunnel forwarding was removed because
// remote sshd environments often block Unix domain socket forwarding.
type RemoteProvider struct {
	host          string
	conn          ConnectionManager
	postCreate    PostCreateHook      // immutable after construction
	onSSEActivity SSEActivityCallback // immutable after construction
	onSSEToolInfo SSEToolInfoCallback // immutable after construction

	mu            sync.Mutex
	sessions      []SessionInfo
	notifications []*model.ToolNotification

	cancelSSE context.CancelFunc
	sseDone   chan struct{} // closed when the current SSE goroutine exits
}

// NewRemoteProvider creates a RemoteProvider for the given host.
// The ConnectionManager must already be connected or will be connected
// separately; this constructor does not initiate the connection.
func NewRemoteProvider(host string, conn ConnectionManager, opts ...RemoteProviderOption) *RemoteProvider {
	rp := &RemoteProvider{
		host: host,
		conn: conn,
	}
	for _, o := range opts {
		o(rp)
	}
	return rp
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
			if rp.sessions[i].ID == ev.SessionID || strings.HasPrefix(rp.sessions[i].ID, ev.SessionID) {
				rp.sessions[i].Activity = ev.Activity
				rp.sessions[i].ToolName = ev.ToolName
				// Forward to local broker for sidebar display.
				if rp.onSSEActivity != nil {
					// Window is a best-effort mirror name; the callback
					// in root.go resolves the authoritative local tmux
					// window ID using sessionID as a lookup hop.
					mirrorWindow := remapRemoteWindow(rp.sessions[i].TmuxWindow)
					rp.onSSEActivity(model.Event{ActivityNotification: &model.ActivityNotification{
						Window:   mirrorWindow,
						State:    ev.Activity,
						ToolName: ev.ToolName,
					}}, rp.sessions[i].ID)
				}
				break
			}
		}
	case EventToolInfo:
		if ev.ToolNotification != nil {
			// Apply optional rewrite hook (e.g. rewrite Window to the
			// local mirror's tmux window ID using ev.SessionID). The
			// callback mutates the notification in place before it is
			// buffered, mirroring the SSEActivity callback pattern.
			if rp.onSSEToolInfo != nil {
				rp.onSSEToolInfo(ev.ToolNotification, ev.SessionID)
			}
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
//
// Callers are responsible for mirror setup. postCreate is NOT called
// from this method because the optimistic UI flow (Create → placeholder
// → completeRemoteCreate) handles mirrors separately.
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
	rp.addToCache(resp)
	return resp, nil
}

func (rp *RemoteProvider) Delete(id string) error {
	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if err := client.DeleteSession(context.Background(), id); err != nil {
		return err
	}
	rp.removeFromCache(id)
	return nil
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

// CaptureScrollback retrieves scrollback via the remote daemon API. This is
// the fullscreen copy-mode path for remote sessions: the local mirror
// window's tmux buffer does not contain the remote tmux's historical
// scrollback, so we ask the remote daemon to run capture-pane against its
// own tmux server.
func (rp *RemoteProvider) CaptureScrollback(id string, width, startLine, endLine int) (*ScrollbackResponse, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return nil, fmt.Errorf("capture scrollback: %w", err)
	}
	return client.CaptureScrollback(context.Background(), ScrollbackRequest{
		ID:        id,
		Width:     width,
		StartLine: startLine,
		EndLine:   endLine,
	})
}

// HistorySize returns the remote tmux pane's scrollback history size via
// the daemon API. Same rationale as CaptureScrollback.
func (rp *RemoteProvider) HistorySize(id string) (int, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return 0, fmt.Errorf("history size: %w", err)
	}
	return client.HistorySize(context.Background(), id)
}

// LocalSessionHost returns the host for a session if it is known to this
// remote provider's cache. Used by CompositeProvider.providerForCapture to
// dispatch capture ops to the correct backend without leaking the
// session.Session type into the daemon package's interface surface.
func (rp *RemoteProvider) LocalSessionHost(id string) (string, bool) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	for _, s := range rp.sessions {
		if s.ID == id {
			return rp.host, true
		}
	}
	return "", false
}

// LaunchLazygit launches lazygit on the remote host via SSH -t.
// This bypasses the daemon API entirely.
func (rp *RemoteProvider) LaunchLazygit(path string) error {
	remoteCmd := fmt.Sprintf("cd %s && lazygit", shell.Quote(path))
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
// target on the remote lazyclaude server. It creates a grouped session
// (new-session -t) so that each client has independent window selection,
// preventing multiple mirrors from overriding each other's active window.
// The grouped session uses destroy-unattached so it is cleaned up when
// the SSH connection drops.
func buildTmuxAttachCommand(tmuxTarget string) string {
	// Extract the window name from "session:window" format.
	window := tmuxTarget
	if _, after, ok := strings.Cut(tmuxTarget, ":"); ok {
		window = after
	}
	return fmt.Sprintf(
		"tmux -L lazyclaude set-option -t lazyclaude window-size largest 2>/dev/null; "+
			"tmux -L lazyclaude new-session -t lazyclaude -s attach-$$ "+
			"\\; set-option destroy-unattached on "+
			"\\; select-window -t %s",
		shell.Quote(window),
	)
}

// --- WorktreeProvider ---

// addToCache appends (or replaces) a session in rp.sessions based on resp.
// This keeps the in-memory session cache coherent with the remote daemon
// after a successful create call, without waiting for the next SSE
// full_sync. Without this, SSE activity events keyed by the new session's
// UUID would MISS the cache and the sidebar would never transition from
// Unknown → Running/Idle until the user reconnects.
//
// Safe to call with rp.mu unlocked; this method takes the lock itself.
func (rp *RemoteProvider) addToCache(resp *SessionCreateResponse) {
	if resp == nil || resp.ID == "" {
		return
	}
	info := SessionInfo{
		ID:         resp.ID,
		Name:       resp.Name,
		Path:       resp.Path,
		Host:       rp.host,
		TmuxWindow: resp.TmuxWindow,
		Role:       resp.Role,
		Status:     "running",
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	for i := range rp.sessions {
		if rp.sessions[i].ID == resp.ID {
			rp.sessions[i] = info
			return
		}
	}
	rp.sessions = append(rp.sessions, info)
}

// removeFromCache drops a session from rp.sessions by ID.
// Keeps the cache coherent with remote daemon state after a delete.
func (rp *RemoteProvider) removeFromCache(id string) {
	if id == "" {
		return
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	for i := range rp.sessions {
		if rp.sessions[i].ID == id {
			rp.sessions = append(rp.sessions[:i], rp.sessions[i+1:]...)
			return
		}
	}
}

// invokePostCreate calls the postCreate hook if one is registered.
// projectRoot is the local grouping path; resp contains the newly created session details.
func (rp *RemoteProvider) invokePostCreate(projectRoot string, resp *SessionCreateResponse) error {
	rp.addToCache(resp)
	if rp.postCreate != nil {
		return rp.postCreate(rp.host, projectRoot, resp)
	}
	return nil
}

func (rp *RemoteProvider) CreateWorktree(name, prompt, projectRoot string) error {
	resp, err := rp.createWorktreeResp(name, prompt, projectRoot)
	if err != nil {
		return err
	}
	return rp.invokePostCreate(projectRoot, resp)
}

// createWorktreeResp creates a worktree on the remote daemon and returns the
// response containing the session ID and tmux window name.
func (rp *RemoteProvider) createWorktreeResp(name, prompt, projectRoot string) (*SessionCreateResponse, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}
	resp, err := client.CreateWorktree(context.Background(), WorktreeCreateRequest{
		Name:        name,
		Prompt:      prompt,
		ProjectRoot: projectRoot,
	})
	if err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}
	return &SessionCreateResponse{
		ID:         resp.SessionID,
		Name:       name,
		Path:       resp.Path,
		TmuxWindow: resp.TmuxWindow,
		Role:       resp.Role,
	}, nil
}

func (rp *RemoteProvider) ResumeWorktree(worktreePath, prompt, projectRoot string) error {
	resp, err := rp.resumeWorktreeResp(worktreePath, prompt, projectRoot)
	if err != nil {
		return err
	}
	return rp.invokePostCreate(projectRoot, resp)
}

// resumeWorktreeResp resumes a worktree on the remote daemon and returns the
// response containing the session ID and tmux window name.
func (rp *RemoteProvider) resumeWorktreeResp(worktreePath, prompt, projectRoot string) (*SessionCreateResponse, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return nil, fmt.Errorf("resume worktree: %w", err)
	}
	resp, err := client.ResumeWorktree(context.Background(), WorktreeResumeRequest{
		WorktreePath: worktreePath,
		Prompt:       prompt,
		ProjectRoot:  projectRoot,
	})
	if err != nil {
		return nil, fmt.Errorf("resume worktree: %w", err)
	}
	return &SessionCreateResponse{
		ID:         resp.SessionID,
		Name:       resp.Name,
		Path:       resp.Path,
		TmuxWindow: resp.TmuxWindow,
		Role:       resp.Role,
	}, nil
}

func (rp *RemoteProvider) ResumeSession(id, prompt, name string) error {
	client, err := rp.conn.Client()
	if err != nil {
		return fmt.Errorf("resume session: %w", err)
	}
	resp, err := client.ResumeSession(context.Background(), SessionResumeRequest{
		ID:     id,
		Prompt: prompt,
		Name:   name,
	})
	if err != nil {
		return fmt.Errorf("resume session: %w", err)
	}
	return rp.invokePostCreate("", &SessionCreateResponse{
		ID:         resp.SessionID,
		Name:       resp.Name,
		Path:       resp.Path,
		TmuxWindow: resp.TmuxWindow,
		Role:       resp.Role,
	})
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
	resp, err := rp.createPMSessionResp(projectRoot)
	if err != nil {
		return err
	}
	return rp.invokePostCreate(projectRoot, resp)
}

// createPMSessionResp creates a PM session on the remote daemon and returns
// the response containing the session ID and tmux window name.
func (rp *RemoteProvider) createPMSessionResp(projectRoot string) (*SessionCreateResponse, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return nil, fmt.Errorf("create PM session: %w", err)
	}
	return client.CreateSession(context.Background(), SessionCreateRequest{
		Path:        projectRoot,
		SessionType: "pm",
		ProjectRoot: projectRoot,
	})
}

func (rp *RemoteProvider) CreateWorkerSession(name, prompt, projectRoot string) error {
	resp, err := rp.createWorkerSessionResp(name, prompt, projectRoot)
	if err != nil {
		return err
	}
	return rp.invokePostCreate(projectRoot, resp)
}

// createWorkerSessionResp creates a worker session on the remote daemon and
// returns the response containing the session ID and tmux window name.
func (rp *RemoteProvider) createWorkerSessionResp(name, prompt, projectRoot string) (*SessionCreateResponse, error) {
	client, err := rp.conn.Client()
	if err != nil {
		return nil, fmt.Errorf("create worker session: %w", err)
	}
	return client.CreateSession(context.Background(), SessionCreateRequest{
		Path:        projectRoot,
		SessionType: "worker",
		Name:        name,
		Prompt:      prompt,
		ProjectRoot: projectRoot,
	})
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
