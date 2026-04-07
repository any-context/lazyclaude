package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/any-context/lazyclaude/internal/gui"
	"github.com/any-context/lazyclaude/internal/notify"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/google/uuid"
)

// guiCompositeAdapter wraps daemon.CompositeProvider to implement gui.SessionProvider.
// This bridges the daemon's type system (daemon.SessionInfo etc.) to the GUI's
// type system (gui.SessionItem etc.).
type guiCompositeAdapter struct {
	cp         *daemon.CompositeProvider
	localMgr   *session.Manager
	tmuxClient tmux.Client
	paths      config.Paths

	// windowActivityFn provides window->activity mapping from the App layer.
	windowActivityFn func() map[string]gui.WindowActivityEntry

	// cachedPending is refreshed once per layout cycle.
	cachedPending map[string]bool

	// Lazy remote connection: pendingHost is the default SSH host, initially set
	// at construction from DetectSSHHost() and updated by SetPendingHost after
	// successful connect-dialog connections. Protected by hostMu for thread safety.
	// RWMutex because reads (every operation) vastly outnumber writes (connect dialog).
	hostMu           sync.RWMutex
	pendingHost      string             // Default SSH host (updated after connect dialog)
	localProjectRoot string             // Local project root at startup (immutable after construction)
	connectFn        func(string) error // connectRemoteHost from root.go
	connectMu        sync.Mutex
	connecting       map[string]*lazyConn // one entry per host

	// onError reports errors to the GUI via showError. Wired in root.go.
	// lastErrorMsg deduplicates consecutive identical errors to avoid flooding
	// the GUI when Sessions() fails persistently (e.g. daemon unreachable).
	onError      func(msg string)
	lastErrorMsg string

	// guiUpdateFn triggers a GUI refresh from background goroutines.
	guiUpdateFn func() // triggers gui.Update (wired in root.go)
}

// Compile-time check.
var _ gui.SessionProvider = (*guiCompositeAdapter)(nil)

// SetPendingHost updates the default remote host. Called after a successful
// connection via the connect dialog so that subsequent operations route to
// the newly connected host.
func (a *guiCompositeAdapter) SetPendingHost(host string) {
	a.hostMu.Lock()
	defer a.hostMu.Unlock()
	debugLog("SetPendingHost: %q -> %q", a.pendingHost, host)
	a.pendingHost = host
}

// readPendingHost returns the current default remote host (thread-safe).
func (a *guiCompositeAdapter) readPendingHost() string {
	a.hostMu.RLock()
	defer a.hostMu.RUnlock()
	return a.pendingHost
}

// lazyConn ensures a remote host is connected exactly once.
// If the initial connect fails, subsequent callers see the cached error
// without retrying (connectRemoteHost leaves no side effects on failure).
type lazyConn struct {
	once sync.Once
	err  error
}

// markConnected records that a host has been successfully connected via an
// external path (e.g. the connect dialog). This populates the lazyConn cache
// so that ensureRemoteConnected skips the redundant connectFn call.
func (a *guiCompositeAdapter) markConnected(host string) {
	a.connectMu.Lock()
	defer a.connectMu.Unlock()
	if a.connecting == nil {
		a.connecting = make(map[string]*lazyConn)
	}
	lc := &lazyConn{}
	lc.once.Do(func() {}) // mark as completed with nil error
	a.connecting[host] = lc
	debugLog("markConnected: host=%q cached in lazyConn", host)
}

