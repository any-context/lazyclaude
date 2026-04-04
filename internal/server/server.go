package server

import (
	"context"
	"crypto/subtle"
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
	"time"

	"github.com/any-context/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/any-context/lazyclaude/internal/core/event"
	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/notify"
	"nhooyr.io/websocket"
)

// Config holds server configuration.
type Config struct {
	Port       int
	Token      string
	IDEDir     string // lock files directory
	PortFile   string // path to write the listening port
	RuntimeDir string // choice files directory
}

// activityEntry stores the current activity state for a tmux window.
type activityEntry struct {
	State    model.ActivityState
	ToolName string
}

// Server is the MCP WebSocket + HTTP server.
type Server struct {
	config         Config
	state          *State
	handler        *Handler
	lock           *LockManager
	tmux           tmux.Client
	log            *log.Logger
	notifyBroker   *event.Broker[model.Event]
	ownsBroker     bool // true when the server created the broker (and must close it)
	sessionLister  SessionLister
	sessionCreator SessionCreator

	// activityMap tracks hook-based activity state per tmux window.
	// Updated directly by hook handlers; read by /msg/sessions.
	activityMap map[string]activityEntry
	activityMu  sync.RWMutex

	listener net.Listener
	httpSrv  *http.Server

	mu       sync.RWMutex
	shutdown bool
}

// ServerOption configures optional Server behaviour.
type ServerOption func(*Server)

// WithBroker injects an externally-owned event broker.
// The server will publish events to it but will NOT close it on Stop().
// This allows the broker to outlive server restarts so that GUI
// subscriptions remain valid across restart cycles.
func WithBroker(b *event.Broker[model.Event]) ServerOption {
	return func(s *Server) {
		s.notifyBroker = b
		s.ownsBroker = false
	}
}

// New creates a new Server.
func New(cfg Config, tmuxClient tmux.Client, logger *log.Logger, opts ...ServerOption) *Server {
	state := NewState()
	handler := NewHandler(state, tmuxClient, logger)
	handler.SetRuntimeDir(cfg.RuntimeDir)
	lockMgr := NewLockManager(cfg.IDEDir)

	s := &Server{
		config:      cfg,
		state:       state,
		handler:     handler,
		lock:        lockMgr,
		tmux:        tmuxClient,
		log:         logger,
		activityMap: make(map[string]activityEntry),
	}

	for _, opt := range opts {
		opt(s)
	}

	// Create a default broker if no external one was injected.
	if s.notifyBroker == nil {
		s.notifyBroker = event.NewBroker[model.Event]()
		s.ownsBroker = true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/notify", s.handleNotify)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/session-start", s.handleSessionStart)
	mux.HandleFunc("/prompt-submit", s.handlePromptSubmit)
	mux.HandleFunc("/msg/send", s.handleMsgSend)
	mux.HandleFunc("/msg/create", s.handleMsgCreate)
	mux.HandleFunc("/msg/sessions", s.handleMsgSessions)
	mux.HandleFunc("/", s.handleWebSocket)

	s.httpSrv = &http.Server{Handler: mux}
	return s
}

// Start begins listening. Returns the actual port (useful when port=0).
func (s *Server) Start(ctx context.Context) (int, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", s.config.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("listen %s: %w", addr, err)
	}
	s.listener = ln

	port := ln.Addr().(*net.TCPAddr).Port
	s.config.Port = port

	// Clean stale lock files from crashed servers before writing our own.
	if n := s.lock.CleanStale(); n > 0 {
		s.log.Printf("cleaned %d stale lock file(s)", n)
	}

	// Write lock file
	if err := s.lock.Write(port, s.config.Token); err != nil {
		ln.Close()
		return 0, fmt.Errorf("write lock: %w", err)
	}

	// Write port file
	if err := s.writePortFile(port); err != nil {
		s.log.Printf("warning: write port file: %v", err)
	}

	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Printf("server error: %v", err)
		}
	}()

	s.log.Printf("listening on 127.0.0.1:%d", port)
	return port, nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return nil
	}
	s.shutdown = true
	s.mu.Unlock()

	// Remove lock file
	if err := s.lock.Remove(s.config.Port); err != nil {
		s.log.Printf("warning: remove lock: %v", err)
	}

	// Close the notify broker only when the server owns it.
	// When an external broker is injected (WithBroker), it outlives the
	// server so that GUI subscriptions survive server restarts.
	if s.ownsBroker {
		s.notifyBroker.Close()
	}

	return s.httpSrv.Shutdown(ctx)
}

