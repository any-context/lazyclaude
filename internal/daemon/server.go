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

	"github.com/any-context/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/any-context/lazyclaude/internal/core/choice"
	"github.com/any-context/lazyclaude/internal/core/event"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
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

	// Preview / Scrollback
	mux.HandleFunc("GET /session/{id}/preview", s.withAuth(s.handlePreview))
	mux.HandleFunc("GET /session/{id}/scrollback", s.withAuth(s.handleScrollback))
	mux.HandleFunc("GET /session/{id}/history-size", s.withAuth(s.handleHistorySize))

	// Input
	mux.HandleFunc("POST /session/{id}/send-keys", s.withAuth(s.handleSendKeys))
	mux.HandleFunc("POST /session/{id}/send-choice", s.withAuth(s.handleSendChoice))

	// Attach
	mux.HandleFunc("GET /session/{id}/attach", s.withAuth(s.handleAttach))

	// Worktree
	mux.HandleFunc("POST /worktree/create", s.withAuth(s.handleWorktreeCreate))
	mux.HandleFunc("POST /worktree/resume", s.withAuth(s.handleWorktreeResume))
	mux.HandleFunc("GET /worktrees", s.withAuth(s.handleWorktreeList))

	// Messaging
	mux.HandleFunc("POST /msg/send", s.withAuth(s.handleMsgSend))
	mux.HandleFunc("POST /msg/create", s.withAuth(s.handleMsgCreate))
	mux.HandleFunc("GET /msg/sessions", s.withAuth(s.handleMsgSessions))

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

// Port returns the listening port.
func (s *DaemonServer) Port() int {
	return s.config.Port
}

// --- Auth middleware ---

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
		sess, err = s.mgr.Create(ctx, req.Path)
	case "worktree":
		sess, err = s.mgr.CreateWorktree(ctx, req.Name, req.Prompt, req.ProjectRoot)
	case "pm":
		sess, err = s.mgr.CreatePMSession(ctx, req.ProjectRoot)
	case "worker":
		sess, err = s.mgr.CreateWorkerSession(ctx, req.Name, req.Prompt, req.ProjectRoot)
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
		TmuxWindow: sess.TmuxWindow,
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

// --- Preview / Scrollback handlers ---

func (s *DaemonServer) handlePreview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	width, _ := strconv.Atoi(r.URL.Query().Get("width"))
	height, _ := strconv.Atoi(r.URL.Query().Get("height"))

	sess := s.mgr.Store().FindByID(id)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	target := resolveTarget(sess)
	ctx := r.Context()

	if width > 0 && height > 0 {
		if err := s.tmux.ResizeWindow(ctx, target, width, height); err != nil {
			s.log.Printf("preview: resize %s: %v", target, err)
		} else {
			select {
			case <-time.After(20 * time.Millisecond):
			case <-ctx.Done():
				return
			}
		}
	}

	content, err := s.tmux.CapturePaneANSI(ctx, target)
	if err != nil {
		http.Error(w, "capture failed", http.StatusInternalServerError)
		return
	}

	var cursorX, cursorY int
	if pos, posErr := s.tmux.ShowMessage(ctx, target, "#{cursor_x},#{cursor_y}"); posErr == nil {
		parts := strings.SplitN(strings.TrimSpace(pos), ",", 2)
		if len(parts) == 2 {
			cursorX, _ = strconv.Atoi(parts[0])
			cursorY, _ = strconv.Atoi(parts[1])
		}
	}

	writeJSON(w, http.StatusOK, PreviewResponse{
		Content: content,
		CursorX: cursorX,
		CursorY: cursorY,
	})
}

func (s *DaemonServer) handleScrollback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	width, _ := strconv.Atoi(r.URL.Query().Get("width"))
	startLine, _ := strconv.Atoi(r.URL.Query().Get("start_line"))
	endLine, _ := strconv.Atoi(r.URL.Query().Get("end_line"))

	sess := s.mgr.Store().FindByID(id)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	target := resolveTarget(sess)
	_ = width // width is used by caller for truncation, not by capture

	content, err := s.tmux.CapturePaneANSIRange(r.Context(), target, startLine, endLine)
	if err != nil {
		http.Error(w, "capture failed", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, ScrollbackResponse{Content: content})
}

