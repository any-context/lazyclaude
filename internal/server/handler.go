package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/any-context/lazyclaude/internal/core/model"
	"github.com/any-context/lazyclaude/internal/core/tmux"
	"github.com/any-context/lazyclaude/internal/notify"
)

// Handler processes MCP protocol messages.
type Handler struct {
	state      *State
	tmux       tmux.Client
	log        *log.Logger
	runtimeDir string // for writing notification files
}

// NewHandler creates an MCP message handler.
func NewHandler(state *State, tmuxClient tmux.Client, logger *log.Logger) *Handler {
	return &Handler{
		state: state,
		tmux:  tmuxClient,
		log:   logger,
	}
}

// SetRuntimeDir sets the runtime directory for notification files.
func (h *Handler) SetRuntimeDir(dir string) {
	h.runtimeDir = dir
}

// HandleMessage processes a single JSON-RPC request and returns an optional response.
// Returns nil response for notifications that need no reply.
func (h *Handler) HandleMessage(ctx context.Context, connID string, req *Request) *Response {
	switch req.Method {
	case "initialize":
		return h.handleInitialize(req)
	case "notifications/initialized":
		return nil // no response needed
	case "ide_connected":
		h.handleIDEConnected(ctx, connID, req)
		return nil
	case "openDiff":
		return h.handleOpenDiff(ctx, connID, req)
	default:
		if req.IsNotification() {
			return nil
		}
		resp := NewErrorResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
		return &resp
	}
}

// MCP capabilities returned during initialization.
type initializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      serverInfo     `json:"serverInfo"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (h *Handler) handleInitialize(req *Request) *Response {
	result := initializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{},
		ServerInfo: serverInfo{
			Name:    "lazyclaude",
			Version: "0.1.0",
		},
	}
	resp := NewResponse(req.ID, result)
	return &resp
}

type ideConnectedParams struct {
	PID int `json:"pid"`
}

func (h *Handler) handleIDEConnected(ctx context.Context, connID string, req *Request) {
	var params ideConnectedParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		h.log.Printf("ide_connected: invalid params: %v", err)
		return
	}
	if params.PID <= 0 {
		h.log.Printf("ide_connected: invalid pid: %d", params.PID)
		return
	}

	window, err := h.resolveWindow(ctx, params.PID)
	if err != nil {
		h.log.Printf("ide_connected: local resolve failed for pid %d: %v", params.PID, err)
	}

	if window == "" {
		h.log.Printf("ide_connected: no window found for pid %d", params.PID)
		return
	}

	h.state.SetConn(connID, &ConnState{
		PID:    params.PID,
		Window: window,
	})
	h.log.Printf("ide_connected: pid=%d window=%s", params.PID, window)
}

func (h *Handler) resolveWindow(ctx context.Context, pid int) (string, error) {
	// Check cache first
	if w := h.state.WindowForPID(pid); w != "" {
		return w, nil
	}

	// Walk process tree
	w, err := tmux.FindWindowForPid(ctx, h.tmux, pid)
	if err != nil {
		return "", err
	}
	if w != nil {
		return w.ID, nil
	}
	return "", nil
}

type openDiffParams struct {
	OldFilePath string `json:"old_file_path"`
	NewContents string `json:"new_contents"`
}

func validateFilePath(path string) error {
	if path == "" {
		return fmt.Errorf("empty file path")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %s", path)
	}
	return nil
}

func (h *Handler) handleOpenDiff(ctx context.Context, connID string, req *Request) *Response {
	var params openDiffParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		resp := NewErrorResponse(req.ID, -32602, "invalid params")
		return &resp
	}

	if err := validateFilePath(params.OldFilePath); err != nil {
		resp := NewErrorResponse(req.ID, -32602, err.Error())
		return &resp
	}

	cs := h.state.GetConn(connID)
	if cs == nil || cs.Window == "" {
		resp := NewErrorResponse(req.ID, -32603, "connection not registered")
		return &resp
	}

	h.log.Printf("openDiff: window=%s file=%s", cs.Window, params.OldFilePath)

	// Write notification file for TUI overlay
	if h.runtimeDir != "" {
		n := model.ToolNotification{
			ToolName:    "Diff",
			OldFilePath: params.OldFilePath,
			NewContents: params.NewContents,
			Window:      cs.Window,
			Timestamp:   time.Now(),
		}
		if enqErr := notify.Enqueue(h.runtimeDir, n); enqErr != nil {
			h.log.Printf("openDiff: write notification: %v", enqErr)
		}
	}

	resp := NewResponse(req.ID, map[string]string{
		"window":   cs.Window,
		"old_path": params.OldFilePath,
	})
	return &resp
}
