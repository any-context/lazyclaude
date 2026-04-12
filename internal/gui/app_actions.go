package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/any-context/lazyclaude/internal/core/choice"
	"github.com/any-context/lazyclaude/internal/gui/keyhandler"
	"github.com/any-context/lazyclaude/internal/gui/keymap"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/jesseduffield/gocui"
)

// Compile-time check: *App implements keyhandler.AppActions.
var _ keyhandler.AppActions = (*App)(nil)

// --- State queries ---

func (a *App) HasPopup() bool    { return a.hasPopup() }
func (a *App) IsFullScreen() bool { return a.fullscreen.IsActive() }

// --- Tree helpers ---

// refreshTreeNodes rebuilds the flat tree node list from projects.
// Called once per layout cycle to ensure consistency within a render pass.
func (a *App) refreshTreeNodes() {
	if a.sessions == nil {
		a.cachedNodes = nil
		return
	}
	a.cachedNodes = BuildTreeNodes(a.sessions.Projects())
}

// treeNodes returns the tree node list, filtered when search is active.
func (a *App) treeNodes() []TreeNode {
	return a.filteredTreeNodes()
}

// currentNode returns the tree node at the cursor, or nil if out of bounds.
func (a *App) currentNode() *TreeNode {
	nodes := a.filteredTreeNodes()
	if a.cursor < 0 || a.cursor >= len(nodes) {
		return nil
	}
	return &nodes[a.cursor]
}

// currentSession returns the session at the cursor, or nil if cursor is on a project.
func (a *App) currentSession() *SessionItem {
	node := a.currentNode()
	if node == nil || node.Kind != SessionNode {
		return nil
	}
	return node.Session
}

// --- Tree operations ---

func (a *App) ToggleProjectExpanded() {
	node := a.currentNode()
	if node == nil || node.Kind != ProjectNode {
		return
	}
	a.sessions.ToggleProjectExpanded(node.ProjectID)
	// Rebuild cache immediately so cursor bounds are correct.
	a.refreshTreeNodes()
}

func (a *App) CollapseProject() {
	node := a.currentNode()
	if node == nil || node.Kind != ProjectNode {
		return
	}
	if node.Project != nil && node.Project.Expanded {
		a.sessions.ToggleProjectExpanded(node.ProjectID)
		a.refreshTreeNodes()
	}
}

func (a *App) ExpandProject() {
	node := a.currentNode()
	if node == nil || node.Kind != ProjectNode {
		return
	}
	if node.Project != nil && !node.Project.Expanded {
		a.sessions.ToggleProjectExpanded(node.ProjectID)
		a.refreshTreeNodes()
	}
}

func (a *App) CursorIsProject() bool {
	node := a.currentNode()
	return node != nil && node.Kind == ProjectNode
}

// --- Session cursor ---

func (a *App) MoveCursorDown() {
	if a.sessions == nil {
		return
	}
	nodes := a.treeNodes()
	if a.cursor < len(nodes)-1 {
		a.cursor++
	}
	a.clearError()
	a.syncPluginProject()
}

func (a *App) MoveCursorUp() {
	if a.cursor > 0 {
		a.cursor--
	}
	a.clearError()
	a.syncPluginProject()
}

// syncPluginProjectOnce triggers the first plugin load during layout.
// Tries to derive the project path from the session tree; if no sessions
// exist yet, falls back to the process working directory so that installed
// plugins are displayed immediately.
func (a *App) syncPluginProjectOnce() {
	if a.plugins == nil || a.pluginState.projectDir != "" {
		return
	}

	// Try session tree first (preferred: project-scoped context).
	// syncPluginProject handles both local and remote nodes: for remote
	// it sets pluginState.remoteDisabled without touching projectDir,
	// for local it sets projectDir and triggers the async refresh.
	// Either signal is enough to short-circuit before the CWD fallback.
	node := a.currentNode()
	if node != nil {
		a.syncPluginProject()
		if a.pluginState.projectDir != "" || a.pluginState.remoteDisabled {
			return
		}
	}

	// Fallback: no sessions yet — use process CWD so plugins load immediately.
	// Explicitly clear remoteDisabled: we are going to refresh against local
	// data, so the panels must leave any prior "remote disabled" state even
	// if the caller had a remote node selected before landing here.
	a.pluginState.remoteDisabled = false
	if a.mcpServers != nil {
		a.mcpState.remoteDisabled = false
		a.mcpState.remoteKey = ""
	}
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Refresh(ctx)
	})
	if a.mcpServers != nil {
		cwd, _ := filepath.Abs(".")
		// Atomically install the local target so any in-flight async
		// goroutine spawned from a previous remote selection cannot
		// observe a (host, projectDir) mixed pair mid-swap.
		a.mcpServers.SetRemote("", cwd)
		a.runMCPAsync(func(ctx context.Context) error {
			return a.mcpServers.Refresh(ctx)
		})
	}
	a.pluginState.projectDir = "." // mark as initialised to prevent re-entry
}

