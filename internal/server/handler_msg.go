package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// SessionLister provides session metadata for the /msg/sessions endpoint.
type SessionLister interface {
	Sessions() []SessionInfo
}

// SessionCreator provides session creation for the /msg/create endpoint.
type SessionCreator interface {
	// FindProjectForSession returns the project owning the given session ID.
	FindProjectForSession(id string) *SessionProjectInfo
	// CreateWorkerSession creates a git worktree worker session.
	CreateWorkerSession(ctx context.Context, name, prompt, projectRoot string) (*SessionCreateResult, error)
	// CreateLocalSession creates a plain session at projectPath.
	CreateLocalSession(ctx context.Context, name, projectPath string) (*SessionCreateResult, error)
	// ResumeSession resumes a session by ID with a worktree name fallback
	// for sessions that have been GC'd from state.json.
	ResumeSession(ctx context.Context, id, prompt, name string) (*SessionCreateResult, error)
}

// SessionProjectInfo is the minimal project data needed by the handler.
type SessionProjectInfo struct {
	Path string
}

// SessionCreateResult is the data returned after creating a session.
type SessionCreateResult struct {
	ID     string
	Name   string
	Role   string
	Path   string
	Window string
}

// MsgCreateResponse is the JSON response for POST /msg/create.
type MsgCreateResponse struct {
	Status  string            `json:"status"`
	Session *MsgCreateSession `json:"session,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// MsgCreateSession is the session info returned in a create response.
type MsgCreateSession struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Path   string `json:"path"`
	Window string `json:"window"`
}

// SessionInfo is a lightweight session descriptor returned by /msg/sessions.
type SessionInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Path     string `json:"path"`
	Host     string `json:"host,omitempty"`     // SSH host (e.g. "user@host"); empty for local
	Window   string `json:"window,omitempty"`   // tmux window ID (e.g. "@1")
	Status   string `json:"status,omitempty"`   // runtime status string (e.g. "Running")
	Activity string `json:"activity"` // hook-based activity state (e.g. "running", "idle", "unknown")
}

type msgCreateRequest struct {
	From   string `json:"from"`
	Name   string `json:"name"`
	Type   string `json:"type"`   // "worker" or "local"
	Prompt string `json:"prompt"` // optional
}

// handleMsgCreate handles POST /msg/create.
// It creates a new session (worker or local) scoped to the caller's project.
func (s *Server) handleMsgCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := extractAuthToken(r)
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.config.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	sc := s.sessionCreator
	s.mu.RUnlock()

	if sc == nil {
		http.Error(w, "session creator not available", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req msgCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.From == "" || req.Name == "" {
		http.Error(w, "from and name are required", http.StatusBadRequest)
		return
	}

	if req.Type != "worker" && req.Type != "local" {
		http.Error(w, "type must be worker or local", http.StatusBadRequest)
		return
	}

	project := sc.FindProjectForSession(req.From)
	if project == nil {
		http.Error(w, "caller session not found", http.StatusNotFound)
		return
	}

	ctx := r.Context()
	var result *SessionCreateResult
	var err error

	switch req.Type {
	case "worker":
		result, err = sc.CreateWorkerSession(ctx, req.Name, req.Prompt, project.Path)
	case "local":
		result, err = sc.CreateLocalSession(ctx, req.Name, project.Path)
	}
	if err != nil {
		s.log.Printf("msg/create: %v", err)
		http.Error(w, "create session failed", http.StatusInternalServerError)
		return
	}
	if result == nil {
		s.log.Printf("msg/create: nil result with no error")
		http.Error(w, "create session failed", http.StatusInternalServerError)
		return
	}

	// For local sessions, send prompt via tmux if provided.
	if req.Type == "local" && req.Prompt != "" && result.Window != "" {
		target := "lazyclaude:" + result.Window
		if sendErr := s.tmux.SendKeysLiteral(ctx, target, req.Prompt); sendErr != nil {
			s.log.Printf("msg/create: send prompt to %s: %v", target, sendErr)
		} else if enterErr := s.tmux.SendKeys(ctx, target, "Enter"); enterErr != nil {
			s.log.Printf("msg/create: send Enter to %s: %v", target, enterErr)
		}
	}

	resp := MsgCreateResponse{
		Status: "created",
		Session: &MsgCreateSession{
			ID:     result.ID,
			Name:   result.Name,
			Role:   result.Role,
			Path:   result.Path,
			Window: result.Window,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Printf("msg/create: encode: %v", err)
	}
}

type msgSendRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
	Body string `json:"body"`
}

// handleMsgSend handles POST /msg/send.
// It resolves the recipient session, builds a message text, and pastes it
// directly into the recipient's tmux pane (push-based delivery).
func (s *Server) handleMsgSend(w http.ResponseWriter, r *http.Request) {
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
	var req msgSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.From == "" || req.To == "" {
		http.Error(w, "from and to are required", http.StatusBadRequest)
		return
	}

	if req.From == req.To {
		http.Error(w, "cannot send a message to yourself", http.StatusBadRequest)
		return
	}

	if !isValidMsgType(req.Type) {
		http.Error(w, "invalid message type", http.StatusBadRequest)
		return
	}

	const maxBodyLen = 10 * 1024 // 10 KB
	if len(req.Body) > maxBodyLen {
		http.Error(w, "body too large (max 10KB)", http.StatusBadRequest)
		return
	}

	// Resolve sessions from the lister.
	s.mu.RLock()
	sl := s.sessionLister
	s.mu.RUnlock()

	var sessions []SessionInfo
	if sl != nil {
		sessions = sl.Sessions()
	}
	if len(sessions) == 0 {
		sessions = s.readSessionsFromState()
	}

	// Find recipient and sender.
	var recipient *SessionInfo
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
		http.Error(w, "recipient session not found", http.StatusNotFound)
		return
	}

	// Resolve tmux window: prefer stored value, fall back to tmux query.
	// This avoids dependence on status detection being correct.
	window := recipient.Window
	if window == "" {
		// Compute window name from session ID and look it up directly in tmux.
		wName := "lc-" + recipient.ID
		if len(recipient.ID) > 8 {
			wName = "lc-" + recipient.ID[:8]
		}
		if windows, err := s.tmux.ListWindows(context.Background(), "lazyclaude"); err == nil {
			for _, win := range windows {
				if win.Name == wName {
					window = win.ID
					break
				}
			}
		}
	}
	if window == "" {
		http.Error(w, "recipient session has no tmux window", http.StatusBadGateway)
		return
	}

	// Build the message text delivered to the recipient's input.
	text := fmt.Sprintf("[MESSAGE from %s (%s)]\ntype: %s\n---\n%s\n",
		senderName, req.From, req.Type, req.Body)

	// Deliver directly via tmux send-keys. Let tmux report the error
	// if the pane doesn't exist — no status pre-check needed.
	target := "lazyclaude:" + window
	if err := s.tmux.SendKeysLiteral(context.Background(), target, text); err != nil {
		s.log.Printf("msg/send: send text to pane %s: %v", target, err)
		http.Error(w, "failed to deliver message", http.StatusBadGateway)
		return
	}
	if err := s.tmux.SendKeys(context.Background(), target, "Enter"); err != nil {
		s.log.Printf("msg/send: send Enter to pane %s: %v", target, err)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "delivered"}); err != nil {
		s.log.Printf("msg/send: encode: %v", err)
	}
}

// MsgResumeResponse is the JSON response for POST /msg/resume.
type MsgResumeResponse struct {
	Status  string            `json:"status"`
	Session *MsgCreateSession `json:"session,omitempty"`
	Error   string            `json:"error,omitempty"`
}

type msgResumeRequest struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt,omitempty"`
	Name   string `json:"name,omitempty"` // worktree name (for GC'd sessions)
}

