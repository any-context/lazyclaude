package tmuxadapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/KEMSHlM/lazyclaude/internal/core/choice"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
)

const sessionPrefix = "lazyclaude"

// choiceToKey maps a Choice to the key Claude Code's permission dialog expects.
// Single key press selects immediately (no Enter needed).
var choiceToKey = map[choice.Choice]string{
	choice.Accept: "1",
	choice.Allow:  "2",
	choice.Reject: "3",
}

// SendToPane sends the user's choice as a key to Claude Code's permission dialog
// in the specified tmux pane. It detects the max option count from the pane content
// and clamps the key if needed.
//
// If the choice is Cancel, no key is sent (safe no-op).
// The window parameter is a bare window ID (e.g., "@3") or a full target
// (e.g., "lazyclaude:@3"). If no session prefix is present, "lazyclaude:" is prepended.
func SendToPane(ctx context.Context, client tmux.Client, window string, c choice.Choice) error {
	if c == choice.Cancel {
		return nil
	}

	key, ok := choiceToKey[c]
	if !ok {
		return nil
	}

	target := window
	if !strings.Contains(window, ":") {
		target = sessionPrefix + ":" + window
	}

	// Detect max option from the pane content and clamp
	paneContent, err := client.CapturePaneANSI(ctx, target)
	if err != nil {
		// If capture fails, send the key anyway (best-effort)
		return client.SendKeys(ctx, target, key)
	}

	maxOpt := DetectMaxOption(paneContent)
	keyNum := int(c)
	if keyNum > maxOpt {
		key = fmt.Sprintf("%d", maxOpt)
	}

	return client.SendKeys(ctx, target, key)
}
