package daemon

import (
	"context"
	"strconv"
	"strings"

	"github.com/any-context/lazyclaude/internal/core/tmux"
)

// CapturePreviewContent captures the content and cursor position from a tmux
// pane. This is the shared implementation used by both the daemon server
// (handlePreview) and the local provider (CapturePreview).
//
// It performs:
//  1. Capture pane content with ANSI escape codes
//  2. Query cursor position via tmux display-message
//
// Resize must be performed by the caller before calling this function,
// because resize deduplication logic differs between callers.
func CapturePreviewContent(ctx context.Context, tc tmux.Client, target string) (*PreviewResponse, error) {
	content, err := tc.CapturePaneANSI(ctx, target)
	if err != nil {
		return nil, err
	}

	var cursorX, cursorY int
	if pos, posErr := tc.ShowMessage(ctx, target, "#{cursor_x},#{cursor_y}"); posErr == nil {
		parts := strings.SplitN(strings.TrimSpace(pos), ",", 2)
		if len(parts) == 2 {
			cursorX, _ = strconv.Atoi(parts[0])
			cursorY, _ = strconv.Atoi(parts[1])
		}
	}

	return &PreviewResponse{
		Content: content,
		CursorX: cursorX,
		CursorY: cursorY,
	}, nil
}