// syncPluginProject updates the plugin panel's project context
// based on the currently selected session/project in the tree.
func (a *App) syncPluginProject() {
	if a.plugins == nil {
		return
	}

	// clearRemoteDisabled resets the plugin remoteDisabled flag and
	// the MCP dedupe cache. It deliberately does NOT touch the MCP
	// provider's host/projectDir — the caller path is responsible for
	// calling SetRemote atomically with the final target so that no
	// async goroutine can observe a mid-swap mixed pair.
	clearRemoteDisabled := func() {
		a.pluginState.remoteDisabled = false
		if a.mcpServers != nil {
			a.mcpState.remoteDisabled = false
			a.mcpState.remoteKey = ""
		}
	}

	node := a.currentNode()
	if node == nil {
		// Recovery path — only when the underlying tree is genuinely
		// empty (not a transient filter-hides-everything or cursor-
		// out-of-range state). Reset the providers to the CWD fallback
		// so a cached pluginState.projectDir from an earlier local
		// selection cannot bleed through into subsequent writes.
		// Without this reset, the flow (local A → remote B → tree
		// empties out-of-band) would leave pluginState.projectDir at
		// "A" and the next plugin/MCP write would mutate an unrelated
		// local repo. See codex P1 on commit a25ed88 for the scenario.
		//
		// Idempotent by the projectDir-equals-CWD short-circuit: once
		// the reset has run the first time, subsequent layout passes
		// observe projectDir == cwd and skip the re-spawn.
		if len(a.cachedNodes) == 0 {
			// Always clear the remoteDisabled flags on empty-tree
			// recovery: even if projectDir already matches cwd (the
			// user started lazyclaude in the lazyclaude repo, for
			// instance), leaving the flags set would pin the panels
			// to the remote placeholder and make guardRemoteOp reject
			// local plugin/MCP actions forever.
			clearRemoteDisabled()

			cwd, _ := filepath.Abs(".")

			// Atomic SetRemote is unconditional: even when
			// pluginState.projectDir already equals cwd (user
			// started lazyclaude in the fallback directory), the
			// MCP manager may still hold a stale (remoteHost,
			// remoteDir) pair from a prior remote selection.
			// Without this reset the nil-node fallback in
			// guardRemoteOp would permit an MCP toggle that
			// ultimately targets the old remote file.
			if a.mcpServers != nil {
				a.mcpServers.SetRemote("", cwd)
			}

			if a.pluginState.projectDir != cwd {
				// Panel cursors track the previous project's item
				// count; swapping to the CWD fallback without
				// zeroing them can leave installedCursor /
				// marketCursor / mcpState.cursor out of range,
				// which silently blocks write handlers that
				// short-circuit on `cursor >= len(...)`. Mirror
				// the local-node branch below.
				a.pluginState.installedCursor = 0
				a.pluginState.marketCursor = 0
				a.pluginState.projectDir = cwd
				a.plugins.SetProjectDir(cwd)
				a.runPluginAsync(func(ctx context.Context) error {
					return a.plugins.Refresh(ctx)
				})
				if a.mcpServers != nil {
					a.mcpState.cursor = 0
					a.runMCPAsync(func(ctx context.Context) error {
						return a.mcpServers.Refresh(ctx)
					})
				}
			}
		}
		// Transient nil-node (filter hides everything, cursor out of
		// range): preserve the previous flag and projectDir so the
		// logical selection is intact. The write guards' flag fallback
		// keeps writes honest until the tree resolves.
		return
	}

	// Remote node.
	//   - Plugin panel: stays disabled (Phase 3 will SSH-wrap the
	//     `claude plugins` CLI). We intentionally do NOT clear
	//     pluginState.projectDir or call SetProjectDir("") — see the
	//     Phase 1 rationale below.
	//   - MCP panel: Phase 2 drives the provider through its SSH code
	//     path. SetRemote atomically flips the manager into remote
	//     mode and runMCPAsync loads the remote server list.
	if host, isRemote := a.isRemoteNodeSelected(); isRemote {
		a.pluginState.remoteDisabled = true
		if a.mcpServers != nil {
			a.mcpState.remoteDisabled = false

			var remoteProjectPath string
			if node.Kind == ProjectNode && node.Project != nil {
				remoteProjectPath = node.Project.Path
			} else if node.Session != nil {
				remoteProjectPath = a.configDirForSession(node.Session)
			}

			// Dedupe: avoid respam on cursor moves within the same
			// remote project. Every MoveCursorUp/Down triggers this
			// sync, so kicking off an SSH round-trip unconditionally
			// would hammer the remote host.
			key := host + "|" + remoteProjectPath
			if remoteProjectPath != "" && a.mcpState.remoteKey != key {
				a.mcpState.remoteKey = key
				a.mcpState.cursor = 0
				// Atomic: one lock acquisition installs both
				// host and projectDir, so a racing async
				// goroutine cannot observe a mixed pair.
				a.mcpServers.SetRemote(host, remoteProjectPath)
				a.runMCPAsync(func(ctx context.Context) error {
					return a.mcpServers.Refresh(ctx)
				})
			}
		}
		return
	}

	// Local node: clear the remote flag and proceed with the existing refresh.
	clearRemoteDisabled()

	var projectPath string
	if node.Kind == ProjectNode && node.Project != nil {
		projectPath = node.Project.Path
	} else if node.Session != nil {
		projectPath = a.configDirForSession(node.Session)
	}
	if projectPath == "" {
		return
	}

	// Atomically install the local MCP target BEFORE the refresh
	// short-circuit. Even when projectPath matches the cached
	// pluginState.projectDir (local-to-same-local cursor movement, or
	// remote→local where the cache points to this local project),
	// this guarantees the provider's (host, projectDir) pair is
	// consistent with the live cursor. Without the unconditional
	// SetRemote here, a remote→local transition into a cached
	// project would leave the manager holding (remoteHost, remoteDir)
	// — a subsequent MCPRefresh / MCPToggleDenied would then target
	// the wrong machine.
	if a.mcpServers != nil {
		a.mcpServers.SetRemote("", projectPath)
	}

	if projectPath == a.pluginState.projectDir {
		return
	}
	a.pluginState.projectDir = projectPath
	a.pluginState.installedCursor = 0
	a.pluginState.marketCursor = 0
	a.plugins.SetProjectDir(projectPath)
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Refresh(ctx)
	})

	if a.mcpServers != nil {
		a.mcpState.cursor = 0
		a.runMCPAsync(func(ctx context.Context) error {
			return a.mcpServers.Refresh(ctx)
		})
	}
}