// Port returns the listening port.
func (s *Server) Port() int {
	return s.config.Port
}

// State returns the server's shared state (for testing).
func (s *Server) State() *State {
	return s.state
}

// RuntimeDir returns the runtime directory path.
func (s *Server) RuntimeDir() string {
	return s.config.RuntimeDir
}

// SetSessionLister sets the provider used by GET /msg/sessions to enumerate
// known sessions. It is safe to call after New() and before the first request.
// Passing nil clears the lister (the endpoint will return an empty array).
func (s *Server) SetSessionLister(sl SessionLister) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionLister = sl
}

// SetSessionCreator sets the provider used by POST /msg/create to spawn sessions.
// It is safe to call after New() and before the first request.
func (s *Server) SetSessionCreator(sc SessionCreator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionCreator = sc
}

// NotifyBroker returns the event broker that publishes model.Event when a
// tool permission request arrives via /notify. When no WithBroker option is
// used, the broker is created with the server and closed on Stop(). When an
// external broker is injected via WithBroker, it outlives the server.
func (s *Server) NotifyBroker() *event.Broker[model.Event] {
	return s.notifyBroker
}

// setActivity records the hook-based activity state for a tmux window.
// Called directly by hook handlers (not via broker subscription) to avoid
// inflating HasSubscribers() and altering broker-vs-file dispatch logic.
func (s *Server) setActivity(window string, state model.ActivityState, toolName string) {
	if window == "" {
		return
	}
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	s.activityMap[window] = activityEntry{State: state, ToolName: toolName}
}

// stopReasonToActivity maps a stop_reason string to an ActivityState.
func stopReasonToActivity(reason string) model.ActivityState {
	switch reason {
	case "error", "interrupt":
		return model.ActivityError
	default:
		return model.ActivityIdle
	}
}

// WindowActivity returns the current activity state for a tmux window.
func (s *Server) WindowActivity(window string) (model.ActivityState, string) {
	s.activityMu.RLock()
	defer s.activityMu.RUnlock()
	e, ok := s.activityMap[window]
	if !ok {
		return model.ActivityUnknown, ""
	}
	return e.State, e.ToolName
}

// enrichWithActivity overlays hook-based activity state onto session info.
func (s *Server) enrichWithActivity(sessions []SessionInfo) {
	s.activityMu.RLock()
	defer s.activityMu.RUnlock()
	for i := range sessions {
		if sessions[i].Window == "" {
			sessions[i].Activity = model.ActivityUnknown.String()
			continue
		}
		// Local sessions: activityMap is keyed by tmux window ID (e.g. "@43"),
		// resolved from local PID walks in resolveNotifyWindow.
		if e, ok := s.activityMap[sessions[i].Window]; ok {
			sessions[i].Activity = e.State.String()
			continue
		}
		// SSH sessions: activityMap is keyed by window NAME (e.g. "lc-2c86ae79"),
		// resolved from the pending window file. Compute the window name from
		// session ID and check as fallback. Mirrors session.WindowName() logic.
		if id := sessions[i].ID; id != "" {
			wName := "lc-" + id
			if len(id) > 8 {
				wName = "lc-" + id[:8]
			}
			if e, ok := s.activityMap[wName]; ok {
				sessions[i].Activity = e.State.String()
				continue
			}
		}
		sessions[i].Activity = model.ActivityUnknown.String()
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Verify auth token (header only — never accept via URL query to avoid log leakage)
	token := extractAuthToken(r)
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.config.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
	})
	if err != nil {
		s.log.Printf("ws accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	connID := fmt.Sprintf("ws-%s", r.RemoteAddr)
	s.log.Printf("ws connected: %s", connID)

	ctx := r.Context()
	s.serveConn(ctx, conn, connID)

	s.state.RemoveConn(connID)
	s.log.Printf("ws disconnected: %s", connID)
}

func (s *Server) serveConn(ctx context.Context, conn *websocket.Conn, connID string) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return
			}
			s.log.Printf("ws read %s: %v", connID, err)
			return
		}

		req, err := ParseRequest(data)
		if err != nil {
			s.log.Printf("ws parse %s: %v", connID, err)
			continue
		}

		resp := s.handler.HandleMessage(ctx, connID, req)
		if resp == nil {
			continue
		}

		respData, err := MarshalResponse(*resp)
		if err != nil {
			s.log.Printf("ws marshal %s: %v", connID, err)
			continue
		}

		if err := conn.Write(ctx, websocket.MessageText, respData); err != nil {
			s.log.Printf("ws write %s: %v", connID, err)
			return
		}
	}
}

