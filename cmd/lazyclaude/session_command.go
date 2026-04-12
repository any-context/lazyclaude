package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/any-context/lazyclaude/internal/gui"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/google/uuid"
)

// absWorkingDir returns filepath.Abs("."), used for tmux window start dirs.
func absWorkingDir() (string, error) {
	return filepath.Abs(".")
}

// OperationTarget identifies where a session command should execute.
// Host == "" means local; non-empty means route to the remote daemon.
type OperationTarget struct {
	Host        string // SSH host (empty = local)
	ProjectRoot string // project root path (local or remote)
}

// remoteSessionAPI is the subset of *daemon.RemoteProvider methods that
// SessionCommandService invokes directly. Defining this interface lets
// tests inject a fake remote backend without a real SSH connection.
// *daemon.RemoteProvider satisfies it implicitly.
type remoteSessionAPI interface {
	CreateSession(path string) (*daemon.SessionCreateResponse, error)
	Delete(id string) error
	Rename(id, newName string) error
	ResumeSession(id, prompt, name string) error
}

// SessionCommandService encapsulates all session create/delete/rename
// operations, hiding the local vs remote branching from the GUI adapter.
//
// The host-specific branching ("if sess.Host != ''") lives here rather
// than in the GUI adapter, keeping guiCompositeAdapter a thin pass-through.
type SessionCommandService struct {
	localMgr *session.Manager
	cp       *daemon.CompositeProvider
	mirrors  MirrorCreator
	tmux     tmux.Client

	// onError reports errors to the GUI (wired from App).
	onError func(msg string)
	// guiUpdateFn triggers a GUI refresh from background goroutines.
	guiUpdateFn func()
	// ensureConnectedFn lazily establishes a remote connection.
	ensureConnectedFn func(host string) error
	// resolveRemotePathFn maps a local path to the remote CWD.
	resolveRemotePathFn func(path, host string) string
	// remoteProviderFn overrides the default remote provider lookup.
	// Used by tests to inject a fake remoteSessionAPI without building
	// a real SSH-backed *daemon.RemoteProvider. In production this is
	// left nil and remoteProvider() falls back to the concrete type
	// assertion via cp.RemoteProvider.
	remoteProviderFn func(host string) remoteSessionAPI
}

// MirrorCreator is the subset of MirrorManager needed by SessionCommandService.
type MirrorCreator interface {
	CreateMirror(host, groupPath string, resp *daemon.SessionCreateResponse) error
	DeleteMirror(sessionID string) error
}

// prepareRemote ensures the remote host is connected and resolves the
// project root to the remote CWD. Must be called before any remote API
// call that takes a project root.
func (s *SessionCommandService) prepareRemote(target *OperationTarget) error {
	if target.Host == "" {
		return nil
	}
	if err := s.ensureConnected(target.Host); err != nil {
		return err
	}
	if s.resolveRemotePathFn != nil {
		target.ProjectRoot = s.resolveRemotePathFn(target.ProjectRoot, target.Host)
	}
	return nil
}

// Delete removes a session. For remote sessions, it sends a best-effort
// delete to the remote daemon then removes the local mirror.
func (s *SessionCommandService) Delete(id string) error {
	sess := s.localMgr.Store().FindByID(id)
	if sess == nil {
		return fmt.Errorf("session not found: %s", id)
	}

	if sess.Host != "" {
		// Remote: delete on daemon (best-effort) + kill local mirror.
		if rp := s.remoteProvider(sess.Host); rp != nil {
			if err := rp.Delete(id); err != nil {
				debugLog("SessionCommandService.Delete: remote API failed (continuing): %v", err)
			}
		}
		if s.mirrors != nil {
			_ = s.mirrors.DeleteMirror(id)
		}
		s.localMgr.Store().Remove(id)
		return s.localMgr.Store().Save()
	}
	return s.cp.Delete(id)
}

// Rename renames a session. For remote sessions, it sends the rename to
// the remote daemon then updates the local store.
func (s *SessionCommandService) Rename(id, newName string) error {
	sess := s.localMgr.Store().FindByID(id)
	if sess == nil {
		return fmt.Errorf("session not found: %s", id)
	}

	if sess.Host != "" {
		rp := s.remoteProvider(sess.Host)
		if rp == nil {
			return fmt.Errorf("no remote provider for host %q", sess.Host)
		}
		if err := rp.Rename(id, newName); err != nil {
			return fmt.Errorf("remote rename: %w", err)
		}
		s.localMgr.Store().UpdateSession(id, func(s *session.Session) {
			s.Name = newName
		})
		return s.localMgr.Store().Save()
	}
	return s.cp.Rename(id, newName)
}