// ensureRemoteConnected lazily establishes a remote connection on first use.
// Returns nil if host is empty (local operation) or already connected.
// Uses sync.Once per host to guarantee exactly one connectFn call.
func (a *guiCompositeAdapter) ensureRemoteConnected(host string) error {
	debugLog("ensureRemoteConnected: host=%q connectFn=%v", host, a.connectFn != nil)
	if host == "" || a.connectFn == nil {
		return nil
	}

	a.connectMu.Lock()
	if a.connecting == nil {
		a.connecting = make(map[string]*lazyConn)
	}
	lc, ok := a.connecting[host]
	if !ok {
		lc = &lazyConn{}
		a.connecting[host] = lc
	}
	a.connectMu.Unlock()

	lc.once.Do(func() {
		debugLog("ensureRemoteConnected: calling connectFn for host=%q", host)
		lc.err = a.connectFn(host)
		debugLog("ensureRemoteConnected: connectFn result: %v", lc.err)
	})
	return lc.err
}

func (a *guiCompositeAdapter) RefreshPendingFrom(notifications []*model.ToolNotification) {
	a.cachedPending = pendingWindowSet(notifications)
}

func (a *guiCompositeAdapter) Sessions() []gui.SessionItem {
	// All sessions (local + remote mirror windows) are in the local store.
	sessions := a.localMgr.Sessions()
	activity := a.getWindowActivity()
	return buildSessionItems(sessions, a.cachedPending, activity)
}

func (a *guiCompositeAdapter) getWindowActivity() map[string]gui.WindowActivityEntry {
	if a.windowActivityFn != nil {
		return a.windowActivityFn()
	}
	return nil
}

func (a *guiCompositeAdapter) Projects() []gui.ProjectItem {
	projects := a.localMgr.Projects()
	activity := a.getWindowActivity()
	return buildProjectItems(projects, a.cachedPending, activity)
}

func (a *guiCompositeAdapter) ToggleProjectExpanded(projectID string) {
	a.localMgr.ToggleProjectExpanded(projectID)
}

func (a *guiCompositeAdapter) Create(path string) error {
	return a.createWithHost(path, a.readPendingHost())
}