type notifyRequest struct {
	Type      string          `json:"type,omitempty"`       // "tool_info" or "" (permission_prompt)
	PID       int             `json:"pid"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"` // object from Claude Code hooks
	Input     string          `json:"input,omitempty"`      // string (backward compat with curl tests)
	CWD       string          `json:"cwd,omitempty"`
	Message   string          `json:"message,omitempty"`    // from Notification hook
}

// toolInputString returns tool_input as a string, handling both object and string forms.
func (r *notifyRequest) toolInputString() string {
	if len(r.ToolInput) > 0 {
		return string(r.ToolInput)
	}
	return r.Input
}

// extractAuthToken reads the auth token from either header name.
// Claude Code hooks send "X-Claude-Code-Ide-Authorization",
// direct curl tests use "X-Auth-Token".
func extractAuthToken(r *http.Request) string {
	if t := r.Header.Get("X-Claude-Code-Ide-Authorization"); t != "" {
		return t
	}
	return r.Header.Get("X-Auth-Token")
}

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := extractAuthToken(r)
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.config.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB cap
	var req notifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.PID <= 0 {
		http.Error(w, "invalid pid", http.StatusBadRequest)
		return
	}

	window := s.resolveNotifyWindow(r.Context(), req.PID)
	if window == "" && req.Type != "tool_info" {
		// Fallback for permission_prompt: hook processes may have different PIDs
		// from the earlier PreToolUse hook. Use the most recent pending window.
		window = s.state.LastPendingWindow()
		if window != "" {
			s.log.Printf("notify: pid=%d not found, using last pending window %q", req.PID, window)
		}
	}
	if window == "" {
		s.log.Printf("notify: window not found for pid=%d type=%s tool=%s", req.PID, req.Type, req.ToolName)
		http.Error(w, "window not found", http.StatusNotFound)
		return
	}
	// Cache for future lookups from same PID. Use a fixed "hook-" prefix so all
	// hook types share one entry per PID, avoiding unbounded accumulation.
	s.state.SetConn(fmt.Sprintf("hook-%d", req.PID), &ConnState{PID: req.PID, Window: window})

	s.log.Printf("notify: type=%s pid=%d window=%s tool=%s", req.Type, req.PID, window, req.ToolName)

	switch req.Type {
	case "tool_info":
		// Phase 1: PreToolUse hook — store tool info for later popup trigger
		s.state.SetPending(window, PendingTool{
			ToolName: req.ToolName,
			Input:    req.toolInputString(),
			CWD:      req.CWD,
		})
		// Also signal Running state with the tool name for sidebar display.
		s.setActivity(window, model.ActivityRunning, req.ToolName)
		s.notifyBroker.Publish(model.Event{ActivityNotification: &model.ActivityNotification{
			Window:    window,
			State:     model.ActivityRunning,
			ToolName:  req.ToolName,
			Timestamp: time.Now(),
		}})
	default:
		// Phase 2: Notification hook (permission_prompt) — trigger popup

		// Check if a diff choice was already made (openDiff popup completed)
		if key, ok := s.state.GetDiffChoice(window); ok {
			s.log.Printf("notify: using pending diff choice %q for window %s", key, window)
			go func() {
				time.Sleep(50 * time.Millisecond)
				target := "lazyclaude:" + window
				if err := s.tmux.SendKeys(context.Background(), target, key); err != nil {
					s.log.Printf("notify: send diff choice key: %v", err)
				}
			}()
			break
		}

		// Resolve tool info: prefer stored PreToolUse data, fall back to request fields.
		toolName, input, cwd := s.resolveToolInfo(window, req)
		if toolName != "" {
			s.dispatchToolNotification(window, toolName, input, cwd)
		} else {
			s.log.Printf("notify: DROPPED — empty toolName for window %s pid=%d (no pending and no tool_name in request)", window, req.PID)
		}
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"window": window}); err != nil {
		s.log.Printf("notify: encode response: %v", err)
	}
}

