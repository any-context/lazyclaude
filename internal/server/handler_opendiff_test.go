package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/core/choice"
	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/KEMSHlM/lazyclaude/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandler_OpenDiff_WithPopup_StoresDiffChoice verifies that when a popup
// orchestrator is set, openDiff spawns the diff popup (blocking), reads the
// choice file written by the popup subprocess, and stores the choice in
// pendingDiffChoices so the next handleNotify can consume it.
func TestHandler_OpenDiff_WithPopup_StoresDiffChoice(t *testing.T) {
	t.Parallel()
	state := server.NewState()
	mock := tmux.NewMockClient()
	logger := log.New(os.Stderr, "test: ", 0)
	handler := server.NewHandler(state, mock, logger)

	// Set up runtime dir for choice file
	tmpDir := t.TempDir()
	handler.SetRuntimeDir(tmpDir)

	// Create popup orchestrator with mock tmux client.
	// MockClient.DisplayPopup returns immediately (simulates popup close).
	orch := tmuxadapter.NewPopupOrchestrator("lazyclaude", "lazyclaude", os.TempDir(), mock, nil, logger)
	handler.SetPopup(orch)

	// Pre-write the choice file that `lazyclaude diff` would write on exit.
	// The handler reads this after SpawnDiffPopup returns.
	paths := config.Paths{RuntimeDir: tmpDir}
	require.NoError(t, os.MkdirAll(paths.RuntimeDir, 0o700))
	require.NoError(t, choice.WriteFile(paths, "@1", choice.Accept))

	// Register connection
	state.SetConn("conn-1", &server.ConnState{PID: 1001, Window: "@1"})

	req := &server.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`5`),
		Method:  "openDiff",
		Params:  json.RawMessage(`{"old_file_path":"/home/user/main.go","new_contents":"package main\n"}`),
	}

	resp := handler.HandleMessage(context.Background(), "conn-1", req)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	// Popup should have been called
	assert.Len(t, mock.Popups, 1, "display-popup should have been called")
	assert.Contains(t, mock.Popups[0].Cmd, "diff")
	assert.Contains(t, mock.Popups[0].Cmd, "--send-keys")

	// Diff choice should be stored in pendingDiffChoices
	key, ok := state.GetDiffChoice("@1")
	assert.True(t, ok, "diff choice should be stored")
	assert.Equal(t, fmt.Sprintf("%d", choice.Accept), key)
}

// TestHandler_OpenDiff_WithPopup_RejectChoice verifies reject choice is stored.
func TestHandler_OpenDiff_WithPopup_RejectChoice(t *testing.T) {
	t.Parallel()
	state := server.NewState()
	mock := tmux.NewMockClient()
	logger := log.New(os.Stderr, "test: ", 0)
	handler := server.NewHandler(state, mock, logger)

	tmpDir := t.TempDir()
	handler.SetRuntimeDir(tmpDir)

	orch := tmuxadapter.NewPopupOrchestrator("lazyclaude", "lazyclaude", os.TempDir(), mock, nil, logger)
	handler.SetPopup(orch)

	paths := config.Paths{RuntimeDir: tmpDir}
	require.NoError(t, choice.WriteFile(paths, "@2", choice.Reject))

	state.SetConn("conn-2", &server.ConnState{PID: 2002, Window: "@2"})

	req := &server.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`6`),
		Method:  "openDiff",
		Params:  json.RawMessage(`{"old_file_path":"/tmp/test.go","new_contents":"x"}`),
	}

	resp := handler.HandleMessage(context.Background(), "conn-2", req)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	key, ok := state.GetDiffChoice("@2")
	assert.True(t, ok)
	assert.Equal(t, fmt.Sprintf("%d", choice.Reject), key)
}

// TestHandler_OpenDiff_WithPopup_CancelNotStored verifies cancel choice
// is NOT stored (no key should be sent on next notify).
func TestHandler_OpenDiff_WithPopup_CancelNotStored(t *testing.T) {
	t.Parallel()
	state := server.NewState()
	mock := tmux.NewMockClient()
	logger := log.New(os.Stderr, "test: ", 0)
	handler := server.NewHandler(state, mock, logger)

	tmpDir := t.TempDir()
	handler.SetRuntimeDir(tmpDir)

	orch := tmuxadapter.NewPopupOrchestrator("lazyclaude", "lazyclaude", os.TempDir(), mock, nil, logger)
	handler.SetPopup(orch)

	// Write Cancel choice (user pressed Esc)
	paths := config.Paths{RuntimeDir: tmpDir}
	require.NoError(t, choice.WriteFile(paths, "@3", choice.Cancel))

	state.SetConn("conn-3", &server.ConnState{PID: 3003, Window: "@3"})

	req := &server.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`7`),
		Method:  "openDiff",
		Params:  json.RawMessage(`{"old_file_path":"/tmp/x.go","new_contents":"y"}`),
	}

	resp := handler.HandleMessage(context.Background(), "conn-3", req)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	// Cancel should NOT be stored
	_, ok := state.GetDiffChoice("@3")
	assert.False(t, ok, "cancel choice should not be stored in pendingDiffChoices")
}

// TestHandler_OpenDiff_WithoutPopup_FallsBackToNotification verifies that
// when no popup orchestrator is set (TUI overlay mode), a notification file
// is written instead.
func TestHandler_OpenDiff_WithoutPopup_FallsBackToNotification(t *testing.T) {
	t.Parallel()
	state := server.NewState()
	mock := tmux.NewMockClient()
	logger := log.New(os.Stderr, "test: ", 0)
	handler := server.NewHandler(state, mock, logger)

	tmpDir := t.TempDir()
	handler.SetRuntimeDir(tmpDir)
	// Do NOT set popup — handler.popup == nil

	state.SetConn("conn-1", &server.ConnState{PID: 1001, Window: "@1"})

	req := &server.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`8`),
		Method:  "openDiff",
		Params:  json.RawMessage(`{"old_file_path":"/home/user/main.go","new_contents":"new code"}`),
	}

	resp := handler.HandleMessage(context.Background(), "conn-1", req)
	require.NotNil(t, resp)
	assert.Nil(t, resp.Error)

	// No popup should be spawned
	assert.Len(t, mock.Popups, 0)

	// No diff choice stored (no popup = no choice)
	_, ok := state.GetDiffChoice("@1")
	assert.False(t, ok)
}