// createWithHost is the shared implementation for Create.
func (a *guiCompositeAdapter) createWithHost(path, host string) error {
	debugLog("createWithHost: path=%q host=%q", path, host)
	if host == "" {
		// Local: synchronous (existing behavior).
		return a.cp.Create(path, "")
	}

	// Remote: optimistic creation. Add a placeholder to the local store
	// immediately so it appears in the sidebar, then attempt connection
	// and session creation in the background. The path is resolved to the
	// remote CWD after the connection is established.
	placeholder := session.Session{
		ID:        uuid.New().String(),
		Name:      a.localMgr.Store().GenerateName(host),
		Path:      path,
		Host:      host,
		Status:    session.StatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	a.localMgr.Store().Add(placeholder, "")
	if err := a.localMgr.Store().Save(); err != nil {
		return fmt.Errorf("save placeholder: %w", err)
	}

	go a.completeRemoteCreate(placeholder.ID, path, host)
	return nil
}

// completeRemoteCreate runs in a background goroutine to finish the
// optimistic session creation. It creates the session on the remote daemon,
// then creates a local mirror window (ssh -t host tmux attach) so that
// the TUI can capture-pane and send-keys through the local tmux.
func (a *guiCompositeAdapter) completeRemoteCreate(placeholderID, localPath, host string) {
	debugLog("completeRemoteCreate: placeholderID=%q localPath=%q host=%q", placeholderID, localPath, host)
	if err := a.ensureRemoteConnected(host); err != nil {
		debugLog("completeRemoteCreate: ensureRemoteConnected failed: %v", err)
		a.failPlaceholder(placeholderID, fmt.Sprintf("Connection failed: %v", err))
		return
	}

	// Resolve the local path to the remote CWD now that the connection exists.
	remotePath := a.resolveRemotePath(localPath, host)
	debugLog("completeRemoteCreate: resolveRemotePath input=%q output=%q", localPath, remotePath)

	// Create session on remote daemon and get session ID.
	rp := a.remoteProvider(host)
	if rp == nil {
		a.failPlaceholder(placeholderID, fmt.Sprintf("no remote provider for host %q", host))
		return
	}
	resp, err := rp.CreateSession(remotePath)
	if err != nil {
		debugLog("completeRemoteCreate: CreateSession failed: %v", err)
		a.failPlaceholder(placeholderID, fmt.Sprintf("Session creation failed: %v", err))
		return
	}
	debugLog("completeRemoteCreate: CreateSession succeeded id=%q window=%q", resp.ID, resp.TmuxWindow)

	// Create local mirror window.
	mirrorName := session.MirrorWindowName(resp.ID)
	if err := a.createMirrorWindow(host, resp.TmuxWindow, mirrorName); err != nil {
		debugLog("completeRemoteCreate: createMirrorWindow failed: %v", err)
		a.failPlaceholder(placeholderID, fmt.Sprintf("Mirror window failed: %v", err))
		return
	}

	// Update the placeholder in the local store with real session info.
	store := a.localMgr.Store()
	store.UpdateSession(placeholderID, func(sess *session.Session) {
		sess.ID = resp.ID
		sess.Name = resp.Name
		sess.Host = host
		sess.Status = session.StatusRunning
		sess.TmuxWindow = mirrorName
	})
	if err := store.Save(); err != nil {
		debugLog("completeRemoteCreate: save store failed: %v", err)
	}
	debugLog("completeRemoteCreate: mirror window %q created for remote session %q", mirrorName, resp.ID)
	a.triggerGUIUpdate()
}

// remoteProvider returns the concrete RemoteProvider for the given host.
func (a *guiCompositeAdapter) remoteProvider(host string) *daemon.RemoteProvider {
	sp := a.cp.RemoteProvider(host)
	if sp == nil {
		return nil
	}
	rp, ok := sp.(*daemon.RemoteProvider)
	if !ok {
		return nil
	}
	return rp
}

// createMirrorWindow creates a local tmux window that SSH-attaches to a
// remote lazyclaude tmux session. The remote command is base64-encoded
// to prevent shell injection from user-controlled host strings.
func (a *guiCompositeAdapter) createMirrorWindow(host, remoteWindow, localWindowName string) error {
	// Build the remote tmux attach command. Quote the target to prevent
	// shell injection from untrusted TmuxWindow values (fetched from daemon API).
	remoteTarget := "lazyclaude:" + remoteWindow
	remoteCmd := fmt.Sprintf(
		"tmux -L lazyclaude set-option -t lazyclaude window-size largest 2>/dev/null; "+
			"tmux -L lazyclaude attach-session -t %s",
		daemon.PosixQuote(remoteTarget),
	)

	// Base64-encode the remote command to prevent shell injection.
	encoded := base64.StdEncoding.EncodeToString([]byte(remoteCmd))

	sshHost, port := daemon.SplitHostPort(host)
	sshArgs := "ssh -t"
	if port != "" {
		sshArgs += " -p " + port
	}
	sshArgs += " " + sshHost
	command := fmt.Sprintf("exec %s eval \"$(echo %s | base64 -d)\"", sshArgs, encoded)

	abs, err := filepath.Abs(".")
	if err != nil {
		abs = "."
	}

	ctx := context.Background()
	return a.tmuxClient.NewWindow(ctx, tmux.NewWindowOpts{
		Session:  "lazyclaude",
		Name:     localWindowName,
		Command:  command,
		StartDir: abs,
	})
}

// createMirrorForExisting creates a mirror window and local store entry for
// an existing remote session discovered during host connection (c key).
// Skips sessions that already have a mirror window in the local store.
func (a *guiCompositeAdapter) createMirrorForExisting(host string, s daemon.SessionInfo) {
	// Skip if already in local store (e.g. reconnection).
	if a.localMgr.Store().FindByID(s.ID) != nil {
		debugLog("createMirrorForExisting: session %q already in store, skipping", s.ID)
		return
	}

	mirrorName := session.MirrorWindowName(s.ID)
	if err := a.createMirrorWindow(host, s.TmuxWindow, mirrorName); err != nil {
		debugLog("createMirrorForExisting: failed for %q: %v", s.ID, err)
		return
	}

	sess := session.Session{
		ID:         s.ID,
		Name:       s.Name,
		Path:       s.Path,
		Host:       host,
		Status:     session.StatusRunning,
		TmuxWindow: mirrorName,
	}
	a.localMgr.Store().Add(sess, s.Path)
	if err := a.localMgr.Store().Save(); err != nil {
		debugLog("createMirrorForExisting: save store failed: %v", err)
	}
	debugLog("createMirrorForExisting: mirror %q created for session %q", mirrorName, s.ID)
}

// failPlaceholder marks a placeholder session as dead and creates a tmux error
// window so that preview, fullscreen, and visual mode all work normally.
func (a *guiCompositeAdapter) failPlaceholder(id, msg string) {
	a.localMgr.Store().SetStatus(id, session.StatusDead)

	// Create a tmux window that displays the error message.
	// This makes the error visible via normal pane capture (preview/fullscreen).
	// The message is passed via environment variable to avoid shell injection
	// (error messages may contain newlines, quotes, or control characters).
	sess := a.localMgr.Store().FindByID(id)
	if sess != nil && a.tmuxClient != nil {
		windowName := sess.WindowName()
		const errCmd = "echo 'lazyclaude: session launch failed'; echo; echo \"$LAZYCLAUDE_ERR_MSG\"; echo; echo 'Press Enter to close'; read"
		abs, err := filepath.Abs(".")
		if err != nil {
			abs = "."
		}
		ctx := context.Background()
		if err := a.tmuxClient.NewWindow(ctx, tmux.NewWindowOpts{
			Session:  "lazyclaude",
			Name:     windowName,
			Command:  errCmd,
			StartDir: abs,
			Env:      map[string]string{"LAZYCLAUDE_ERR_MSG": msg},
		}); err != nil {
			if a.onError != nil {
				a.onError(fmt.Sprintf("create error window: %v", err))
			}
		} else {
			a.localMgr.Store().SetTmuxWindow(id, "lazyclaude:"+windowName)
		}
	}

	if err := a.localMgr.Store().Save(); err != nil && a.onError != nil {
		a.onError(fmt.Sprintf("save store: %v", err))
	}
	if a.onError != nil {
		a.onError(msg)
	}
	a.triggerGUIUpdate()
}

// triggerGUIUpdate schedules a GUI refresh if the callback is wired.
func (a *guiCompositeAdapter) triggerGUIUpdate() {
	if a.guiUpdateFn != nil {
		a.guiUpdateFn()
	}
}

// resolveRemotePath maps a local path to the remote daemon's CWD when
// creating the first session on an SSH host. Once remote sessions exist,
// currentProjectRoot() returns the correct remote path from the session
// tree, so the provided path is returned unchanged.
//
// The remote CWD is obtained via the daemon GET /cwd API. This requires
// the remote connection to be established first (call ensureRemoteConnected
// before this method).
func (a *guiCompositeAdapter) resolveRemotePath(path, host string) string {
	debugLog("resolveRemotePath: input=%q host=%q", path, host)
	// Always query the remote daemon for its CWD when the host is set.
	// Local paths (from currentProjectRoot fallback) are meaningless on
	// the remote machine.
	remoteCWD := a.queryRemoteCWD(host)
	if remoteCWD != "" {
		debugLog("resolveRemotePath: output=%q (from queryRemoteCWD)", remoteCWD)
		return remoteCWD
	}
	// Fallback: use "." so the daemon uses its own CWD.
	if host != "" {
		debugLog("resolveRemotePath: output=%q (fallback dot)", ".")
		return "."
	}
	debugLog("resolveRemotePath: output=%q (passthrough)", path)
	return path
}

// cwdQueryTimeout is the maximum time to wait for a remote CWD query.
const cwdQueryTimeout = 10 * time.Second

// queryRemoteCWD fetches the working directory from a connected remote daemon.
// Returns "" if the query fails (caller should fall back to the original path).
func (a *guiCompositeAdapter) queryRemoteCWD(host string) string {
	debugLog("queryRemoteCWD: host=%q", host)
	provider := a.cp.RemoteProvider(host)
	debugLog("queryRemoteCWD: provider=%v", provider != nil)
	if provider == nil {
		return ""
	}
	querier, ok := provider.(daemon.CWDQuerier)
	debugLog("queryRemoteCWD: implements CWDQuerier=%v", ok)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), cwdQueryTimeout)
	defer cancel()
	cwd, err := querier.QueryCWD(ctx)
	debugLog("queryRemoteCWD: cwd=%q err=%v", cwd, err)
	if err != nil {
		return ""
	}
	return cwd
}

