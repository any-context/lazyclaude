package presentation_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/stretchr/testify/assert"
)

func TestFormatSessionLine_Running(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("my-app", "Running", "", "", nil, 40)

	assert.Contains(t, line, "my-app")
	assert.Contains(t, line, "●") // green filled circle
}

func TestFormatSessionLine_Dead(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("my-app", "Dead", "", "", nil, 40)

	assert.Contains(t, line, "×") // red cross
}

func TestFormatSessionLine_Orphan(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("orphaned", "Orphan", "", "", nil, 40)

	assert.Contains(t, line, "○") // yellow empty circle
}

func TestFormatSessionLine_Detached(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("idle", "Detached", "", "", nil, 40)

	assert.Contains(t, line, "◆") // gray diamond
}

func TestFormatSessionLine_Unknown(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("new", "Unknown", "", "", nil, 40)

	assert.Contains(t, line, "?")
}

func TestFormatSessionLine_Remote(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("work", "Running", "srv1", "", nil, 40)

	assert.Contains(t, line, "srv1:work")
	assert.Contains(t, line, "●")
}

func TestFormatSessionLine_WithFlags(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("my-app", "Running", "", "", []string{"--resume"}, 40)

	assert.Contains(t, line, "R")
	assert.Contains(t, line, "●")
}

func TestFormatSessionLine_TruncateLongName(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("very-long-project-name-that-exceeds-width", "Running", "", "", nil, 30)

	// Status icons contain ANSI escapes, so byte length exceeds display width.
	// Just verify truncation marker is present and name is shortened.
	assert.Contains(t, line, "~") // truncation marker
	assert.NotContains(t, line, "very-long-project-name-that-exceeds-width")
}

func TestFormatSessionLines(t *testing.T) {
	t.Parallel()
	names := []string{"app", "lib", "work"}
	statuses := []string{"Running", "Detached", "Running"}
	hosts := []string{"", "", "srv1"}
	flags := [][]string{nil, nil, nil}

	paths := []string{"", "", ""}
	lines := presentation.FormatSessionLines(names, statuses, hosts, paths, flags, 40)
	assert.Len(t, lines, 3)
	assert.Contains(t, lines[0], "app")
	assert.Contains(t, lines[1], "lib")
	assert.Contains(t, lines[2], "srv1:work")
}

func TestFormatSessionLine_Worktree(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("fix-popup", "Running", "", "/project/.claude/worktrees/fix-popup", nil, 60)

	assert.Contains(t, line, "[W]")
	assert.Contains(t, line, "fix-popup")
	assert.Contains(t, line, "●")
}

func TestFormatSessionLine_NarrowWidth(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("app", "Running", "", "", nil, 10)

	// Should not panic, should produce some output
	assert.NotEmpty(t, line)
}

func TestServerStatusLine(t *testing.T) {
	t.Parallel()
	line := presentation.ServerStatusLine(7860, 2, "3h 24m")

	assert.Contains(t, line, ":7860")
	assert.Contains(t, line, "2")
	assert.Contains(t, line, "3h 24m")
}