// isRemoteNodeSelected reports whether the cursor is on a remote (SSH) node.
// Returns (host, true) when the cursor is on a remote session/project,
// ("", false) otherwise. Wraps currentSessionHost() so callers do not need
// to interpret its (host, onNode) return shape.
func (a *App) isRemoteNodeSelected() (string, bool) {
	host, onNode := a.currentSessionHost()
	if !onNode || host == "" {
		return "", false
	}
	return host, true
}

// guardRemoteOp short-circuits a write handler when the cursor is on a
// remote node, showing a status message. Returns true if the caller should
// return early.
//
// Decision order:
//
//  1. Live local node → false (allow).
//  2. Live remote node → true (block, status message).
//  3. Nil node (filter hid every row, cursor briefly out of range) →
//     fall back to pluginState.remoteDisabled / mcpState.remoteDisabled.
//     When set, block; when clear, allow.
//
// Callers MUST have synced the panel state to the current cursor
// before invoking writes — this is done automatically by the standard
// cursor-moving paths (MoveCursorDown/Up, applySearchFilter,
// closeSearch Esc restore, moveCursorToLastSession). Do NOT call
// syncPluginProject from inside guardRemoteOp: the refresh it spawns
// is asynchronous, so a write handler running immediately afterwards
// would read stale cached Installed()/Servers() data from the previous
// project and mutate items that no longer exist in the new context.
//
// The caller sites are AppActions methods invoked by the keydispatch
// layer and do not receive a *gocui.Gui. setStatus requires a gui to
// find the status view, so we re-enter the main goroutine via
// gui.Update. This is the same pattern runPluginAsync / runMCPAsync
// use for their own status writes and is consistent with the
// plan-mandated wrapper shape.
func (a *App) guardRemoteOp(feature string) bool {
	host, onNode := a.currentSessionHost()

	switch {
	case onNode && host == "":
		// Live local node — authoritative, ignore the cached flag.
		return false
	case onNode && host != "":
		// Live remote node — guard regardless of flag state.
	default:
		// No resolvable node: fall back to the cached panel flag.
		if !a.pluginState.remoteDisabled && !a.mcpState.remoteDisabled {
			return false
		}
		host = "remote"
	}

	msg := fmt.Sprintf("%s on remote (%s) is not supported yet", feature, host)
	a.gui.Update(func(g *gocui.Gui) error {
		a.setStatus(g, msg)
		return nil
	})
	return true
}

// --- Path helpers ---

// currentProjectRoot returns the project root path for the currently selected
// tree node. For ProjectNode, returns Project.Path directly. For SessionNode,
// looks up the parent project's stored Path (avoids InferProjectRoot which can
// mismatch on remote when paths differ from the stored project path).
// Falls back to filepath.Abs(".") when no node is selected.
func (a *App) currentProjectRoot() string {
	node := a.currentNode()
	if node != nil {
		switch node.Kind {
		case ProjectNode:
			if node.Project != nil && node.Project.Path != "" {
				return node.Project.Path
			}
		case SessionNode:
			// Look up the parent project's stored path instead of inferring
			// from the session's worktree path. This prevents mismatches when
			// the stored project path differs from InferProjectRoot output
			// (e.g. relative "." vs absolute, or symlink-resolved paths).
			if path := a.projectPathByID(node.ProjectID); path != "" {
				return path
			}
			if node.Session != nil && node.Session.Path != "" {
				return session.InferProjectRoot(node.Session.Path)
			}
		}
	}
	abs, err := filepath.Abs(".")
	if err != nil {
		return "."
	}
	return session.InferProjectRoot(abs)
}

// projectPathByID returns the stored Path for the project with the given ID.
// Returns "" if not found. Scans the current tree nodes which are already
// cached, so this is inexpensive.
func (a *App) projectPathByID(projectID string) string {
	if projectID == "" {
		return ""
	}
	for _, n := range a.treeNodes() {
		if n.Kind == ProjectNode && n.ProjectID == projectID && n.Project != nil {
			return n.Project.Path
		}
	}
	return ""
}

// configDirForSession returns the directory to use for configuration lookups
// (MCP servers, plugins, settings) for the given session.
// Worktree sessions use their worktree path directly so that configuration
// is localized to the worktree. Non-worktree sessions use InferProjectRoot
// to resolve back to the project root.
func (a *App) configDirForSession(s *SessionItem) string {
	if s == nil || s.Path == "" {
		return ""
	}
	if session.IsWorktreePath(s.Path) {
		return s.Path
	}
	return session.InferProjectRoot(s.Path)
}

// --- Host routing ---