// handleMsgResume handles POST /msg/resume.
// It resumes a session by ID, falling back to the worktree name when the
// session has been GC'd from state.json.
func (s *Server) handleMsgResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := extractAuthToken(r)
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.config.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	sc := s.sessionCreator
	s.mu.RUnlock()

	if sc == nil {
		http.Error(w, "session creator not available", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req msgResumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	result, err := sc.ResumeSession(ctx, req.ID, req.Prompt, req.Name)
	if err != nil {
		s.log.Printf("msg/resume: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := MsgResumeResponse{
		Status: "resumed",
		Session: &MsgCreateSession{
			ID:     result.ID,
			Name:   result.Name,
			Role:   result.Role,
			Path:   result.Path,
			Window: result.Window,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.log.Printf("msg/resume: encode: %v", err)
	}
}

// validMsgTypes is the allowlist of message types accepted by /msg/send.
var validMsgTypes = map[string]bool{
	"review_request":  true,
	"review_response": true,
	"status":          true,
	"done":            true,
	"issue":           true,
}

func isValidMsgType(t string) bool {
	return validMsgTypes[t]
}

// handleMsgSessions handles GET /msg/sessions.
// It returns all known sessions with their roles so PM/Worker can discover each other.
func (s *Server) handleMsgSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := extractAuthToken(r)
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.config.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	sl := s.sessionLister
	s.mu.RUnlock()

	var sessions []SessionInfo
	if sl != nil {
		sessions = sl.Sessions()
	}
	// Fallback: read state.json directly when SessionLister is not wired
	// (e.g. daemon mode, or in-process server before adapter is set).
	if len(sessions) == 0 {
		sessions = s.readSessionsFromState()
	}
	if sessions == nil {
		sessions = []SessionInfo{}
	}

	// Overlay hook-based activity state recorded by hook handlers.
	s.enrichWithActivity(sessions)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sessions); err != nil {
		s.log.Printf("msg/sessions: encode: %v", err)
	}
}

// stateSession mirrors the JSON shape of session.Session for state.json parsing.
type stateSession struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	Host string `json:"host,omitempty"`
	Role string `json:"role,omitempty"`
}