func (a *guiCompositeAdapter) Delete(id string) error {
	sess := a.localMgr.Store().FindByID(id)
	if sess == nil {
		return fmt.Errorf("session not found: %s", id)
	}
	if sess.Host != "" {
		// Remote session: delete on daemon first, then kill local mirror.
		if rp := a.remoteProvider(sess.Host); rp != nil {
			if err := rp.Delete(id); err != nil {
				debugLog("Delete: daemon API failed (continuing): %v", err)
			}
		}
		// Kill local mirror window.
		mirrorName := session.MirrorWindowName(id)
		_ = a.tmuxClient.KillWindow(context.Background(), "lazyclaude:"+mirrorName)
		a.localMgr.Store().Remove(id)
		return a.localMgr.Store().Save()
	}
	return a.cp.Delete(id)
}

func (a *guiCompositeAdapter) Rename(id, newName string) error {
	sess := a.localMgr.Store().FindByID(id)
	if sess == nil {
		return fmt.Errorf("session not found: %s", id)
	}
	if sess.Host != "" {
		// Remote session: rename on daemon + update local store.
		rp := a.remoteProvider(sess.Host)
		if rp == nil {
			return fmt.Errorf("no remote provider for host %q", sess.Host)
		}
		if err := rp.Rename(id, newName); err != nil {
			return fmt.Errorf("remote rename: %w", err)
		}
		a.localMgr.Store().UpdateSession(id, func(s *session.Session) {
			s.Name = newName
		})
		return a.localMgr.Store().Save()
	}
	return a.cp.Rename(id, newName)
}