// resolveNotifyWindow determines which tmux window a /notify request belongs to.
// It checks the state cache first, then falls back to local PID walk, then to
// the pending-window file written for SSH remote sessions.
func (s *Server) resolveNotifyWindow(ctx context.Context, pid int) string {
	if window := s.state.WindowForPID(pid); window != "" {
		return window
	}

	// Try local tmux PID resolution
	if w2, err := tmux.FindWindowForPid(ctx, s.tmux, pid); err == nil && w2 != nil {
		return w2.ID
	}

	// Fallback for remote SSH sessions: read pending window file.
	// Keep the file — SSH hooks spawn new processes with varying PIDs,
	// so the file serves as a persistent fallback until overwritten by
	// the next session creation (Manager.Create).
	pending := filepath.Join(s.config.RuntimeDir, pendingWindowFile)
	if data, err := os.ReadFile(pending); err == nil {
		if w := strings.TrimSpace(string(data)); w != "" {
			s.log.Printf("notify: using pending remote window %q for pid %d", w, pid)
			return w
		}
	}

	return ""
}

// resolveToolInfo returns the effective tool name, input, and cwd for a permission-prompt
// notification. It prefers data stored by an earlier PreToolUse hook over the request fields.
func (s *Server) resolveToolInfo(window string, req notifyRequest) (toolName, input, cwd string) {
	toolName = req.ToolName
	input = req.toolInputString()
	cwd = req.CWD
	if pending, ok := s.state.GetPending(window); ok {
		s.log.Printf("notify: resolveToolInfo window=%s using pending tool=%s", window, pending.ToolName)
		toolName = pending.ToolName
		input = pending.Input
		if pending.CWD != "" {
			cwd = pending.CWD
		}
	} else {
		s.log.Printf("notify: resolveToolInfo window=%s no pending found, req.ToolName=%q", window, req.ToolName)
	}
	return toolName, input, cwd
}

// dispatchToolNotification builds a ToolNotification and delivers it via
// the appropriate single path: broker (TUI in-process) or display-popup (daemon).
func (s *Server) dispatchToolNotification(window, toolName, input, cwd string) {
	s.setActivity(window, model.ActivityNeedsInput, toolName)
	// Detect max option from Claude's permission dialog.
	// Use bare window ID (e.g., "@1") — tmux resolves it across sessions.
	maxOpt := 3
	if content, capErr := s.tmux.CapturePaneANSI(context.Background(), window); capErr == nil {
		maxOpt = tmuxadapter.DetectMaxOption(content)
	}

	n := model.ToolNotification{
		ToolName:  toolName,
		Input:     input,
		CWD:       cwd,
		Window:    window,
		Timestamp: time.Now(),
		MaxOption: maxOpt,
	}

	// NOTE: HasSubscribers() and Publish() acquire separate locks, so a
	// subscriber could Cancel() between the check and the Publish(). In that
	// narrow window the event is silently dropped. This is acceptable because
	// (a) it only happens during TUI shutdown, and (b) Publish is non-blocking
	// by design — a dropped event during shutdown has no user impact.
	if s.notifyBroker.HasSubscribers() {
		// TUI is in-process and subscribed — broker delivers directly.
		// No disk enqueue needed (broker bypasses file polling).
		s.notifyBroker.Publish(model.Event{Notification: &n})
		s.log.Printf("notify: delivered via broker for window %s", window)
	} else {
		// No TUI subscriber — daemon mode or TUI not running.
		// Enqueue to disk for file-based polling.
		s.log.Printf("notify: no TUI subscriber for window %s tool=%s, enqueueing to disk", window, toolName)
		if err := notify.Enqueue(s.config.RuntimeDir, n); err != nil {
			s.log.Printf("notify: write notification: %v", err)
		}
	}
}

