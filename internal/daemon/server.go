// Package daemon provides the HTTP daemon server for lazyclaude.
// It wraps session.Manager, event.Broker, and tmux.Client as a REST API.
package daemon

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/any-context/lazyclaude/internal/core/event"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/profile"
	"github.com/any-context/lazyclaude/internal/session"
)

const tmuxSessionName = "lazyclaude"

// DaemonConfig holds daemon server configuration.
type DaemonConfig struct {
	Port       int    // 0 = random
	Token      string // auth token; auto-generated if empty
	RuntimeDir string // /tmp/lazyclaude-$USER
}

// DaemonInfo is defined in lifecycle.go. The daemon.json file also includes
// the PID for process management, but DaemonInfo is the shared type.

// DaemonServer is the daemon HTTP server.
type DaemonServer struct {
	config  DaemonConfig
	mgr     *session.Manager
	broker  *event.Broker[model.Event]
	tmux    tmux.Client
	log     *log.Logger
	startAt time.Time
	version string

	listener net.Listener
	httpSrv  *http.Server

	sseEventID atomic.Uint64

	mu         sync.RWMutex
	shutdown   bool
	shutdownCh chan struct{} // closed on shutdown request
}

// DaemonOption configures the daemon server.
type DaemonOption func(*DaemonServer)

// WithVersion sets the binary version string for /health.
func WithVersion(v string) DaemonOption {
	return func(s *DaemonServer) { s.version = v }
}

// NewDaemonServer creates a new daemon server.
func NewDaemonServer(
	cfg DaemonConfig,
	mgr *session.Manager,
	broker *event.Broker[model.Event],
	tmuxClient tmux.Client,
	logger *log.Logger,
	opts ...DaemonOption,
) *DaemonServer {
	s := &DaemonServer{
		config:     cfg,
		mgr:        mgr,
		broker:     broker,
		tmux:       tmuxClient,
		log:        logger,
		startAt:    time.Now(),
		version:    "dev",
		shutdownCh: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}

	mux := http.NewServeMux()

	// Session CRUD
	mux.HandleFunc("POST /session/create", s.withAuth(s.handleSessionCreate))
	mux.HandleFunc("DELETE /session/{id}", s.withAuth(s.handleSessionDelete))
	mux.HandleFunc("POST /session/{id}/rename", s.withAuth(s.handleSessionRename))
	mux.HandleFunc("GET /sessions", s.withAuth(s.handleSessionList))

	// Capture (scrollback / history size). Added in API v2 so that remote
	// fullscreen copy mode can read the remote tmux server's scrollback
	// directly (the local mirror window's tmux buffer is empty).
	mux.HandleFunc("POST /session/{id}/scrollback", s.withAuth(s.handleScrollback))
	mux.HandleFunc("GET /session/{id}/history-size", s.withAuth(s.handleHistorySize))

	// Worktree
	mux.HandleFunc("POST /worktree/create", s.withAuth(s.handleWorktreeCreate))
	mux.HandleFunc("POST /worktree/resume", s.withAuth(s.handleWorktreeResume))
	mux.HandleFunc("GET /worktrees", s.withAuth(s.handleWorktreeList))

	// Session resume (by ID with worktree name fallback)
	mux.HandleFunc("POST /session/resume", s.withAuth(s.handleSessionResume))

	// Messaging
	mux.HandleFunc("POST /msg/send", s.withAuth(s.handleMsgSend))
	mux.HandleFunc("POST /msg/create", s.withAuth(s.handleMsgCreate))
	mux.HandleFunc("GET /msg/sessions", s.withAuth(s.handleMsgSessions))

	// Profiles
	mux.HandleFunc("GET /profiles", s.withAuth(s.handleProfiles))

	// System info
	mux.HandleFunc("GET /cwd", s.withAuth(s.handleCWD))

	// Health / Lifecycle
	mux.HandleFunc("GET /health", s.handleHealth) // no auth for health check
	mux.HandleFunc("POST /shutdown", s.withAuth(s.handleShutdown))

	// SSE Notifications
	mux.HandleFunc("GET /notifications", s.withAuth(s.handleSSE))

	s.httpSrv = &http.Server{Handler: mux}
	return s
}