func (s *DaemonServer) handleHistorySize(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := s.mgr.Store().FindByID(id)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	target := resolveTarget(sess)
	out, err := s.tmux.ShowMessage(r.Context(), target, "#{history_size}")
	if err != nil {
		http.Error(w, "tmux error", http.StatusInternalServerError)
		return
	}
	lines, _ := strconv.Atoi(strings.TrimSpace(out))
	writeJSON(w, http.StatusOK, HistorySizeResponse{Lines: lines})
}

// --- Input handlers ---

func (s *DaemonServer) handleSendKeys(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req SendKeysRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sess := s.mgr.Store().FindByID(id)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	target := "lazyclaude:" + sess.WindowName()
	if err := s.tmux.SendKeysLiteral(r.Context(), target, req.Keys); err != nil {
		http.Error(w, "send-keys failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *DaemonServer) handleSendChoice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req SendChoiceRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sess := s.mgr.Store().FindByID(id)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	window := req.Window
	if window == "" {
		window = sess.TmuxWindow
	}
	if window == "" {
		window = sess.WindowName()
	}

	c := choice.Choice(req.Choice)
	if err := tmuxadapter.SendToPane(r.Context(), s.tmux, window, c); err != nil {
		http.Error(w, "send-choice failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Attach handler ---

func (s *DaemonServer) handleAttach(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := s.mgr.Store().FindByID(id)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	target := "lazyclaude:" + sess.WindowName()
	writeJSON(w, http.StatusOK, AttachResponse{TmuxTarget: target})
}

// --- Worktree handlers ---

func (s *DaemonServer) handleWorktreeCreate(w http.ResponseWriter, r *http.Request) {
	var req WorktreeCreateRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sess, err := s.mgr.CreateWorktree(r.Context(), req.Name, req.Prompt, req.ProjectRoot)
	if err != nil {
		s.log.Printf("worktree/create: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	wtPath := session.WorktreePath(req.ProjectRoot, req.Name)
	writeJSON(w, http.StatusCreated, WorktreeCreateResponse{
		SessionID: sess.ID,
		Path:      wtPath,
		Branch:    req.Name,
	})
}

func (s *DaemonServer) handleWorktreeResume(w http.ResponseWriter, r *http.Request) {
	var req WorktreeResumeRequest
	if err := readJSON(w, r, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sess, err := s.mgr.ResumeWorktree(r.Context(), req.WorktreePath, req.Prompt, req.ProjectRoot)
	if err != nil {
		s.log.Printf("worktree/resume: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, WorktreeResumeResponse{SessionID: sess.ID})
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
		sess, err = s.mgr.CreateWorkerSession(ctx, req.Name, req.Prompt, project.Path)
	case "pm":
		sess, err = s.mgr.CreatePMSession(ctx, project.Path)
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

func resolveTarget(sess *session.Session) string {
	if sess.TmuxWindow != "" {
		return sess.TmuxWindow
	}
	return "lazyclaude:" + sess.WindowName()
}

func sessionToInfo(sess session.Session) SessionInfo {
	return SessionInfo{
		ID:         sess.ID,
		Name:       sess.Name,
		Path:       sess.Path,
		Host:       sess.Host,
		Status:     sess.Status.String(),
		TmuxWindow: sess.TmuxWindow,
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
		dir = fmt.Sprintf("/tmp/lazyclaude-%s", os.Getenv("USER"))
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
		dir = fmt.Sprintf("/tmp/lazyclaude-%s", os.Getenv("USER"))
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

// ReadDaemonInfo reads daemon connection info from the runtime directory.
func ReadDaemonInfo(runtimeDir string) (*DaemonInfo, error) {
	data, err := os.ReadFile(filepath.Join(runtimeDir, "daemon.json"))
	if err != nil {
		return nil, fmt.Errorf("read daemon.json: %w", err)
	}
	var info DaemonInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parse daemon.json: %w", err)
	}
	return &info, nil
}