// CurrentSessionHost returns the SSH host of the currently selected session
// or project node along with a flag indicating whether the cursor is on a node.
// The host is "" for local sessions/projects, and onNode is false when no node
// is under the cursor. Callers distinguish "on a local node" (host="", onNode=true)
// from "no node selected" (host="", onNode=false) so that local-node operations
// do not fall back to pendingHost.
// Must be called from the gocui main goroutine (e.g. inside keybinding handlers
// or Update callbacks) because it reads GUI state (cursor, tree nodes).
func (a *App) CurrentSessionHost() (string, bool) {
	return a.currentSessionHost()
}

func (a *App) currentSessionHost() (string, bool) {
	node := a.currentNode()
	if node == nil {
		return "", false
	}
	switch node.Kind {
	case SessionNode:
		if node.Session != nil {
			return node.Session.Host, true
		}
	case ProjectNode:
		if node.Project != nil {
			return node.Project.Host, true
		}
	}
	// Defensive: a node with a nil payload should not be treated as
	// "cursor on a local node" — fall through to pendingHost instead.
	return "", false
}

// --- Session operations ---

func (a *App) CreateSession() { a.createSession(a.currentProjectRoot()) }

// CreateSessionAtCWD creates a session in the lazyclaude pane's CWD. Unlike
// CreateSession, routing is pane-based, not cursor-based: it delegates to
// sessions.CreateAtPaneCWD() which uses pendingHost rather than the cursor's
// tree node host. This keeps N predictable regardless of cursor position.
func (a *App) CreateSessionAtCWD() {
	if a.sessions == nil || a.HasActiveDialog() {
		return
	}
	debugLog("CreateSessionAtCWD")
	go func() {
		err := a.sessions.CreateAtPaneCWD()
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.showError(g, fmt.Sprintf("Error: %v", err))
			} else {
				a.setStatus(g, "Session created")
				a.moveCursorToLastSession()
			}
			return nil
		})
	}()
}

// createSession is the shared implementation for CreateSession.
// localPath is the fallback directory for non-SSH sessions.
// Routes to the host of the currently selected tree node. Falls back to
// pendingHost (inside the adapter) when no node is selected.
// Runs asynchronously to avoid blocking the GUI thread during remote operations.
func (a *App) createSession(localPath string) {
	if a.sessions == nil || a.HasActiveDialog() {
		return
	}
	debugLog("createSession: path=%q", localPath)
	go func() {
		err := a.sessions.Create(localPath)
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.showError(g, fmt.Sprintf("Error: %v", err))
			} else {
				a.setStatus(g, "Session created")
				a.moveCursorToLastSession()
			}
			return nil
		})
	}()
}

// moveCursorToLastSession moves the cursor to the last session node in the
// tree. Used after session creation to select the newly created session.
// Re-syncs the plugin/MCP panels so their remoteDisabled flags and
// cached project path follow the newly selected session — the write
// guards rely on the panel state matching the cursor.
func (a *App) moveCursorToLastSession() {
	a.refreshTreeNodes()
	nodes := a.treeNodes()
	for i := len(nodes) - 1; i >= 0; i-- {
		if nodes[i].Kind == SessionNode {
			a.cursor = i
			a.syncPluginProject()
			return
		}
	}
}

func (a *App) DeleteSession() {
	if a.sessions == nil || a.HasActiveDialog() {
		return
	}
	sess := a.currentSession()
	if sess == nil {
		return
	}
	sessID := sess.ID
	go func() {
		err := a.sessions.Delete(sessID)
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.showError(g, fmt.Sprintf("Error: %v", err))
			} else {
				nodes := a.treeNodes()
				if a.cursor > 0 && a.cursor >= len(nodes) {
					a.cursor--
				}
				// Re-sync the plugin/MCP panels: deleting the last
				// session in a project pulls the cursor onto a
				// neighbouring node which may belong to a different
				// project (or host). The write guards need the panel
				// state to track that jump.
				a.syncPluginProject()
				a.setStatus(g, "Session deleted")
			}
			return nil
		})
	}()
}

func (a *App) LaunchLazygit() {
	if a.sessions == nil {
		return
	}
	s := a.currentSession()
	if s == nil {
		return
	}
	sess := *s
	g := a.gui
	if err := g.Suspend(); err != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.showError(g, fmt.Sprintf("Suspend error: %v", err))
			return nil
		})
		return
	}
	launchErr := a.sessions.LaunchLazygit(sess.Path)
	if err := g.Resume(); err != nil {
		return
	}
	if launchErr != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.showError(g, fmt.Sprintf("lazygit error: %v", launchErr))
			return nil
		})
	}
}

func (a *App) AttachSession() {
	if a.sessions == nil {
		return
	}
	sess := a.currentSession()
	if sess == nil {
		return
	}
	g := a.gui
	if err := g.Suspend(); err != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.showError(g, fmt.Sprintf("Suspend error: %v", err))
			return nil
		})
		return
	}
	attachErr := a.sessions.AttachSession(sess.ID)
	if err := g.Resume(); err != nil {
		return
	}
	if attachErr != nil {
		a.gui.Update(func(g *gocui.Gui) error {
			a.showError(g, fmt.Sprintf("Attach error: %v", attachErr))
			return nil
		})
	}
}

func (a *App) EnterFullScreen() {
	if a.sessions == nil {
		return
	}
	sess := a.currentSession()
	if sess == nil {
		return
	}
	a.enterFullScreen(sess.ID)
}