// Start begins listening. Returns the actual port.
func (s *DaemonServer) Start(ctx context.Context) (int, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", s.config.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("listen %s: %w", addr, err)
	}
	s.listener = ln

	port := ln.Addr().(*net.TCPAddr).Port
	s.config.Port = port

	if err := s.writeDaemonInfo(port); err != nil {
		ln.Close()
		return 0, fmt.Errorf("write daemon info: %w", err)
	}

	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Printf("daemon server error: %v", err)
		}
	}()

	s.log.Printf("daemon listening on 127.0.0.1:%d", port)
	return port, nil
}

// Stop gracefully shuts down the server.
func (s *DaemonServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return nil
	}
	s.shutdown = true
	close(s.shutdownCh)
	s.mu.Unlock()

	s.removeDaemonInfo()
	return s.httpSrv.Shutdown(ctx)
}

// ShutdownCh returns a channel that is closed when shutdown is requested.
func (s *DaemonServer) ShutdownCh() <-chan struct{} {
	return s.shutdownCh
}

// --- Auth middleware ---

// withAuth authenticates requests using the X-Daemon-Authorization header.
func (s *DaemonServer) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get(AuthHeader)
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.config.Token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// --- JSON helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Best effort; headers already sent.
		_ = err
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	return json.NewDecoder(r.Body).Decode(v)
}

// --- Session CRUD handlers ---

func (s *DaemonServer) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	var req SessionCreateRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var sess *session.Session
	var err error

	switch req.SessionType {
	case "plain", "":
		// Profile/Options support for plain sessions is deferred to Phase 2b.
		// The API fields are accepted but not forwarded to keep the schema
		// forward-compatible without breaking existing callers.
		sess, err = s.mgr.Create(ctx, req.Path)
	case "worktree":
		sess, err = s.mgr.CreateWorktreeOpts(ctx, session.WorktreeOpts{
			Name:        req.Name,
			Prompt:      req.Prompt,
			ProjectRoot: req.ProjectRoot,
			Profile:     req.Profile,
			Options:     req.Options,
		})
	case "pm":
		sess, err = s.mgr.CreatePMSessionOpts(ctx, session.PMOpts{
			ProjectRoot: req.ProjectRoot,
			Profile:     req.Profile,
			Options:     req.Options,
		})
	case "worker":
		sess, err = s.mgr.CreateWorkerSessionOpts(ctx, session.WorkerOpts{
			Name:        req.Name,
			Prompt:      req.Prompt,
			ProjectRoot: req.ProjectRoot,
			Profile:     req.Profile,
			Options:     req.Options,
		})
	default:
		http.Error(w, "invalid session_type", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.log.Printf("session/create: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, SessionCreateResponse{
		ID:         sess.ID,
		Name:       sess.Name,
		Path:       sess.Path,
		TmuxWindow: sess.WindowName(),
		Role:       string(sess.Role),
	})
}

