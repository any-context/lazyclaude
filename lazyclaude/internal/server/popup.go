package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
)

// PopupOrchestrator spawns tool/diff popups inside tmux display-popup.
type PopupOrchestrator struct {
	binary string      // path to lazyclaude binary
	tmux   tmux.Client // for display-popup, show-message
	log    *log.Logger
}

// NewPopupOrchestrator creates a popup orchestrator.
func NewPopupOrchestrator(binary string, tmuxClient tmux.Client, logger *log.Logger) *PopupOrchestrator {
	return &PopupOrchestrator{
		binary: binary,
		tmux:   tmuxClient,
		log:    logger,
	}
}

// SpawnToolPopup spawns a tool confirmation popup via tmux display-popup.
// The popup runs `lazyclaude tool --send-keys --window <W>` with env vars.
// Returns immediately; the popup runs in a goroutine.
func (p *PopupOrchestrator) SpawnToolPopup(ctx context.Context, window, toolName, toolInput, toolCWD string) {
	go func() {
		w, h := EstimatePopupSize(toolName, toolInput, 200, 50) // TODO: get real term size
		cmd := fmt.Sprintf("%s tool --window %s --send-keys", p.binary, window)
		env := map[string]string{
			"TOOL_NAME":  toolName,
			"TOOL_INPUT": toolInput,
			"TOOL_CWD":   toolCWD,
		}
		// Pass tmux socket so popup's --send-keys uses the correct server
		if s := os.Getenv("LAZYCLAUDE_TMUX_SOCKET"); s != "" {
			env["LAZYCLAUDE_TMUX_SOCKET"] = s
		}
		opts := tmux.PopupOpts{
			Target: window,
			Width:  w,
			Height: h,
			Cmd:    cmd,
			Env:    env,
		}
		if err := p.tmux.DisplayPopup(ctx, opts); err != nil {
			p.log.Printf("popup: spawn tool: %v", err)
		}
	}()
}

// SpawnDiffPopup spawns a diff viewer popup via tmux display-popup.
// Blocks until the popup closes so the caller can read the choice file.
func (p *PopupOrchestrator) SpawnDiffPopup(ctx context.Context, window, oldPath, newContentsFile string) {
	cmd := fmt.Sprintf("%s diff --window %s --send-keys --old %s --new %s",
		p.binary, window, oldPath, newContentsFile)
	env := map[string]string{}
	if s := os.Getenv("LAZYCLAUDE_TMUX_SOCKET"); s != "" {
		env["LAZYCLAUDE_TMUX_SOCKET"] = s
	}
	opts := tmux.PopupOpts{
		Target: window,
		Width:  80,
		Height: 80,
		Cmd:    cmd,
		Env:    env,
	}
	if err := p.tmux.DisplayPopup(ctx, opts); err != nil {
		p.log.Printf("popup: spawn diff: %v", err)
	}
}

// EstimatePopupSize returns width and height percentages for a popup
// based on tool name and input length.
func EstimatePopupSize(toolName, toolInput string, termW, termH int) (wPct, hPct int) {
	inputLen := len(toolInput)

	// Base size depends on tool type
	switch {
	case toolName == "Bash" || toolName == "bash":
		wPct = 60
		hPct = 40
	case toolName == "Read" || toolName == "Glob" || toolName == "Grep":
		wPct = 50
		hPct = 30
	case toolName == "Write" || toolName == "Edit":
		wPct = 70
		hPct = 60
	case toolName == "Agent":
		wPct = 60
		hPct = 50
	default:
		wPct = 55
		hPct = 35
	}

	// Scale up for long inputs
	if inputLen > 200 {
		wPct += 10
	}
	if inputLen > 500 {
		hPct += 10
	}
	if inputLen > 1000 {
		wPct += 5
		hPct += 10
	}

	// Clamp
	if wPct > 90 {
		wPct = 90
	}
	if hPct > 90 {
		hPct = 90
	}

	// Ensure minimum sizes
	if wPct < 30 {
		wPct = 30
	}
	if hPct < 20 {
		hPct = 20
	}

	// Reduce for small terminals
	if termW < 80 {
		wPct = 90
	}
	if termH < 24 {
		hPct = 90
	}

	return wPct, hPct
}

// isDiffTool returns true if the tool produces diffs (Edit, Write, MultiEdit, NotebookEdit).
func isDiffTool(toolName string) bool {
	switch strings.ToLower(toolName) {
	case "edit", "write", "multiedit", "notebookedit":
		return true
	}
	return false
}