func (a *App) DismissError() {
	a.clearError()
}

func (a *App) CopyError() {
	if a.errorMsg == "" {
		return
	}
	copyToClipboard(a.errorMsg)
}

func (a *App) StartRename() {
	if a.sessions == nil || a.dialog.RenameID != "" {
		return
	}
	sess := a.currentSession()
	if sess == nil {
		return
	}
	a.dialog.RenameID = sess.ID
	name := sess.Name
	a.gui.Update(func(g *gocui.Gui) error {
		if !a.showRenameInput(g, name) {
			a.dialog.RenameID = ""
		}
		return nil
	})
}

func (a *App) StartPMSession() {
	if a.sessions == nil || a.HasActiveDialog() {
		return
	}
	projectRoot := a.currentProjectRoot()
	debugLog("StartPMSession: projectRoot=%q", projectRoot)
	go func() {
		err := a.sessions.CreatePMSession(projectRoot)
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.showError(g, fmt.Sprintf("PM error: %v", err))
			} else {
				a.setStatus(g, "PM session started")
			}
			return nil
		})
	}()
}

func (a *App) StartWorktreeInput() {
	if a.sessions == nil || a.HasActiveDialog() {
		return
	}
	a.gui.Update(func(g *gocui.Gui) error {
		if !a.showWorktreeDialog(g) {
			a.showError(g, "Error: could not open worktree dialog")
		}
		return nil
	})
}

func (a *App) SelectWorktree() {
	if a.sessions == nil || a.HasActiveDialog() {
		return
	}
	projectRoot := a.currentProjectRoot()
	debugLog("SelectWorktree: projectRoot=%q", projectRoot)
	go func() {
		items, err := a.sessions.ListWorktrees(projectRoot)
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.showError(g, fmt.Sprintf("Error: %v", err))
				return nil
			}
			if len(items) == 0 {
				a.setStatus(g, "No worktrees found")
				return nil
			}
			wtItems := make([]WorktreeInfo, len(items))
			copy(wtItems, items)
			if !a.showWorktreeChooser(g, wtItems) {
				a.showError(g, "Error: could not open worktree chooser")
			}
			return nil
		})
	}()
}

// connectToHost initiates a remote connection to the given host.
// Must be called from the gocui event loop goroutine.
func (a *App) connectToHost(g *gocui.Gui, host string) {
	if a.connectFn == nil {
		a.showError(g, "Remote connection not available")
		return
	}
	debugLog("connectToHost: host=%q", host)
	a.setStatus(g, "Connecting to "+host+"...")
	go func() {
		debugLog("connectToHost: calling connectFn host=%q", host)
		err := a.connectFn(host)
		debugLog("connectToHost: connectFn result: %v", err)
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.showError(g, fmt.Sprintf("Connection failed: %v", err))
			} else {
				a.setStatus(g, "Connected to "+host)
			}
			return nil
		})
	}()
}

func (a *App) ConnectRemote() {
	debugLog("ConnectRemote: triggered hasDialog=%v", a.HasActiveDialog())
	if a.HasActiveDialog() {
		return
	}
	a.gui.Update(func(g *gocui.Gui) error {
		var hosts []string
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			debugLog("ConnectRemote: UserHomeDir error: %v", homeErr)
			a.setStatus(g, fmt.Sprintf("SSH config: %v", homeErr))
		} else {
			parsed, parseErr := ParseSSHHosts(filepath.Join(home, ".ssh", "config"))
			if parseErr != nil {
				debugLog("ConnectRemote: ParseSSHHosts error: %v", parseErr)
				a.setStatus(g, fmt.Sprintf("SSH config read error: %v", parseErr))
			}
			hosts = parsed
		}
		if len(hosts) > 0 {
			if !a.showConnectChooser(g, hosts) {
				a.showError(g, "Error: could not open host chooser")
			}
		} else {
			if !a.showConnectDialog(g) {
				debugLog("ConnectRemote: showConnectDialog failed")
				a.showError(g, "Error: could not open connect dialog")
			} else {
				debugLog("ConnectRemote: dialog opened")
			}
		}
		return nil
	})
}

func (a *App) PurgeOrphans() {
	if a.sessions == nil || a.HasActiveDialog() {
		return
	}
	go func() {
		count, err := a.sessions.PurgeOrphans()
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil {
				a.showError(g, fmt.Sprintf("Error: %v", err))
			} else {
				a.setStatus(g, fmt.Sprintf("Purged %d orphans", count))
			}
			return nil
		})
	}()
}

// --- Popup ---

func (a *App) DismissPopup(c choice.Choice) {
	a.dismissPopup(Choice(c))
}

func (a *App) DismissAllPopups(c choice.Choice) {
	a.dismissAllPopups(Choice(c))
}

func (a *App) SuspendPopups()   { a.suspendAllPopups() }
func (a *App) UnsuspendPopups() {
	if a.popupCount() > 0 && !a.hasPopup() {
		a.unsuspendAll()
	}
}
func (a *App) PopupFocusNext() { a.popupFocusNext() }
func (a *App) PopupFocusPrev() { a.popupFocusPrev() }

func (a *App) PopupScrollDown() {
	p := a.popups.ActivePopup()
	if p == nil {
		return
	}
	vh := p.ViewportHeight()
	if vh <= 0 {
		vh = 20 // fallback before first layout
	}
	if p.ScrollY() < p.MaxScroll(vh) {
		p.SetScrollY(p.ScrollY() + 1)
	}
}

