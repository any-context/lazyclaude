package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	cp      *daemon.CompositeProvider
	localMgr *session.Manager
	paths   config.Paths

	// windowActivityFn provides window->activity mapping from the App layer.
	windowActivityFn func() map[string]gui.WindowActivityEntry

	// cachedPending is refreshed once per layout cycle.
	cachedPending map[string]bool
}

// Compile-time check.
var _ gui.SessionProvider = (*guiCompositeAdapter)(nil)

func (a *guiCompositeAdapter) RefreshPendingFrom(notifications []*model.ToolNotification) {
	a.cachedPending = pendingWindowSet(notifications)
}

func (a *guiCompositeAdapter) Sessions() []gui.SessionItem {
	sessions, err := a.cp.Sessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: composite sessions: %v\n", err)
		return nil
	}
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
	return a.cp.Create(path, "")
}

func (a *guiCompositeAdapter) Delete(id string) error {
	return a.cp.Delete(id)
}

func (a *guiCompositeAdapter) Rename(id, newName string) error {
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
	return a.cp.CreateWorktree(name, prompt, projectRoot, "")
}

func (a *guiCompositeAdapter) ResumeWorktree(worktreePath, prompt, projectRoot string) error {
	return a.cp.ResumeWorktree(worktreePath, prompt, projectRoot, "")
}

func (a *guiCompositeAdapter) ListWorktrees(projectRoot string) ([]gui.WorktreeInfo, error) {
	items, err := a.cp.ListWorktrees(projectRoot, "")
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
	return a.cp.CreatePMSession(projectRoot, "")
}

func (a *guiCompositeAdapter) CreateWorkerSession(name, prompt, projectRoot string) error {
	return a.cp.CreateWorkerSession(name, prompt, projectRoot, "")
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
