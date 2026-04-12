package main

import (
	"context"
	"sync"
	"time"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/any-context/lazyclaude/internal/gui"
	"github.com/any-context/lazyclaude/internal/notify"
	"github.com/any-context/lazyclaude/internal/session"
)

// sessionCommander is the subset of SessionCommandService methods invoked by
// guiCompositeAdapter. Defining the dependency as an interface here lets tests
// inject a mock to verify routing decisions without standing up a real
// daemon/CompositeProvider.
type sessionCommander interface {
	Create(target OperationTarget) error
	Delete(id string) error
	Rename(id, newName string) error
	LaunchLazygit(target OperationTarget) error
	CreateWorktree(target OperationTarget, name, prompt string) error
	ResumeWorktree(target OperationTarget, wtPath, prompt string) error
	ListWorktrees(target OperationTarget) ([]gui.WorktreeInfo, error)
	CreatePMSession(target OperationTarget) error
	CreateWorkerSession(target OperationTarget, name, prompt string) error
}

// Compile-time check that SessionCommandService satisfies sessionCommander.
var _ sessionCommander = (*SessionCommandService)(nil)

// guiCompositeAdapter wraps daemon.CompositeProvider to implement gui.SessionProvider.
// This bridges the daemon's type system (daemon.SessionInfo etc.) to the GUI's
// type system (gui.SessionItem etc.).
type guiCompositeAdapter struct {
	cp       *daemon.CompositeProvider
	localMgr *session.Manager
	paths    config.Paths
	commands sessionCommander

	// windowActivityFn provides window->activity mapping from the App layer.
	windowActivityFn func() map[string]gui.WindowActivityEntry

	// cachedPending is refreshed once per layout cycle.
	cachedPending map[string]bool

	// currentHostFn returns the SSH host of the currently selected tree node
	// along with a flag indicating whether the cursor is on a node. The flag
	// lets resolveHost() distinguish "cursor on a local node" (host="", onNode=true)
	// from "no node selected" (host="", onNode=false).
	// Wired from app.CurrentSessionHost() in root.go.
	currentHostFn func() (string, bool)

	// Lazy remote connection: pendingHost is the default SSH host, initially set
	// at construction from DetectSSHHost() and updated by SetPendingHost after
	// successful connect-dialog connections. Protected by hostMu for thread safety.
	// RWMutex because reads (every operation) vastly outnumber writes (connect dialog).
	hostMu           sync.RWMutex
	pendingHost      string // Default SSH host (updated after connect dialog)
	localProjectRoot string // Local project root at startup (immutable after construction)

	// onError reports errors to the GUI via showError. Wired in root.go.
	// lastErrorMsg deduplicates consecutive identical errors to avoid flooding
	// the GUI when Sessions() fails persistently (e.g. daemon unreachable).
	onError      func(msg string)
	lastErrorMsg string

	// cachedHost and cachedOnNode store the most recently resolved host info
	// from currentHostFn. Updated on every layout cycle (main goroutine) via
	// Sessions(). Read by resolveHost() from any goroutine.
	// The zero value (cachedOnNode=false) is intentional: before the first
	// Sessions() call, resolveHost() must treat the state as "no node under
	// cursor" and fall back to pendingHost.
	hostCacheMu  sync.RWMutex
	cachedHost   string
	cachedOnNode bool
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

// resolveHost returns the SSH host for the current operation.
//
// Routing rules:
//   - Cursor on a node: return the node's host verbatim. Host=="" means the
//     node is local, and the operation must stay local (no pendingHost fallback).
//   - Cursor not on a node: fall back to pendingHost (set by connect dialog or
//     DetectSSHHost at startup).
//
// Thread-safe: reads cachedHost/cachedOnNode updated by Sessions() each layout
// cycle. May return a stale value between cycles, acceptable because operations
// are synchronous on the calling goroutine.
func (a *guiCompositeAdapter) resolveHost() string {
	a.hostCacheMu.RLock()
	h := a.cachedHost
	onNode := a.cachedOnNode
	a.hostCacheMu.RUnlock()
	if onNode {
		return h
	}
	return a.readPendingHost()
}

func (a *guiCompositeAdapter) RefreshPendingFrom(notifications []*model.ToolNotification) {
	a.cachedPending = pendingWindowSet(notifications)
}

func (a *guiCompositeAdapter) Sessions() []gui.SessionItem {
	// Update cached host on every layout cycle (main goroutine).
	if a.currentHostFn != nil {
		h, onNode := a.currentHostFn()
		a.hostCacheMu.Lock()
		a.cachedHost = h
		a.cachedOnNode = onNode
		a.hostCacheMu.Unlock()
	}

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
	return a.commands.Create(a.resolveTarget(path))
}

// CreateAtPaneCWD implements the N key: create a session in the lazyclaude
// pane's CWD. Host routing is pane-based, so we use pendingHost directly
// instead of resolveHost(): the cursor's tree node must not influence where
// a pane-CWD session lands. The resolveRemotePathFn in SessionCommandService
// translates "." to the remote daemon's CWD when the host is non-empty.
func (a *guiCompositeAdapter) CreateAtPaneCWD() error {
	return a.commands.Create(OperationTarget{
		Host:        a.readPendingHost(),
		ProjectRoot: ".",
	})
}

// resolveRemotePath maps a local path to the remote daemon's CWD when
// creating the first session on an SSH host. Local-origin paths (the
// local project root from the startup fallback, or ".") are meaningless
// on the remote machine and must be translated via the daemon GET /cwd
// API. Any other path is assumed to be an existing remote project path
// (e.g. from the session tree) and is returned unchanged.
//
// Querying the remote CWD requires the remote connection to be established
// first (call ensureRemoteConnected before this method).
func (a *guiCompositeAdapter) resolveRemotePath(path, host string) string {
	debugLog("resolveRemotePath: input=%q host=%q localProjectRoot=%q", path, host, a.localProjectRoot)
	// Treat a path as local-origin only if it is "." or matches a known
	// local project root. When localProjectRoot is unset (zero value), the
	// only local marker we can trust is ".", so any other path is passed
	// through unchanged.
	isLocalOrigin := path == "." || (a.localProjectRoot != "" && path == a.localProjectRoot)
	if !isLocalOrigin {
		debugLog("resolveRemotePath: output=%q (passthrough, already remote)", path)
		return path
	}
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
	return a.commands.Delete(id)
}

func (a *guiCompositeAdapter) Rename(id, newName string) error {
	return a.commands.Rename(id, newName)
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
	// Local notifications from file system (written by hooks when broker
	// is not active). When the in-process broker is wired, the server
	// publishes to the broker instead of writing files, so ReadAll
	// returns empty — no duplicates.
	var result []*model.ToolNotification
	if local, err := notify.ReadAll(a.paths.RuntimeDir); err != nil {
		debugLog("PendingNotifications: ReadAll error: %v", err)
	} else {
		result = append(result, local...)
	}

	// Remote notifications buffered by SSE in each RemoteProvider.
	// Window names are remapped from lc-xxxx to rm-xxxx by CompositeProvider.
	result = append(result, a.cp.PendingNotifications()...)

	if len(result) == 0 {
		return nil
	}
	return result
}

func (a *guiCompositeAdapter) SendChoice(window string, c gui.Choice) error {
	return a.cp.SendChoice(window, int(c))
}

func (a *guiCompositeAdapter) AttachSession(id string) error {
	return a.cp.AttachSession(id)
}

func (a *guiCompositeAdapter) LaunchLazygit(path string) error {
	return a.commands.LaunchLazygit(a.resolveTarget(path))
}

func (a *guiCompositeAdapter) CreateWorktree(name, prompt, projectRoot string) error {
	return a.commands.CreateWorktree(a.resolveTarget(projectRoot), name, prompt)
}

func (a *guiCompositeAdapter) ResumeWorktree(worktreePath, prompt, projectRoot string) error {
	return a.commands.ResumeWorktree(a.resolveTarget(projectRoot), worktreePath, prompt)
}

func (a *guiCompositeAdapter) ListWorktrees(projectRoot string) ([]gui.WorktreeInfo, error) {
	return a.commands.ListWorktrees(a.resolveTarget(projectRoot))
}

func (a *guiCompositeAdapter) CreatePMSession(projectRoot string) error {
	return a.commands.CreatePMSession(a.resolveTarget(projectRoot))
}

func (a *guiCompositeAdapter) CreateWorkerSession(name, prompt, projectRoot string) error {
	return a.commands.CreateWorkerSession(a.resolveTarget(projectRoot), name, prompt)
}

// resolveTarget builds an OperationTarget from a project root path.
// Determines the host from the cached/pending host and resolves the
// path to the remote CWD if operating on a remote host.
func (a *guiCompositeAdapter) resolveTarget(projectRoot string) OperationTarget {
	return OperationTarget{Host: a.resolveHost(), ProjectRoot: projectRoot}
}