func (s *Server) writePortFile(port int) error {
	if s.config.PortFile == "" {
		return nil
	}
	return os.WriteFile(s.config.PortFile, []byte(strconv.Itoa(port)), 0o600)
}

type stopRequest struct {
	PID        int    `json:"pid"`
	StopReason string `json:"stop_reason"`
	SessionID  string `json:"session_id"`
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := extractAuthToken(r)
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.config.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req stopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	window := s.resolveNotifyWindow(r.Context(), req.PID)
	if window == "" {
		window = s.state.LastPendingWindow()
	}
	// Cache PID→window so subsequent hooks with the same PID resolve instantly.
	if window != "" && req.PID > 0 {
		s.state.SetConn(fmt.Sprintf("hook-%d", req.PID), &ConnState{PID: req.PID, Window: window})
	}

	s.log.Printf("stop: pid=%d window=%s reason=%s", req.PID, window, req.StopReason)
	s.setActivity(window, stopReasonToActivity(req.StopReason), "")

	n := model.StopNotification{
		Window:     window,
		StopReason: req.StopReason,
		SessionID:  req.SessionID,
		Timestamp:  time.Now(),
	}
	s.notifyBroker.Publish(model.Event{StopNotification: &n})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

type sessionStartRequest struct {
	PID       int    `json:"pid"`
	SessionID string `json:"session_id"`
}

func (s *Server) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := extractAuthToken(r)
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.config.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req sessionStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	window := s.resolveNotifyWindow(r.Context(), req.PID)
	if window == "" {
		window = s.state.LastPendingWindow()
	}
	// Cache PID→window so subsequent hooks with the same PID resolve instantly.
	if window != "" && req.PID > 0 {
		s.state.SetConn(fmt.Sprintf("hook-%d", req.PID), &ConnState{PID: req.PID, Window: window})
	}

	s.log.Printf("session-start: pid=%d window=%s", req.PID, window)
	s.setActivity(window, model.ActivityRunning, "")

	n := model.SessionStartNotification{
		Window:    window,
		SessionID: req.SessionID,
		Timestamp: time.Now(),
	}
	s.notifyBroker.Publish(model.Event{SessionStartNotification: &n})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

type promptSubmitRequest struct {
	PID       int    `json:"pid"`
	SessionID string `json:"session_id"`
}

func (s *Server) handlePromptSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := extractAuthToken(r)
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.config.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req promptSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	window := s.resolveNotifyWindow(r.Context(), req.PID)
	if window == "" {
		window = s.state.LastPendingWindow()
	}
	if window == "" {
		s.log.Printf("prompt-submit: window not found for pid=%d", req.PID)
		http.Error(w, "window not found", http.StatusNotFound)
		return
	}
	// Cache PID→window so subsequent hooks with the same PID resolve instantly.
	if req.PID > 0 {
		s.state.SetConn(fmt.Sprintf("hook-%d", req.PID), &ConnState{PID: req.PID, Window: window})
	}

	s.log.Printf("prompt-submit: pid=%d window=%s", req.PID, window)
	s.setActivity(window, model.ActivityRunning, "")

	n := model.PromptSubmitNotification{
		Window:    window,
		SessionID: req.SessionID,
		Timestamp: time.Now(),
	}
	s.notifyBroker.Publish(model.Event{PromptSubmitNotification: &n})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}
