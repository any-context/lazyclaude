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
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/any-context/lazyclaude/internal/profile"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// localDaemonProvider wraps session.Manager to implement daemon.SessionProvider.
// Used as the local backend for CompositeProvider.
type localDaemonProvider struct {
	mgr  *session.Manager
	tmux tmux.Client

	lastResizeID string
	lastResizeW  int
	lastResizeH  int
}

// Compile-time check.
var _ daemon.SessionProvider = (*localDaemonProvider)(nil)

func (p *localDaemonProvider) HasSession(sessionID string) bool {
	return p.mgr.Store().FindByID(sessionID) != nil
}

// LocalSessionHost returns the Host field of the session with the given ID
// from the local store. Remote mirror sessions are registered in the local
// store with Host set to the SSH host, so this helper lets the composite
// provider dispatch capture operations to the right backend.
func (p *localDaemonProvider) LocalSessionHost(id string) (string, bool) {
	sess := p.mgr.Store().FindByID(id)
	if sess == nil {
		return "", false
	}
	return sess.Host, true
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
	target := sess.TmuxTarget()
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

	resp, err := daemon.CapturePreviewContent(ctx, p.tmux, target)
	if err != nil {
		return nil, err
	}
	if width <= 0 {
		return resp, nil
	}

	lines := strings.Split(resp.Content, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	resp.Content = strings.Join(lines, "\n")

	return resp, nil
}

func (p *localDaemonProvider) CaptureScrollback(id string, _, startLine, endLine int) (*daemon.ScrollbackResponse, error) {
	sess := p.mgr.Store().FindByID(id)
	if sess == nil {
		return &daemon.ScrollbackResponse{}, nil
	}
	target := sess.TmuxTarget()
	content, err := p.tmux.CapturePaneANSIRange(context.Background(), target, startLine, endLine)
	return &daemon.ScrollbackResponse{Content: content}, err
}

func (p *localDaemonProvider) HistorySize(id string) (int, error) {
	sess := p.mgr.Store().FindByID(id)
	if sess == nil {
		return 0, nil
	}
	target := sess.TmuxTarget()
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
	target := sess.TmuxTarget()

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

func (p *localDaemonProvider) ResumeSession(id, prompt, name string) error {
	_, err := p.mgr.ResumeSession(context.Background(), id, prompt, name)
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

// Profiles returns the local profile list loaded from
// $HOME/.lazyclaude/config.json.
//
// It implements the daemon.profileFetcher interface so that
// CompositeProvider.Profiles(ctx, "") delegates here for the local host.
//
// Returns (profiles, daemonErrStr, transportErr). transportErr is always nil
// (file I/O failures are encoded in daemonErrStr). On config-absent, profiles
// contains the builtin default and daemonErrStr is empty. On parse error,
// profiles is nil and daemonErrStr contains the human-readable description.
func (p *localDaemonProvider) Profiles(_ context.Context) ([]daemon.ProfileDefAPI, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Sprintf("resolve home dir: %v", err), nil
	}
	configPath := filepath.Join(home, ".lazyclaude", "config.json")
	_, profiles, loadErr := profile.Load(configPath)
	if loadErr != nil {
		return nil, loadErr.Error(), nil
	}

	apiProfiles := make([]daemon.ProfileDefAPI, len(profiles))
	for i, pd := range profiles {
		apiProfiles[i] = daemon.ProfileDefAPI{
			Name:        pd.Name,
			Command:     pd.Command,
			Description: pd.Description,
			Default:     pd.Default,
			Builtin:     pd.Builtin,
		}
		if len(pd.Args) > 0 {
			apiProfiles[i].Args = make([]string, len(pd.Args))
			copy(apiProfiles[i].Args, pd.Args)
		}
		if len(pd.Env) > 0 {
			apiProfiles[i].Env = make(map[string]string, len(pd.Env))
			for k, v := range pd.Env {
				apiProfiles[i].Env[k] = v
			}
		}
	}
	return apiProfiles, "", nil
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