// readSessionsFromState reads state.json as a fallback when SessionLister is nil.
// It also queries tmux to populate the Window and Status fields.
func (s *Server) readSessionsFromState() []SessionInfo {
	// Use UserHomeDir for the standard data path.
	// Fall back to RuntimeDir-relative path only if UserHomeDir fails.
	home, err := os.UserHomeDir()
	var stateFile string
	if err == nil {
		stateFile = filepath.Join(home, ".local", "share", "lazyclaude", "state.json")
	} else {
		stateFile = filepath.Join(s.config.RuntimeDir, "..", "lazyclaude", "state.json")
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil
	}

	var raw []stateSession
	if err := json.Unmarshal(data, &raw); err != nil {
		s.log.Printf("msg/sessions: parse state.json: %v", err)
		return nil
	}

	// Query tmux to populate Window and Status when possible.
	windowByName := map[string]string{} // window name -> window ID
	paneByWindow := map[string]bool{}   // window ID -> is alive
	if s.tmux != nil {
		ctx := context.Background()
		if windows, err := s.tmux.ListWindows(ctx, "lazyclaude"); err == nil {
			for _, w := range windows {
				windowByName[w.Name] = w.ID
			}
		}
		if panes, err := s.tmux.ListPanes(ctx, ""); err == nil {
			for _, p := range panes {
				if !p.Dead && p.PID > 0 {
					paneByWindow[p.Window] = true
				}
			}
		}
	}

	sessions := make([]SessionInfo, len(raw))
	for i, r := range raw {
		info := SessionInfo{
			ID:   r.ID,
			Name: r.Name,
			Role: r.Role,
			Path: r.Path,
			Host: r.Host,
		}
		// Compute window name: "lc-" + first 8 chars of ID.
		wName := "lc-" + r.ID
		if len(r.ID) > 8 {
			wName = "lc-" + r.ID[:8]
		}
		if wID, ok := windowByName[wName]; ok {
			info.Window = wID
			if paneByWindow[wID] {
				info.Status = "Running"
			} else {
				info.Status = "Detached"
			}
		} else {
			info.Status = "Orphan"
		}
		sessions[i] = info
	}
	return sessions
}