func (a *App) PopupScrollUp() {
	p := a.popups.ActivePopup()
	if p == nil {
		return
	}
	if p.ScrollY() > 0 {
		p.SetScrollY(p.ScrollY() - 1)
	}
}

// --- FullScreen ---

func (a *App) ExitFullScreen() { a.exitFullScreen() }

func (a *App) ForwardSpecialKey(tmuxKey string) {
	a.forwardSpecialKey(tmuxKey)
}

// --- Send key to pane (works without fullscreen) ---

func (a *App) SendKeyToPane(key string) {
	target := a.resolveSessionTarget()
	if target == "" {
		return
	}
	if a.fullscreen.forwarder == nil {
		return
	}
	go func() {
		_ = a.fullscreen.forwarder.ForwardKey(target, key)
	}()
}

// --- Logs ---

func (a *App) LogsCursorDown()    { a.logs.CursorDown() }
func (a *App) LogsCursorUp()      { a.logs.CursorUp() }
func (a *App) LogsCursorToEnd()   { a.logs.ToEnd() }
func (a *App) LogsCursorToTop()   { a.logs.ToTop() }
func (a *App) LogsToggleSelect()  { a.logs.ToggleSelect() }

func (a *App) LogsCopySelection() {
	text := a.logs.CopyText(a.readLogLines())
	if text != "" {
		copyToClipboard(text)
	}
	a.logs.ClearSelection()
}

func (a *App) LogsClear() {
	// Best-effort truncate: the server logger may be writing concurrently
	// via its own *os.File handle, so the file position may be stale after
	// truncation.  This is acceptable for a single-user TUI tool — the next
	// log write will simply start at whatever offset the logger's fd is at.
	if err := os.Truncate(serverLogPath, 0); err != nil && !os.IsNotExist(err) {
		return
	}
	a.logCache = logFileCache{modTime: -1}
	a.logRender = logRenderCache{}
	a.logs.ClearSelection()
	a.logs.SetLineCount(0)
	a.logs.ToTop()
}

// --- Panel tab switching (generic) ---

func (a *App) PanelNextTab() {
	panel := a.panelManager.ActivePanel()
	if panel == nil || panel.TabCount() <= 1 {
		return
	}
	name := panel.Name()
	cur := a.panelTabs[name]
	if cur < panel.TabCount()-1 {
		newTab := cur + 1
		a.panelTabs[name] = newTab
		panel.OnTabChanged(newTab, a)
	}
}

func (a *App) PanelPrevTab() {
	panel := a.panelManager.ActivePanel()
	if panel == nil || panel.TabCount() <= 1 {
		return
	}
	name := panel.Name()
	cur := a.panelTabs[name]
	if cur > 0 {
		newTab := cur - 1
		a.panelTabs[name] = newTab
		panel.OnTabChanged(newTab, a)
	}
}

func (a *App) ActivePanelTabIndex() int {
	panel := a.panelManager.ActivePanel()
	if panel == nil {
		return 0
	}
	return a.panelTabs[panel.Name()]
}

// PluginSetTab sets the active plugin tab index.
// Called from PluginsPanel.OnTabChanged via AppActions interface.
func (a *App) PluginSetTab(tab int) {
	a.pluginState.tabIdx = tab
}

// --- Plugin panel ---

func (a *App) PluginCursorDown() {
	max := a.pluginItemCount() - 1
	cur := a.pluginState.Cursor()
	if cur < max {
		a.pluginState.SetCursor(cur + 1)
	}
}

func (a *App) PluginCursorUp() {
	cur := a.pluginState.Cursor()
	if cur > 0 {
		a.pluginState.SetCursor(cur - 1)
	}
}

func (a *App) PluginInstall() {
	if a.guardRemoteOp("Plugin editing") {
		return
	}
	if a.plugins == nil || a.pluginState.tabIdx != keymap.PluginTabMarketplace {
		return
	}
	avail := a.filteredAvailablePlugins()
	if a.pluginState.marketCursor >= len(avail) {
		return
	}
	pluginID := avail[a.pluginState.marketCursor].PluginID
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Install(ctx, pluginID)
	})
}

func (a *App) PluginUninstall() {
	if a.guardRemoteOp("Plugin editing") {
		return
	}
	if a.plugins == nil || a.pluginState.tabIdx != keymap.PluginTabPlugins {
		return
	}
	installed := a.filteredInstalledPlugins()
	if a.pluginState.installedCursor >= len(installed) {
		return
	}
	p := installed[a.pluginState.installedCursor]
	if p.Scope != "project" {
		a.gui.Update(func(g *gocui.Gui) error {
			a.showError(g, "Only project-scoped plugins can be uninstalled")
			return nil
		})
		return
	}
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Uninstall(ctx, p.ID, p.Scope)
	})
}

func (a *App) PluginToggleEnabled() {
	if a.guardRemoteOp("Plugin editing") {
		return
	}
	if a.plugins == nil || a.pluginState.tabIdx != keymap.PluginTabPlugins {
		return
	}
	installed := a.filteredInstalledPlugins()
	if a.pluginState.installedCursor >= len(installed) {
		return
	}
	p := installed[a.pluginState.installedCursor]
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.ToggleEnabled(ctx, p.ID, p.Scope)
	})
}