func (s *DaemonServer) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	if err := s.mgr.Delete(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			s.log.Printf("session/delete: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *DaemonServer) handleSessionRename(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req SessionRenameRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.mgr.Rename(id, req.NewName); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *DaemonServer) handleSessionList(w http.ResponseWriter, r *http.Request) {
	sessions := s.mgr.Sessions()
	items := make([]SessionInfo, len(sessions))
	for i, sess := range sessions {
		items[i] = sessionToInfo(sess)
	}
	writeJSON(w, http.StatusOK, SessionListResponse{Sessions: items})
}

// --- Capture handlers ---

// handleScrollback captures a range of scrollback lines for a session by
// running tmux capture-pane on the daemon's own tmux server. This is the
// fullscreen copy-mode path for remote sessions: the local mirror window's
// tmux buffer does not contain the remote tmux's historical scrollback, so
// the TUI asks the remote daemon to capture from its own server.
func (s *DaemonServer) handleScrollback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	var req ScrollbackRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sess := s.mgr.Store().FindByID(id)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	target := sess.TmuxTarget()
	content, err := s.tmux.CapturePaneANSIRange(r.Context(), target, req.StartLine, req.EndLine)
	if err != nil {
		s.log.Printf("session/%s/scrollback: %v", id, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, ScrollbackResponse{Content: content})
}

// handleHistorySize returns the pane's scrollback history size (number of
// lines currently held in the tmux pane's history buffer). Used together
// with handleScrollback by the fullscreen copy-mode.
func (s *DaemonServer) handleHistorySize(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	sess := s.mgr.Store().FindByID(id)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	target := sess.TmuxTarget()
	out, err := s.tmux.ShowMessage(r.Context(), target, "#{history_size}")
	if err != nil {
		s.log.Printf("session/%s/history-size: %v", id, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// tmux #{history_size} is always a plain integer. If we get anything
	// else the pane is in an unexpected state; surface it as 502 so the
	// client can distinguish from a legitimate "0 history lines".
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		s.log.Printf("session/%s/history-size: parse %q: %v", id, out, err)
		http.Error(w, "invalid history size from tmux", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, HistorySizeResponse{Lines: n})
}

// --- Worktree handlers ---

func (s *DaemonServer) handleWorktreeCreate(w http.ResponseWriter, r *http.Request) {
	var req WorktreeCreateRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sess, err := s.mgr.CreateWorktreeOpts(r.Context(), session.WorktreeOpts{
		Name:        req.Name,
		Prompt:      req.Prompt,
		ProjectRoot: req.ProjectRoot,
		Profile:     req.Profile,
		Options:     req.Options,
	})
	if err != nil {
		s.log.Printf("worktree/create: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	wtPath := session.WorktreePath(req.ProjectRoot, req.Name)
	writeJSON(w, http.StatusCreated, WorktreeCreateResponse{
		SessionID:  sess.ID,
		Path:       wtPath,
		Branch:     req.Name,
		TmuxWindow: sess.WindowName(),
		Role:       string(sess.Role),
	})
}

func (s *DaemonServer) handleWorktreeResume(w http.ResponseWriter, r *http.Request) {
	var req WorktreeResumeRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sess, err := s.mgr.ResumeWorktreeOpts(r.Context(), session.ResumeOpts{
		WorktreePath: req.WorktreePath,
		Prompt:       req.Prompt,
		ProjectRoot:  req.ProjectRoot,
		Profile:      req.Profile,
		Options:      req.Options,
	})
	if err != nil {
		s.log.Printf("worktree/resume: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, WorktreeResumeResponse{
		SessionID:  sess.ID,
		Name:       sess.Name,
		Path:       sess.Path,
		TmuxWindow: sess.WindowName(),
		Role:       string(sess.Role),
	})
}

func (s *DaemonServer) handleSessionResume(w http.ResponseWriter, r *http.Request) {
	var req SessionResumeRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	sess, err := s.mgr.ResumeSession(r.Context(), req.ID, req.Prompt, req.Name)
	if err != nil {
		s.log.Printf("session/resume: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, SessionResumeResponse{
		SessionID:  sess.ID,
		Name:       sess.Name,
		Path:       sess.Path,
		TmuxWindow: sess.WindowName(),
		Role:       string(sess.Role),
	})
}

func (s *DaemonServer) handleWorktreeList(w http.ResponseWriter, r *http.Request) {
	projectRoot := r.URL.Query().Get("project_root")
	if projectRoot == "" {
		http.Error(w, "project_root query param required", http.StatusBadRequest)
		return
	}

	items, err := session.ListWorktrees(r.Context(), projectRoot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	wts := make([]WorktreeInfo, len(items))
	for i, item := range items {
		wts[i] = WorktreeInfo{Name: item.Name, Path: item.Path, Branch: item.Branch}
	}
	writeJSON(w, http.StatusOK, WorktreeListResponse{Worktrees: wts})
}

// --- Messaging handlers ---

func (s *DaemonServer) handleMsgSend(w http.ResponseWriter, r *http.Request) {
	var req MsgSendRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.From == "" || req.To == "" {
		writeJSON(w, http.StatusBadRequest, MsgSendResponse{Error: "from and to are required"})
		return
	}
	if req.From == req.To {
		writeJSON(w, http.StatusBadRequest, MsgSendResponse{Error: "cannot send a message to yourself"})
		return
	}

	ctx := r.Context()
	sessions := s.mgr.Sessions()

	var recipient *session.Session
	var senderName string
	for i := range sessions {
		if sessions[i].ID == req.To {
			recipient = &sessions[i]
		}
		if sessions[i].ID == req.From {
			senderName = sessions[i].Name
		}
	}
	if recipient == nil {
		writeJSON(w, http.StatusNotFound, MsgSendResponse{Error: "recipient session not found"})
		return
	}

	window := recipient.TmuxWindow
	if window == "" {
		window = s.resolveWindowByName(ctx, recipient.WindowName())
	}
	if window == "" {
		writeJSON(w, http.StatusBadGateway, MsgSendResponse{Error: "recipient has no tmux window"})
		return
	}

	text := fmt.Sprintf("[MESSAGE from %s (%s)]\ntype: %s\n---\n%s\n",
		senderName, req.From, req.Type, req.Body)

	target := "lazyclaude:" + window
	if err := s.tmux.SendKeysLiteral(ctx, target, text); err != nil {
		s.log.Printf("msg/send: %v", err)
		writeJSON(w, http.StatusBadGateway, MsgSendResponse{Error: "failed to deliver message"})
		return
	}
	if err := s.tmux.SendKeys(ctx, target, "Enter"); err != nil {
		s.log.Printf("msg/send: send Enter: %v", err)
	}

	writeJSON(w, http.StatusOK, MsgSendResponse{Delivered: true})
}

func (s *DaemonServer) handleMsgCreate(w http.ResponseWriter, r *http.Request) {
	var req MsgCreateRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.From == "" || req.Name == "" {
		http.Error(w, "from and name are required", http.StatusBadRequest)
		return
	}

	project := s.mgr.Store().FindProjectForSession(req.From)
	if project == nil {
		http.Error(w, "caller session not found", http.StatusNotFound)
		return
	}

	ctx := r.Context()
	var sess *session.Session
	var err error

	switch req.Type {
	case "worker":
		sess, err = s.mgr.CreateWorkerSessionOpts(ctx, session.WorkerOpts{
			Name:        req.Name,
			Prompt:      req.Prompt,
			ProjectRoot: project.Path,
			Profile:     req.Profile,
			Options:     req.Options,
		})
	case "pm":
		sess, err = s.mgr.CreatePMSessionOpts(ctx, session.PMOpts{
			ProjectRoot: project.Path,
			Profile:     req.Profile,
			Options:     req.Options,
		})
	default:
		http.Error(w, "type must be worker or pm", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.log.Printf("msg/create: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, MsgCreateResponse{SessionID: sess.ID})
}

func (s *DaemonServer) handleMsgSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.mgr.Sessions()
	items := make([]MsgSessionInfo, len(sessions))
	for i, sess := range sessions {
		items[i] = MsgSessionInfo{
			ID:   sess.ID,
			Name: sess.Name,
			Role: string(sess.Role),
		}
	}
	writeJSON(w, http.StatusOK, MsgSessionsResponse{Sessions: items})
}

// --- Profiles handler ---

// handleProfiles returns the profile list from the daemon's own
// $HOME/.lazyclaude/config.json. Because the daemon runs on the remote host,
// os.UserHomeDir() automatically resolves to the remote user's home directory,
// making this the canonical way to discover remote profiles.
//
// HTTP 200 is always returned; errors are encoded in ProfileListResponse.Error:
//
//   - Config present, valid:   Profiles: [...user profiles + builtin default]
//   - Config absent:           Profiles: [{builtin default}], Error: ""
//   - Config malformed:        Profiles: nil, Error: "invalid JSON at line N..."
//   - Home dir unavailable:    Profiles: nil, Error: "resolve home dir: ..."
//
// Security note: ProfileDefAPI.Env carries raw environment variable values from
// config.json. This endpoint is protected by token authentication
// (X-Daemon-Authorization), so only callers that already possess the daemon
// token receive these values. Users who store secrets in profile.env accept
// that any authenticated API client can read them.
func (s *DaemonServer) handleProfiles(w http.ResponseWriter, _ *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeJSON(w, http.StatusOK, ProfileListResponse{
			Error: fmt.Sprintf("resolve home dir: %v", err),
		})
		return
	}
	configPath := filepath.Join(home, ".lazyclaude", "config.json")
	_, profiles, loadErr := profile.Load(configPath)
	if loadErr != nil {
		writeJSON(w, http.StatusOK, ProfileListResponse{
			Error: loadErr.Error(),
		})
		return
	}

	apiProfiles := make([]ProfileDefAPI, len(profiles))
	for i, p := range profiles {
		apiProfiles[i] = profileDefToAPI(p)
	}
	writeJSON(w, http.StatusOK, ProfileListResponse{Profiles: apiProfiles})
}

// profileDefToAPI converts a profile.ProfileDef to the wire representation.
func profileDefToAPI(p profile.ProfileDef) ProfileDefAPI {
	api := ProfileDefAPI{
		Name:        p.Name,
		Command:     p.Command,
		Description: p.Description,
		Default:     p.Default,
		Builtin:     p.Builtin,
	}
	if len(p.Args) > 0 {
		api.Args = make([]string, len(p.Args))
		copy(api.Args, p.Args)
	}
	if len(p.Env) > 0 {
		api.Env = make(map[string]string, len(p.Env))
		for k, v := range p.Env {
			api.Env[k] = v
		}
	}
	return api
}

// --- CWD handler ---

// CWDResponse is the JSON response for GET /cwd.
type CWDResponse struct {
	CWD string `json:"cwd"`
}

func (s *DaemonServer) handleCWD(w http.ResponseWriter, _ *http.Request) {
	cwd, err := detectUserShellCWD()
	if err != nil {
		// Fallback: daemon's own CWD
		cwd, err = os.Getwd()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, CWDResponse{CWD: cwd})
}

// --- Health handler ---

func (s *DaemonServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	sessions := s.mgr.Sessions()
	writeJSON(w, http.StatusOK, HealthResponse{
		APIVersion:    APIVersion,
		BinaryVersion: s.version,
		UptimeSeconds: int64(time.Since(s.startAt).Seconds()),
		SessionCount:  len(sessions),
	})
}

// --- Shutdown handler ---

func (s *DaemonServer) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "shutting_down"})

	// Signal the shutdown channel; the caller (daemon_cmd.go) is responsible
	// for calling Stop() after the select loop detects the signal.
	s.mu.Lock()
	if !s.shutdown {
		s.shutdown = true
		close(s.shutdownCh)
	}
	s.mu.Unlock()
}

// --- Helpers ---

func sessionToInfo(sess session.Session) SessionInfo {
	return SessionInfo{
		ID:         sess.ID,
		Name:       sess.Name,
		Path:       sess.Path,
		Host:       sess.Host,
		Status:     sess.Status.String(),
		TmuxWindow: sess.WindowName(),
		Role:       string(sess.Role),
	}
}

func (s *DaemonServer) resolveWindowByName(ctx context.Context, windowName string) string {
	windows, err := s.tmux.ListWindows(ctx, tmuxSessionName)
	if err != nil {
		return ""
	}
	for _, w := range windows {
		if w.Name == windowName {
			return w.ID
		}
	}
	return ""
}

func (s *DaemonServer) writeDaemonInfo(port int) error {
	dir := s.config.RuntimeDir
	if dir == "" {
		dir = DaemonInfoDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	info := DaemonInfo{Port: port, Token: s.config.Token, PID: os.Getpid()}
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal daemon info: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "daemon.json"), data, 0o600)
}

func (s *DaemonServer) removeDaemonInfo() {
	dir := s.config.RuntimeDir
	if dir == "" {
		dir = DaemonInfoDir()
	}
	os.Remove(filepath.Join(dir, "daemon.json"))
}

// GenerateDaemonToken generates a random hex token for daemon auth.
func GenerateDaemonToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
