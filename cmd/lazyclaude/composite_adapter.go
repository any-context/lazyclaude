package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/any-context/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/any-context/lazyclaude/internal/core/choice"
	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/any-context/lazyclaude/internal/gui"
	"github.com/any-context/lazyclaude/internal/notify"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

// localDaemonProvider wraps session.Manager to implement daemon.SessionProvider.
// Used as the local backend for CompositeProvider.
type localDaemonProvider struct {
	mgr   *session.Manager
	tmux  tmux.Client
	paths config.Paths

	lastResizeID string
	lastResizeW  int
	lastResizeH  int
}

// Compile-time check.
var _ daemon.SessionProvider = (*localDaemonProvider)(nil)

func (p *localDaemonProvider) HasSession(sessionID string) bool {
	return p.mgr.Store().FindByID(sessionID) != nil
}

func (p *localDaemonProvider) Host() string { return "" }

func (p *localDaemonProvider) Sessions() ([]daemon.SessionInfo, error) {
	sessions := p.mgr.Sessions()
	items := make([]daemon.SessionInfo, len(sessions))
	for i, s := range sessions {
		items[i] = sessionToDaemonInfo(s)
	}
	return items, nil
}

func (p *localDaemonProvider) Create(path string) error {
	if path == "." {
		abs, err := filepath.Abs(".")
		if err != nil {
			return err
		}
		path = abs
	}
	_, err := p.mgr.Create(context.Background(), path)
	return err
}

func (p *localDaemonProvider) Delete(id string) error {
	return p.mgr.Delete(context.Background(), id)
}

func (p *localDaemonProvider) Rename(id, newName string) error {
	return p.mgr.Rename(id, newName)
}

func (p *localDaemonProvider) PurgeOrphans() (int, error) {
	return p.mgr.PurgeOrphans()
}