func (a *guiCompositeAdapter) PurgeOrphans() (int, error) {
	return a.cp.PurgeOrphans()
}

func (a *guiCompositeAdapter) CapturePreview(id string, width, height int) (gui.PreviewResult, error) {
	resp, err := a.cp.CapturePreview(id, width, height)
	if err != nil || resp == nil {
		return gui.PreviewResult{}, err
	}
	return gui.PreviewResult{
		Content: resp.Content,
		CursorX: resp.CursorX,
		CursorY: resp.CursorY,
	}, nil
}

func (a *guiCompositeAdapter) CaptureScrollback(id string, width, startLine, endLine int) (gui.PreviewResult, error) {
	resp, err := a.cp.CaptureScrollback(id, width, startLine, endLine)
	if err != nil || resp == nil {
		return gui.PreviewResult{}, err
	}
	return gui.PreviewResult{Content: resp.Content}, nil
}

func (a *guiCompositeAdapter) HistorySize(id string) (int, error) {
	return a.cp.HistorySize(id)
}

func (a *guiCompositeAdapter) PendingNotifications() []*model.ToolNotification {
	notifications, err := notify.ReadAll(a.paths.RuntimeDir)
	if err != nil || len(notifications) == 0 {
		return nil
	}
	return notifications
}

func (a *guiCompositeAdapter) SendChoice(window string, c gui.Choice) error {
	return a.cp.SendChoice(window, int(c))
}

