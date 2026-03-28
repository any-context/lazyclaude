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

	"github.com/KEMSHlM/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/KEMSHlM/lazyclaude/internal/core/event"
	"github.com/KEMSHlM/lazyclaude/internal/core/model"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/notify"
	"nhooyr.io/websocket"
)

// Config holds server configuration.
type Config struct {
	Port       int
	Token      string
	BinaryPath string
	IDEDir     string // lock files directory
	PortFile   string // path to write the listening port
	RuntimeDir string // choice files directory
	TmuxSocket string // lazyclaude tmux socket name (e.g., "lazyclaude")
}

// Server is the MCP WebSocket + HTTP server.
type Server struct {
	config        Config
	state         *State
	handler       *Handler
	lock          *LockManager
	popupOrch     *tmuxadapter.PopupOrchestrator
	tmux          tmux.Client
	log           *log.Logger
	notifyBroker  *event.Broker[model.Event]
	sessionLister SessionLister

	listener net.Listener
	httpSrv  *http.Server

	mu       sync.Mutex
	shutdown bool
}

// New creates a new Server.
func New(cfg Config, tmuxClient tmux.Client, logger *log.Logger) *Server {
	state := NewState()
	handler := NewHandler(state, tmuxClient, logger)
	handler.SetRuntimeDir(cfg.RuntimeDir)
	lockMgr := NewLockManager(cfg.IDEDir)

	// Create a tmux client for the user's tmux (for display-popup).
	// LAZYCLAUDE_HOST_TMUX contains the user's $TMUX socket path.
	var hostTmux tmux.Client
	if hostSocket := os.Getenv("LAZYCLAUDE_HOST_TMUX"); hostSocket != "" {
		// $TMUX format: "/path/to/socket,pid,session"
		parts := strings.SplitN(hostSocket, ",", 2)
		hostTmux = tmux.NewExecClientWithSocket(parts[0])
	}
	popupOrch := tmuxadapter.NewPopupOrchestrator(cfg.BinaryPath, cfg.TmuxSocket, cfg.RuntimeDir, tmuxClient, hostTmux, logger)
	handler.SetPopup(popupOrch)

	s := &Server{
		config:       cfg,
		state:        state,
		handler:      handler,
		lock:         lockMgr,
		popupOrch:    popupOrch,
		tmux:         tmuxClient,
		log:          logger,
		notifyBroker: event.NewBroker[model.Event](),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/notify", s.handleNotify)
	mux.HandleFunc("/msg/send", s.handleMsgSend)
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

	// Close the notify broker to release any waiting subscribers.
	s.notifyBroker.Close()

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

// NotifyBroker returns the event broker that publishes model.Event when a
// tool permission request arrives via /notify. The broker is created with the
// server and lives for the server's lifetime; call broker.Close() (or Stop()
// the server) to release subscribers.
func (s *Server) NotifyBroker() *event.Broker[model.Event] {
	return s.notifyBroker
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
	// Cache for future lookups from same PID
	s.state.SetConn(fmt.Sprintf("notify-%d", req.PID), &ConnState{PID: req.PID, Window: window})

	s.log.Printf("notify: type=%s pid=%d window=%s tool=%s", req.Type, req.PID, window, req.ToolName)

	switch req.Type {
	case "tool_info":
		// Phase 1: PreToolUse hook — store tool info for later popup trigger
		s.state.SetPending(window, PendingTool{
			ToolName: req.ToolName,
			Input:    req.toolInputString(),
			CWD:      req.CWD,
		})
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
	// Consumed after first use to match ide_connected behavior.
	pending := filepath.Join(s.config.RuntimeDir, pendingWindowFile)
	if data, err := os.ReadFile(pending); err == nil {
		if w := strings.TrimSpace(string(data)); w != "" {
			s.log.Printf("notify: using pending remote window %q for pid %d", w, pid)
			if rmErr := os.Remove(pending); rmErr != nil {
				s.log.Printf("notify: remove pending file: %v", rmErr)
			}
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
		toolName = pending.ToolName
		input = pending.Input
		if pending.CWD != "" {
			cwd = pending.CWD
		}
	}
	return toolName, input, cwd
}

// dispatchToolNotification builds a ToolNotification and delivers it via
// the appropriate single path: broker (TUI in-process) or display-popup (daemon).
func (s *Server) dispatchToolNotification(window, toolName, input, cwd string) {
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
		// No display-popup needed (TUI handles overlay in both normal and fullscreen).
		// No disk enqueue needed (broker bypasses file polling).
		s.notifyBroker.Publish(model.Event{Notification: &n})
		s.log.Printf("notify: delivered via broker for window %s", window)
	} else {
		// No TUI subscriber — daemon mode or TUI not running.
		// Enqueue to disk (SSH remote compat) and spawn display-popup.
		if err := notify.Enqueue(s.config.RuntimeDir, n); err != nil {
			s.log.Printf("notify: write notification: %v", err)
		}
		s.popupOrch.SpawnToolPopup(context.Background(), window, toolName, input, cwd)
	}
}

func (s *Server) writePortFile(port int) error {
	if s.config.PortFile == "" {
		return nil
	}
	return os.WriteFile(s.config.PortFile, []byte(strconv.Itoa(port)), 0o600)
}