func (a *App) PluginUpdate() {
	if a.guardRemoteOp("Plugin editing") {
		return
	}
	if a.plugins == nil || a.pluginState.tabIdx != keymap.PluginTabPlugins {
		return
	}
	installed := a.filteredInstalledPlugins()
	if a.pluginState.installedCursor >= len(installed) {
		return
	}
	pluginID := installed[a.pluginState.installedCursor].ID
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Update(ctx, pluginID)
	})
}

func (a *App) PluginRefresh() {
	if a.guardRemoteOp("Plugin editing") {
		return
	}
	if a.plugins == nil {
		return
	}
	a.runPluginAsync(func(ctx context.Context) error {
		return a.plugins.Refresh(ctx)
	})
}

// runPluginAsync runs a plugin operation asynchronously with loading state management.
func (a *App) runPluginAsync(fn func(ctx context.Context) error) {
	a.pluginState.loading = true
	go func() {
		err := fn(context.Background())
		a.gui.Update(func(g *gocui.Gui) error {
			a.pluginState.loading = false
			if err != nil {
				a.showError(g, fmt.Sprintf("Plugin error: %v", err))
			}
			return nil
		})
	}()
}

// pluginItemCount returns the number of items in the active plugin tab,
// respecting any active search filter.
func (a *App) pluginItemCount() int {
	if a.plugins == nil {
		return 0
	}
	switch a.pluginState.tabIdx {
	case keymap.PluginTabMCP:
		return len(a.filteredMCPServers())
	case keymap.PluginTabMarketplace:
		return len(a.filteredAvailablePlugins())
	default:
		return len(a.filteredInstalledPlugins())
	}
}

// --- MCP panel ---

func (a *App) MCPCursorDown() {
	max := a.mcpItemCount() - 1
	if a.mcpState.cursor < max {
		a.mcpState.cursor++
	}
}

func (a *App) MCPCursorUp() {
	if a.mcpState.cursor > 0 {
		a.mcpState.cursor--
	}
}

func (a *App) MCPToggleDenied() {
	if a.mcpServers == nil || a.pluginState.tabIdx != keymap.PluginTabMCP {
		return
	}
	servers := a.filteredMCPServers()
	if a.mcpState.cursor >= len(servers) {
		return
	}
	name := servers[a.mcpState.cursor].Name
	a.runMCPAsync(func(ctx context.Context) error {
		return a.mcpServers.ToggleDenied(ctx, name)
	})
}

func (a *App) MCPRefresh() {
	if a.mcpServers == nil {
		return
	}
	a.runMCPAsync(func(ctx context.Context) error {
		return a.mcpServers.Refresh(ctx)
	})
}

func (a *App) runMCPAsync(fn func(ctx context.Context) error) {
	a.mcpState.loading = true
	go func() {
		err := fn(context.Background())
		a.gui.Update(func(g *gocui.Gui) error {
			a.mcpState.loading = false
			if err != nil {
				a.showError(g, fmt.Sprintf("MCP error: %v", err))
			}
			return nil
		})
	}()
}

func (a *App) mcpItemCount() int {
	if a.mcpServers == nil {
		return 0
	}
	return len(a.filteredMCPServers())
}

// --- Help ---

func (a *App) ShowKeybindHelp() {
	if a.HasActiveDialog() || a.fullscreen.IsActive() {
		return
	}

	scope := a.panelManager.ActivePanel().Scope()
	tab := a.ActivePanelTabIndex()

	items := a.keyRegistry.BindingsForScopeTab(scope, tab)
	items = append(items, a.keyRegistry.BindingsForScope(keymap.ScopeGlobal)...)

	a.dialog.Kind = DialogKeybindHelp
	a.dialog.HelpAllItems = items
	a.dialog.HelpItems = items
	a.dialog.HelpCursor = 0
	a.dialog.HelpFilter = ""
	a.dialog.HelpScrollY = 0
}


// --- Search ---

func (a *App) StartSearch() {
	if a.HasActiveDialog() || a.fullscreen.IsActive() {
		return
	}

	panel := a.panelManager.ActivePanel()
	if panel == nil {
		return
	}
	panelName := panel.Name()

	// Save pre-search cursor position for Esc restore.
	var preCursor int
	switch panelName {
	case "sessions":
		preCursor = a.cursor
	case "plugins":
		preCursor = a.pluginState.Cursor()
	case "logs":
		preCursor = a.logs.CursorY()
	default:
		return
	}

	// Clear any previous active filter for this panel so the user
	// starts fresh. The previous filter's results are discarded.
	a.clearActiveFilter(panelName)

	a.dialog.Kind = DialogSearch
	a.dialog.SearchQuery = ""
	a.dialog.SearchPanel = panelName
	a.dialog.SearchPreCursor = preCursor
}

// --- Scroll mode ---

func (a *App) IsScrollMode() bool { return a.scroll.IsActive() }

func (a *App) ScrollModeEnter() {
	viewH := a.scrollViewHeight()
	if viewH <= 0 {
		return
	}
	a.scroll.Enter(viewH)
	a.scroll.BumpGeneration()
	a.captureScrollbackWithHistorySize()
}

func (a *App) ScrollModeExit() {
	a.scroll.Exit()
	a.preview.Invalidate()
}