func (a *guiCompositeAdapter) AttachSession(id string) error {
	return a.cp.AttachSession(id)
}

func (a *guiCompositeAdapter) LaunchLazygit(path string) error {
	return a.cp.LaunchLazygit(path, "")
}

func (a *guiCompositeAdapter) CreateWorktree(name, prompt, projectRoot string) error {
	return a.createWorktreeWithHost(name, prompt, projectRoot, a.readPendingHost())
}

func (a *guiCompositeAdapter) createWorktreeWithHost(name, prompt, projectRoot, host string) error {
	debugLog("createWorktreeWithHost: name=%q host=%q", name, host)
	if err := a.ensureRemoteConnected(host); err != nil {
		return err
	}
	if host != "" {
		projectRoot = a.resolveRemotePath(projectRoot, host)
	}
	return a.cp.CreateWorktree(name, prompt, projectRoot, host)
}

func (a *guiCompositeAdapter) ResumeWorktree(worktreePath, prompt, projectRoot string) error {
	return a.resumeWorktreeWithHost(worktreePath, prompt, projectRoot, a.readPendingHost())
}

func (a *guiCompositeAdapter) resumeWorktreeWithHost(worktreePath, prompt, projectRoot, host string) error {
	debugLog("resumeWorktreeWithHost: wtPath=%q host=%q", worktreePath, host)
	if err := a.ensureRemoteConnected(host); err != nil {
		return err
	}
	if host != "" {
		projectRoot = a.resolveRemotePath(projectRoot, host)
	}
	return a.cp.ResumeWorktree(worktreePath, prompt, projectRoot, host)
}

func (a *guiCompositeAdapter) ListWorktrees(projectRoot string) ([]gui.WorktreeInfo, error) {
	return a.listWorktreesWithHost(projectRoot, a.readPendingHost())
}

func (a *guiCompositeAdapter) listWorktreesWithHost(projectRoot, host string) ([]gui.WorktreeInfo, error) {
	debugLog("listWorktreesWithHost: projectRoot=%q host=%q", projectRoot, host)
	if err := a.ensureRemoteConnected(host); err != nil {
		return nil, err
	}
	if host != "" {
		projectRoot = a.resolveRemotePath(projectRoot, host)
	}
	items, err := a.cp.ListWorktrees(projectRoot, host)
	if err != nil {
		return nil, err
	}
	result := make([]gui.WorktreeInfo, len(items))
	for i, item := range items {
		result[i] = gui.WorktreeInfo{Name: item.Name, Path: item.Path, Branch: item.Branch}
	}
	return result, nil
}

func (a *guiCompositeAdapter) CreatePMSession(projectRoot string) error {
	return a.createPMSessionWithHost(projectRoot, a.readPendingHost())
}

func (a *guiCompositeAdapter) createPMSessionWithHost(projectRoot, host string) error {
	debugLog("createPMSessionWithHost: projectRoot=%q host=%q", projectRoot, host)
	if err := a.ensureRemoteConnected(host); err != nil {
		return err
	}
	if host != "" {
		projectRoot = a.resolveRemotePath(projectRoot, host)
	}
	return a.cp.CreatePMSession(projectRoot, host)
}

func (a *guiCompositeAdapter) CreateWorkerSession(name, prompt, projectRoot string) error {
	return a.createWorkerSessionWithHost(name, prompt, projectRoot, a.readPendingHost())
}

func (a *guiCompositeAdapter) createWorkerSessionWithHost(name, prompt, projectRoot, host string) error {
	debugLog("createWorkerSessionWithHost: name=%q host=%q", name, host)
	if err := a.ensureRemoteConnected(host); err != nil {
		return err
	}
	if host != "" {
		projectRoot = a.resolveRemotePath(projectRoot, host)
	}
	return a.cp.CreateWorkerSession(name, prompt, projectRoot, host)
}

