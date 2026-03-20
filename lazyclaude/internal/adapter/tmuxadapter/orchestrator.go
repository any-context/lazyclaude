package tmuxadapter

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
)

// toolPopupReq holds a queued tool popup request.
type toolPopupReq struct {
	window   string
	toolName string
	input    string
	cwd      string
}

// PopupOrchestrator spawns tool/diff popups inside tmux display-popup.
// For tool popups, it queues requests per window so only one popup
// is active at a time per window.
type PopupOrchestrator struct {
	binary string      // path to lazyclaude binary
	tmux   tmux.Client // for display-popup, show-message
	log    *log.Logger

	mu     sync.Mutex
	active map[string]bool          // window -> popup currently open
	queues map[string][]toolPopupReq // window -> queued requests
}

// NewPopupOrchestrator creates a popup orchestrator.
func NewPopupOrchestrator(binary string, tmuxClient tmux.Client, logger *log.Logger) *PopupOrchestrator {
	return &PopupOrchestrator{
		binary: binary,
		tmux:   tmuxClient,
		log:    logger,
		active: make(map[string]bool),
		queues: make(map[string][]toolPopupReq),
	}
}

// QueueLen returns the number of queued (not active) popups for a window.
func (p *PopupOrchestrator) QueueLen(window string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.queues[window])
}

// SpawnToolPopup spawns a tool confirmation popup via tmux display-popup.
// If a popup is already active for this window, the request is queued.
// Returns immediately; the popup runs in a goroutine.
func (p *PopupOrchestrator) SpawnToolPopup(ctx context.Context, window, toolName, toolInput, toolCWD string) {
	req := toolPopupReq{
		window:   window,
		toolName: toolName,
		input:    toolInput,
		cwd:      toolCWD,
	}

	p.mu.Lock()
	if p.active[window] {
		p.queues[window] = append(p.queues[window], req)
		p.mu.Unlock()
		p.log.Printf("popup: queued tool %s for window %s (queue=%d)", toolName, window, len(p.queues[window]))
		return
	}
	p.active[window] = true
	p.mu.Unlock()

	go p.runToolPopup(ctx, req)
}

// runToolPopup executes a single tool popup and drains the queue on exit.
func (p *PopupOrchestrator) runToolPopup(ctx context.Context, req toolPopupReq) {
	p.spawnToolPopupBlocking(ctx, req)

	// Drain queue: check for next request after popup closes
	for {
		time.Sleep(200 * time.Millisecond)

		p.mu.Lock()
		q := p.queues[req.window]
		if len(q) == 0 {
			delete(p.active, req.window)
			delete(p.queues, req.window)
			p.mu.Unlock()
			return
		}
		next := q[0]
		p.queues[req.window] = q[1:]
		p.mu.Unlock()

		p.spawnToolPopupBlocking(ctx, next)
	}
}

// spawnToolPopupBlocking runs a single tool popup (blocks until close).
func (p *PopupOrchestrator) spawnToolPopupBlocking(ctx context.Context, req toolPopupReq) {
	w, h := EstimatePopupSize(req.toolName, req.input, 200, 50)
	cmd := fmt.Sprintf("%s tool --window %s --send-keys", p.binary, req.window)
	env := map[string]string{
		"TOOL_NAME":  req.toolName,
		"TOOL_INPUT": req.input,
		"TOOL_CWD":   req.cwd,
	}
	if s := os.Getenv("LAZYCLAUDE_TMUX_SOCKET"); s != "" {
		env["LAZYCLAUDE_TMUX_SOCKET"] = s
	}
	opts := tmux.PopupOpts{
		Target: req.window,
		Width:  w,
		Height: h,
		Cmd:    cmd,
		Env:    env,
	}
	// Find active client so display-popup can attach to it.
	// Without a client, display-popup fails with "no current client".
	if client, err := p.tmux.FindActiveClient(ctx); err == nil && client != nil {
		opts.Client = client.Name
	}
	if err := p.tmux.DisplayPopup(ctx, opts); err != nil {
		p.log.Printf("popup: spawn tool: %v", err)
	}
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
	if client, err := p.tmux.FindActiveClient(ctx); err == nil && client != nil {
		opts.Client = client.Name
	}
	if err := p.tmux.DisplayPopup(ctx, opts); err != nil {
		p.log.Printf("popup: spawn diff: %v", err)
	}
}