// Create creates a new session, routing to local or remote based on target.
func (s *SessionCommandService) Create(target OperationTarget) error {
	if target.Host == "" {
		return s.cp.Create(target.ProjectRoot, "")
	}

	// Remote: optimistic creation with placeholder.
	placeholder := session.Session{
		ID:        uuid.New().String(),
		Name:      "connecting...",
		Path:      target.Host,
		Host:      target.Host,
		Status:    session.StatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.localMgr.Store().Add(placeholder, "")
	if err := s.localMgr.Store().Save(); err != nil {
		return fmt.Errorf("save placeholder: %w", err)
	}

	go s.completeRemoteCreate(placeholder.ID, target)
	return nil
}

// completeRemoteCreate runs in a background goroutine to finish the
// optimistic session creation on a remote host.
func (s *SessionCommandService) completeRemoteCreate(placeholderID string, target OperationTarget) {
	debugLog("completeRemoteCreate: placeholderID=%q host=%q", placeholderID, target.Host)
	if s.ensureConnectedFn != nil {
		if err := s.ensureConnectedFn(target.Host); err != nil {
			debugLog("completeRemoteCreate: ensureConnected failed: %v", err)
			s.failPlaceholder(placeholderID, fmt.Sprintf("Connection failed: %v", err))
			return
		}
	}

	// Resolve the local path to the remote CWD now that the connection exists.
	remotePath := target.ProjectRoot
	if s.resolveRemotePathFn != nil {
		remotePath = s.resolveRemotePathFn(target.ProjectRoot, target.Host)
	}
	debugLog("completeRemoteCreate: remotePath=%q", remotePath)

	rp := s.remoteProvider(target.Host)
	if rp == nil {
		s.failPlaceholder(placeholderID, fmt.Sprintf("no remote provider for host %q", target.Host))
		return
	}
	resp, err := rp.CreateSession(remotePath)
	if err != nil {
		debugLog("completeRemoteCreate: CreateSession failed: %v", err)
		s.failPlaceholder(placeholderID, fmt.Sprintf("Session creation failed: %v", err))
		return
	}
	debugLog("completeRemoteCreate: CreateSession succeeded id=%q window=%q", resp.ID, resp.TmuxWindow)

	if s.mirrors != nil {
		if err := s.mirrors.CreateMirror(target.Host, remotePath, resp); err != nil {
			debugLog("completeRemoteCreate: CreateMirror failed: %v", err)
			s.failPlaceholder(placeholderID, fmt.Sprintf("Mirror setup failed: %v", err))
			return
		}
	}

	// Remove the placeholder now that the real session is in the store.
	s.localMgr.Store().Remove(placeholderID)
	if err := s.localMgr.Store().Save(); err != nil {
		debugLog("completeRemoteCreate: save after remove placeholder: %v", err)
	}
	debugLog("completeRemoteCreate: session %q created with path=%q", resp.ID, remotePath)
	s.triggerGUIUpdate()
}

// CreateWorktree creates a worktree session on the appropriate host.
func (s *SessionCommandService) CreateWorktree(target OperationTarget, name, prompt string) error {
	if err := s.prepareRemote(&target); err != nil {
		return err
	}
	return s.cp.CreateWorktree(name, prompt, target.ProjectRoot, target.Host)
}

// ResumeWorktree resumes an existing worktree session.
func (s *SessionCommandService) ResumeWorktree(target OperationTarget, wtPath, prompt string) error {
	if err := s.prepareRemote(&target); err != nil {
		return err
	}
	return s.cp.ResumeWorktree(wtPath, prompt, target.ProjectRoot, target.Host)
}

// ResumeSession resumes a session by ID with worktree name fallback.
func (s *SessionCommandService) ResumeSession(target OperationTarget, id, prompt, name string) error {
	if err := s.prepareRemote(&target); err != nil {
		return err
	}
	return s.cp.ResumeSession(id, prompt, name, target.Host)
}

// ListWorktrees lists worktrees on the appropriate host.
func (s *SessionCommandService) ListWorktrees(target OperationTarget) ([]gui.WorktreeInfo, error) {
	if err := s.prepareRemote(&target); err != nil {
		return nil, err
	}
	items, err := s.cp.ListWorktrees(target.ProjectRoot, target.Host)
	if err != nil {
		return nil, err
	}
	result := make([]gui.WorktreeInfo, len(items))
	for i, item := range items {
		result[i] = gui.WorktreeInfo{Name: item.Name, Path: item.Path, Branch: item.Branch}
	}
	return result, nil
}

// CreatePMSession creates a PM session on the appropriate host.
func (s *SessionCommandService) CreatePMSession(target OperationTarget) error {
	debugLog("SessionCommandService.CreatePMSession: host=%q projectRoot=%q", target.Host, target.ProjectRoot)
	if err := s.prepareRemote(&target); err != nil {
		debugLog("SessionCommandService.CreatePMSession: ensureConnected failed: %v", err)
		return err
	}
	debugLog("SessionCommandService.CreatePMSession: calling cp.CreatePMSession")
	err := s.cp.CreatePMSession(target.ProjectRoot, target.Host)
	debugLog("SessionCommandService.CreatePMSession: result: %v", err)
	return err
}

// CreateWorkerSession creates a worker session on the appropriate host.
func (s *SessionCommandService) CreateWorkerSession(target OperationTarget, name, prompt string) error {
	if err := s.prepareRemote(&target); err != nil {
		return err
	}
	return s.cp.CreateWorkerSession(name, prompt, target.ProjectRoot, target.Host)
}

// LaunchLazygit launches lazygit on the appropriate host.
func (s *SessionCommandService) LaunchLazygit(target OperationTarget) error {
	if err := s.prepareRemote(&target); err != nil {
		return err
	}
	return s.cp.LaunchLazygit(target.ProjectRoot, target.Host)
}

// ensureConnected calls the ensureConnectedFn if the host is non-empty.
func (s *SessionCommandService) ensureConnected(host string) error {
	if host == "" || s.ensureConnectedFn == nil {
		return nil
	}
	return s.ensureConnectedFn(host)
}

// remoteProvider returns the remote API for the given host, or nil if no
// remote provider is registered. Tests can override lookup via
// remoteProviderFn; in production this falls back to the concrete
// *daemon.RemoteProvider obtained from cp.RemoteProvider.
func (s *SessionCommandService) remoteProvider(host string) remoteSessionAPI {
	if s.remoteProviderFn != nil {
		return s.remoteProviderFn(host)
	}
	sp := s.cp.RemoteProvider(host)
	if sp == nil {
		return nil
	}
	rp, ok := sp.(*daemon.RemoteProvider)
	if !ok {
		return nil
	}
	return rp
}

// failPlaceholder marks a placeholder session as dead and creates a tmux
// error window so that preview, fullscreen, and visual mode work normally.
func (s *SessionCommandService) failPlaceholder(id, msg string) {
	s.localMgr.Store().SetStatus(id, session.StatusDead)

	sess := s.localMgr.Store().FindByID(id)
	if sess != nil && s.tmux != nil {
		windowName := sess.WindowName()
		const errCmd = "echo 'lazyclaude: session launch failed'; echo; echo \"$LAZYCLAUDE_ERR_MSG\"; echo; echo 'Press Enter to close'; read"
		abs, err := s.absPath()
		if err != nil {
			abs = "."
		}
		ctx := context.Background()
		if err := s.tmux.NewWindow(ctx, tmux.NewWindowOpts{
			Session:  "lazyclaude",
			Name:     windowName,
			Command:  errCmd,
			StartDir: abs,
			Env:      map[string]string{"LAZYCLAUDE_ERR_MSG": msg},
		}); err != nil {
			if s.onError != nil {
				s.onError(fmt.Sprintf("create error window: %v", err))
			}
		} else {
			s.localMgr.Store().SetTmuxWindow(id, "lazyclaude:"+windowName)
		}
	}

	if err := s.localMgr.Store().Save(); err != nil && s.onError != nil {
		s.onError(fmt.Sprintf("save store: %v", err))
	}
	if s.onError != nil {
		s.onError(msg)
	}
	s.triggerGUIUpdate()
}

// triggerGUIUpdate schedules a GUI refresh if the callback is wired.
func (s *SessionCommandService) triggerGUIUpdate() {
	if s.guiUpdateFn != nil {
		s.guiUpdateFn()
	}
}

// absPath returns filepath.Abs(".") for tmux window start dirs.
func (s *SessionCommandService) absPath() (string, error) {
	return absWorkingDir()
}