func (a *App) ScrollModeUp() {
	// vim-like: move cursor up; if at top of viewport, scroll up
	if a.scroll.CursorY() > 0 {
		a.scroll.CursorUp()
		return
	}
	// Cursor at top edge — scroll viewport up (show older content)
	a.scroll.ScrollUp(1)
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

func (a *App) ScrollModeDown() {
	// vim-like: move cursor down; if at bottom of viewport, scroll down
	maxCursor := len(a.scroll.Lines()) - 1
	if maxCursor < 0 {
		maxCursor = a.scroll.ViewHeight() - 1
	}
	if a.scroll.CursorY() < maxCursor {
		a.scroll.CursorDown()
		return
	}
	// Cursor at bottom edge — scroll viewport down (show newer content)
	if a.scroll.ScrollOffset() > 0 {
		a.scroll.ScrollDown(1)
		a.scroll.BumpGeneration()
		a.captureScrollbackAsync()
	}
}

func (a *App) ScrollModeHalfUp() {
	half := a.scroll.ViewHeight() / 2
	a.scroll.ScrollUp(half)
	// Keep cursor at same relative position (vim Ctrl+U behavior)
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

func (a *App) ScrollModeHalfDown() {
	half := a.scroll.ViewHeight() / 2
	a.scroll.ScrollDown(half)
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

func (a *App) ScrollModeToTop() {
	a.scroll.ToTop()
	a.scroll.BumpGeneration()
	a.captureScrollbackWithHistorySize()
}

func (a *App) ScrollModeToBottom() {
	a.scroll.ToBottom()
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

func (a *App) ScrollModeToggleSelect() {
	a.scroll.ToggleSelect()
}

func (a *App) ScrollModeCopy() {
	text := a.scroll.CopyText()
	if text != "" {
		copyToClipboard(stripANSI(text))
	}
	a.scroll.Exit()
	a.preview.Invalidate()
}

// mouseScrollLines is the number of lines scrolled per mouse wheel tick.
const mouseScrollLines = 3

// ScrollModeMouseUp handles mouse wheel up in fullscreen.
// Enters scroll mode if not already active, then scrolls the viewport up.
func (a *App) ScrollModeMouseUp() {
	if !a.scroll.IsActive() {
		a.ScrollModeEnter()
	}
	a.scroll.ScrollUp(mouseScrollLines)
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

// ScrollModeMouseDown handles mouse wheel down in fullscreen.
// Scrolls the viewport down. Exits scroll mode when reaching live output.
func (a *App) ScrollModeMouseDown() {
	if !a.scroll.IsActive() {
		return
	}
	a.scroll.ScrollDown(mouseScrollLines)
	if a.scroll.ScrollOffset() == 0 {
		a.ScrollModeExit()
		return
	}
	a.scroll.BumpGeneration()
	a.captureScrollbackAsync()
}

// stripANSI removes ANSI escape sequences from text.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// scrollViewHeight returns the inner height of the fullscreen view.
func (a *App) scrollViewHeight() int {
	v, err := a.gui.View("main")
	if err != nil {
		return 0
	}
	return v.InnerHeight()
}


// captureScrollbackWithHistorySize is like captureScrollbackAsync but also
// queries HistorySize asynchronously to update the scroll maxOffset. Used by
// ScrollModeEnter and ScrollModeToTop where the history size is needed for
// correct g/G navigation but must not block the GUI thread.
//
// Both HistorySize and CaptureScrollback run sequentially in the same goroutine.
// Their gui.Update callbacks are delivered in FIFO order (gocui channel semantics).
// The generation guard discards stale results if the user scrolls during the
// network round-trips.
func (a *App) captureScrollbackWithHistorySize() {
	target := a.fullscreen.Target()
	if target == "" {
		return
	}
	gen := a.scroll.Generation()
	startLine, endLine := a.scroll.CaptureRange()
	viewW := a.scrollViewWidth()

	go func() {
		histSize, histErr := a.sessions.HistorySize(target)
		result, scrollErr := a.sessions.CaptureScrollback(target, viewW, startLine, endLine)
		a.gui.Update(func(g *gocui.Gui) error {
			if a.scroll.Generation() != gen {
				return nil
			}
			if histErr == nil && histSize > 0 {
				a.scroll.SetMaxOffset(histSize)
			}
			if scrollErr == nil {
				a.scroll.SetLines(splitLines(result.Content))
			}
			return nil
		})
	}()
}

// captureScrollbackAsync launches a goroutine to capture scrollback content.
func (a *App) captureScrollbackAsync() {
	target := a.fullscreen.Target()
	if target == "" {
		return
	}
	gen := a.scroll.Generation()
	startLine, endLine := a.scroll.CaptureRange()
	viewW := a.scrollViewWidth()

	go func() {
		result, err := a.sessions.CaptureScrollback(target, viewW, startLine, endLine)
		// Serialise state mutation through the gocui event loop to avoid
		// racing with BumpGeneration/Exit on the main goroutine.
		a.gui.Update(func(g *gocui.Gui) error {
			if err != nil || a.scroll.Generation() != gen {
				return nil
			}
			a.scroll.SetLines(splitLines(result.Content))
			return nil
		})
	}()
}

// scrollViewWidth returns the inner width of the fullscreen view.
func (a *App) scrollViewWidth() int {
	v, err := a.gui.View("main")
	if err != nil {
		return 0
	}
	return v.InnerWidth()
}

// splitLines splits a string into lines, preserving empty trailing lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// --- Application ---

func (a *App) Quit() { a.quitRequested = true }