func (p *localDaemonProvider) CapturePreview(id string, width, height int) (*daemon.PreviewResponse, error) {
	sess := p.mgr.Store().FindByID(id)
	if sess == nil {
		return &daemon.PreviewResponse{}, nil
	}
	target := sess.TmuxWindow
	if target == "" {
		target = "lazyclaude:" + sess.WindowName()
	}
	ctx := context.Background()

	if width > 0 && height > 0 && (id != p.lastResizeID || width != p.lastResizeW || height != p.lastResizeH) {
		if err := p.tmux.ResizeWindow(ctx, target, width, height); err != nil {
			return nil, err
		}
		p.lastResizeID = id
		p.lastResizeW = width
		p.lastResizeH = height
		time.Sleep(20 * time.Millisecond)
	}

	content, err := p.tmux.CapturePaneANSI(ctx, target)
	if err != nil || width <= 0 {
		return &daemon.PreviewResponse{Content: content}, err
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
	if pos, posErr := p.tmux.ShowMessage(ctx, target, "#{cursor_x},#{cursor_y}"); posErr == nil {
		parts := strings.SplitN(strings.TrimSpace(pos), ",", 2)
		if len(parts) == 2 {
			cursorX, _ = strconv.Atoi(parts[0])
			cursorY, _ = strconv.Atoi(parts[1])
		}
	}

	return &daemon.PreviewResponse{
		Content: strings.Join(lines, "\n"),
		CursorX: cursorX,
		CursorY: cursorY,
	}, nil
}

func (p *localDaemonProvider) CaptureScrollback(id string, _, startLine, endLine int) (*daemon.ScrollbackResponse, error) {
	sess := p.mgr.Store().FindByID(id)
	if sess == nil {
		return &daemon.ScrollbackResponse{}, nil
	}
	target := sess.TmuxWindow
	if target == "" {
		target = "lazyclaude:" + sess.WindowName()
	}
	content, err := p.tmux.CapturePaneANSIRange(context.Background(), target, startLine, endLine)
	return &daemon.ScrollbackResponse{Content: content}, err
}

func (p *localDaemonProvider) HistorySize(id string) (int, error) {
	sess := p.mgr.Store().FindByID(id)
	if sess == nil {
		return 0, nil
	}
	target := sess.TmuxWindow
	if target == "" {
		target = "lazyclaude:" + sess.WindowName()
	}
	out, err := p.tmux.ShowMessage(context.Background(), target, "#{history_size}")
	if err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n, nil
}

func (p *localDaemonProvider) SendChoice(window string, choiceVal int) error {
	return tmuxadapter.SendToPane(context.Background(), p.tmux, window, choice.Choice(choiceVal))
}

func (p *localDaemonProvider) AttachSession(id string) error {
	sess := p.mgr.Store().FindByID(id)
	if sess == nil {
		return fmt.Errorf("session not found: %s", id)
	}
	target := "lazyclaude:" + sess.WindowName()

	_ = exec.Command("tmux", "-L", "lazyclaude", "set-option", "-t", "lazyclaude", "window-size", "largest").Run()

	cmd := exec.Command("tmux", "-L", "lazyclaude", "attach-session", "-t", target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (p *localDaemonProvider) LaunchLazygit(path string) error {
	if _, err := exec.LookPath("lazygit"); err != nil {
		return fmt.Errorf("lazygit is not installed")
	}
	cmd := exec.Command("lazygit")
	cmd.Dir = path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (p *localDaemonProvider) CreateWorktree(name, prompt, projectRoot string) error {
	_, err := p.mgr.CreateWorktree(context.Background(), name, prompt, projectRoot)
	return err
}

func (p *localDaemonProvider) ResumeWorktree(worktreePath, prompt, projectRoot string) error {
	_, err := p.mgr.ResumeWorktree(context.Background(), worktreePath, prompt, projectRoot)
	return err
}

func (p *localDaemonProvider) ListWorktrees(projectRoot string) ([]daemon.WorktreeInfo, error) {
	items, err := session.ListWorktrees(context.Background(), projectRoot)
	if err != nil {
		return nil, err
	}
	result := make([]daemon.WorktreeInfo, len(items))
	for i, item := range items {
		result[i] = daemon.WorktreeInfo{Name: item.Name, Path: item.Path, Branch: item.Branch}
	}
	return result, nil
}

func (p *localDaemonProvider) CreatePMSession(projectRoot string) error {
	_, err := p.mgr.CreatePMSession(context.Background(), projectRoot)
	return err
}

func (p *localDaemonProvider) CreateWorkerSession(name, prompt, projectRoot string) error {
	_, err := p.mgr.CreateWorkerSession(context.Background(), name, prompt, projectRoot)
	return err
}

func (p *localDaemonProvider) ConnectionState() daemon.ConnectionState {
	return daemon.Connected
}

// sessionToDaemonInfo converts a session.Session to daemon.SessionInfo.
func sessionToDaemonInfo(s session.Session) daemon.SessionInfo {
	return daemon.SessionInfo{
		ID:         s.ID,
		Name:       s.Name,
		Path:       s.Path,
		Host:       s.Host,
		Status:     s.Status.String(),
		Flags:      s.Flags,
		TmuxWindow: s.TmuxWindow,
		Role:       string(s.Role),
	}
}

// guiCompositeAdapter wraps daemon.CompositeProvider to implement gui.SessionProvider.
// This bridges the daemon's type system (daemon.SessionInfo etc.) to the GUI's
// type system (gui.SessionItem etc.).
type guiCompositeAdapter struct {
	cp       *daemon.CompositeProvider
	localMgr *session.Manager
	paths    config.Paths

	// windowActivityFn provides window->activity mapping from the App layer.
	windowActivityFn func() map[string]gui.WindowActivityEntry

	// cachedPending is refreshed once per layout cycle.
	cachedPending map[string]bool

	// Lazy remote connection: pendingHost is set once at construction and never
	// mutated. connectFn is the root.go connectRemoteHost closure.
	pendingHost      string             // SSH host detected at startup (immutable after construction)
	localProjectRoot string             // Local project root at startup (immutable after construction)
	connectFn        func(string) error // connectRemoteHost from root.go
	connectMu        sync.Mutex
	connecting       map[string]*lazyConn // one entry per host

	// onError reports errors to the GUI via showError. Wired in root.go.
	// lastErrorMsg deduplicates consecutive identical errors to avoid flooding
	// the GUI when Sessions() fails persistently (e.g. daemon unreachable).
	onError      func(msg string)
	lastErrorMsg string

	// Optimistic session creation: tracks placeholder sessions created before
	// remote connection is established.
	// sessionErrors maps placeholder session IDs to error messages for display
	// in the preview pane. remoteSessionMap maps placeholder IDs to the real
	// remote session IDs for preview capture routing.
	// guiUpdateFn triggers a GUI refresh from background goroutines.
	optimisticMu     sync.Mutex
	sessionErrors    map[string]string // placeholder ID -> error message
	remoteSessionMap map[string]string // placeholder ID -> remote session ID
	guiUpdateFn      func()           // triggers gui.Update (wired in root.go)
}

// Compile-time check.
var _ gui.SessionProvider = (*guiCompositeAdapter)(nil)

// lazyConn ensures a remote host is connected exactly once.
// If the initial connect fails, subsequent callers see the cached error
// without retrying (connectRemoteHost leaves no side effects on failure).
type lazyConn struct {
	once sync.Once
	err  error
}

// ensureRemoteConnected lazily establishes a remote connection on first use.
// Returns nil if host is empty (local operation) or already connected.
// Uses sync.Once per host to guarantee exactly one connectFn call.
func (a *guiCompositeAdapter) ensureRemoteConnected(host string) error {
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
		lc.err = a.connectFn(host)
	})
	return lc.err
}

func (a *guiCompositeAdapter) RefreshPendingFrom(notifications []*model.ToolNotification) {
	a.cachedPending = pendingWindowSet(notifications)
}

func (a *guiCompositeAdapter) Sessions() []gui.SessionItem {
	sessions, err := a.cp.Sessions()
	if err != nil {
		msg := fmt.Sprintf("Session list error: %v", err)
		if a.onError != nil && msg != a.lastErrorMsg {
			a.lastErrorMsg = msg
			a.onError(msg)
		}
		return nil
	}
	a.lastErrorMsg = "" // clear on success so next error is reported
	items := make([]gui.SessionItem, len(sessions))
	activity := a.getWindowActivity()
	for i, s := range sessions {
		items[i] = daemonInfoToGUIItem(s, a.cachedPending, activity)
	}
	return items
}

func (a *guiCompositeAdapter) getWindowActivity() map[string]gui.WindowActivityEntry {
	if a.windowActivityFn != nil {
		return a.windowActivityFn()
	}
	return nil
}

func (a *guiCompositeAdapter) Projects() []gui.ProjectItem {
	// Use local manager's project grouping and merge remote sessions.
	projects := a.localMgr.Projects()
	activity := a.getWindowActivity()
	return buildProjectItems(projects, a.cachedPending, activity)
}

func (a *guiCompositeAdapter) ToggleProjectExpanded(projectID string) {
	a.localMgr.ToggleProjectExpanded(projectID)
}

func (a *guiCompositeAdapter) Create(path string) error {
	host := a.pendingHost
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
// optimistic session creation. On failure it marks the placeholder as
// dead and stores the error message. On success it maps the placeholder
// to the real remote session for preview routing.
func (a *guiCompositeAdapter) completeRemoteCreate(placeholderID, localPath, host string) {
	if err := a.ensureRemoteConnected(host); err != nil {
		a.failPlaceholder(placeholderID, fmt.Sprintf("Connection failed: %v", err))
		return
	}

	// Resolve the local path to the remote CWD now that the connection exists.
	remotePath := a.resolveRemotePath(localPath, host)

	if err := a.cp.Create(remotePath, host); err != nil {
		a.failPlaceholder(placeholderID, fmt.Sprintf("Session creation failed: %v", err))
		return
	}

	// Update the placeholder's path to the resolved remote path.
	a.localMgr.Store().Rename(placeholderID, a.localMgr.Store().GenerateName(remotePath))
	// Note: Store.Rename only changes the name. We need to update Path too.
	// Since Store doesn't expose a SetPath, we re-use SetStatus + path stays as-is
	// (the name is sufficient for sidebar display).

	// Find the newly created remote session and map it to the placeholder.
	sessions, err := a.cp.Sessions()
	if err == nil {
		for _, s := range sessions {
			if s.Host == host && s.Path == remotePath && s.ID != placeholderID {
				a.setRemoteMapping(placeholderID, s.ID)
				break
			}
		}
	}
	a.triggerGUIUpdate()
}

// failPlaceholder marks a placeholder session as dead and stores the error.
func (a *guiCompositeAdapter) failPlaceholder(id, msg string) {
	a.setSessionError(id, msg)
	a.localMgr.Store().SetStatus(id, session.StatusDead)
	if err := a.localMgr.Store().Save(); err != nil && a.onError != nil {
		a.onError(fmt.Sprintf("save store: %v", err))
	}
	if a.onError != nil {
		a.onError(msg)
	}
	a.triggerGUIUpdate()
}

// setSessionError records an error message for a placeholder session.
func (a *guiCompositeAdapter) setSessionError(id, msg string) {
	a.optimisticMu.Lock()
	defer a.optimisticMu.Unlock()
	if a.sessionErrors == nil {
		a.sessionErrors = make(map[string]string)
	}
	a.sessionErrors[id] = msg
}

// sessionError returns the error message for a session, or "".
func (a *guiCompositeAdapter) sessionError(id string) string {
	a.optimisticMu.Lock()
	defer a.optimisticMu.Unlock()
	return a.sessionErrors[id]
}

// setRemoteMapping maps a placeholder to the real remote session.
func (a *guiCompositeAdapter) setRemoteMapping(placeholderID, remoteID string) {
	a.optimisticMu.Lock()
	defer a.optimisticMu.Unlock()
	if a.remoteSessionMap == nil {
		a.remoteSessionMap = make(map[string]string)
	}
	a.remoteSessionMap[placeholderID] = remoteID
}

// remoteMapping returns the real remote session ID for a placeholder, or "".
func (a *guiCompositeAdapter) remoteMapping(id string) string {
	a.optimisticMu.Lock()
	defer a.optimisticMu.Unlock()
	return a.remoteSessionMap[id]
}

// clearOptimistic removes all optimistic state for a session ID.
func (a *guiCompositeAdapter) clearOptimistic(id string) {
	a.optimisticMu.Lock()
	defer a.optimisticMu.Unlock()
	delete(a.sessionErrors, id)
	delete(a.remoteSessionMap, id)
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
	// "." comes from CreateSessionAtCWD (N key).
	// localProjectRoot match comes from CreateSession (n key) when no
	// remote sessions exist yet and currentProjectRoot() falls back to
	// the local working directory.
	if path != "." && path != a.localProjectRoot {
		return path
	}

	// Query remote daemon for its working directory.
	remoteCWD := a.queryRemoteCWD(host)
	if remoteCWD != "" {
		return remoteCWD
	}
	return path
}

// cwdQueryTimeout is the maximum time to wait for a remote CWD query.
const cwdQueryTimeout = 10 * time.Second

// queryRemoteCWD fetches the working directory from a connected remote daemon.
// Returns "" if the query fails (caller should fall back to the original path).
func (a *guiCompositeAdapter) queryRemoteCWD(host string) string {
	provider := a.cp.RemoteProvider(host)
	if provider == nil {
		return ""
	}
	querier, ok := provider.(daemon.CWDQuerier)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), cwdQueryTimeout)
	defer cancel()
	cwd, err := querier.QueryCWD(ctx)
	if err != nil {
		return ""
	}
	return cwd
}

func (a *guiCompositeAdapter) Delete(id string) error {
	a.clearOptimistic(id)
	return a.cp.Delete(id)
}

func (a *guiCompositeAdapter) Rename(id, newName string) error {
	return a.cp.Rename(id, newName)
}

func (a *guiCompositeAdapter) PurgeOrphans() (int, error) {
	return a.cp.PurgeOrphans()
}

func (a *guiCompositeAdapter) CapturePreview(id string, width, height int) (gui.PreviewResult, error) {
	// Optimistic placeholder with error: return error as preview content.
	if errMsg := a.sessionError(id); errMsg != "" {
		return gui.PreviewResult{Content: "\n  " + errMsg}, nil
	}

	// Optimistic placeholder mapped to a real remote session: route to
	// the remote session for preview capture.
	if remoteID := a.remoteMapping(id); remoteID != "" {
		id = remoteID
	}

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
	if remoteID := a.remoteMapping(id); remoteID != "" {
		id = remoteID
	}
	resp, err := a.cp.CaptureScrollback(id, width, startLine, endLine)
	if err != nil || resp == nil {
		return gui.PreviewResult{}, err
	}
	return gui.PreviewResult{Content: resp.Content}, nil
}

func (a *guiCompositeAdapter) HistorySize(id string) (int, error) {
	if remoteID := a.remoteMapping(id); remoteID != "" {
		id = remoteID
	}
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
	if remoteID := a.remoteMapping(id); remoteID != "" {
		id = remoteID
	}
	return a.cp.AttachSession(id)
}

func (a *guiCompositeAdapter) LaunchLazygit(path string) error {
	return a.cp.LaunchLazygit(path, "")
}

func (a *guiCompositeAdapter) CreateWorktree(name, prompt, projectRoot string) error {
	host := a.pendingHost
	if err := a.ensureRemoteConnected(host); err != nil {
		return err
	}
	if host != "" {
		projectRoot = a.resolveRemotePath(projectRoot, host)
	}
	return a.cp.CreateWorktree(name, prompt, projectRoot, host)
}

func (a *guiCompositeAdapter) ResumeWorktree(worktreePath, prompt, projectRoot string) error {
	host := a.pendingHost
	if err := a.ensureRemoteConnected(host); err != nil {
		return err
	}
	if host != "" {
		projectRoot = a.resolveRemotePath(projectRoot, host)
	}
	return a.cp.ResumeWorktree(worktreePath, prompt, projectRoot, host)
}

func (a *guiCompositeAdapter) ListWorktrees(projectRoot string) ([]gui.WorktreeInfo, error) {
	host := a.pendingHost
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
	host := a.pendingHost
	if err := a.ensureRemoteConnected(host); err != nil {
		return err
	}
	if host != "" {
		projectRoot = a.resolveRemotePath(projectRoot, host)
	}
	return a.cp.CreatePMSession(projectRoot, host)
}

func (a *guiCompositeAdapter) CreateWorkerSession(name, prompt, projectRoot string) error {
	host := a.pendingHost
	if err := a.ensureRemoteConnected(host); err != nil {
		return err
	}
	if host != "" {
		projectRoot = a.resolveRemotePath(projectRoot, host)
	}
	return a.cp.CreateWorkerSession(name, prompt, projectRoot, host)
}

// daemonInfoToGUIItem converts daemon.SessionInfo to gui.SessionItem.
func daemonInfoToGUIItem(s daemon.SessionInfo, pending map[string]bool, windowActivity map[string]gui.WindowActivityEntry) gui.SessionItem {
	activity := model.ActivityUnknown
	toolName := ""

	if s.Status == "running" {
		if wa, ok := windowActivity[s.TmuxWindow]; ok {
			activity = wa.State
			toolName = wa.ToolName
		}
	}

	if s.Status == "running" && pending[s.TmuxWindow] {
		activity = model.ActivityNeedsInput
	}

	return gui.SessionItem{
		ID:         s.ID,
		Name:       s.Name,
		Path:       s.Path,
		Host:       s.Host,
		Status:     s.Status,
		Flags:      s.Flags,
		TmuxWindow: s.TmuxWindow,
		Activity:   activity,
		ToolName:   toolName,
		Role:       s.Role,
	}
}
