package presentation_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/stretchr/testify/assert"
)

func TestFormatSessionLine_Running(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("my-app", "Running", "", nil, 40)

	assert.Contains(t, line, "my-app")
	assert.Contains(t, line, "●") // green filled circle
}

func TestFormatSessionLine_Dead(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("my-app", "Dead", "", nil, 40)

	assert.Contains(t, line, "×") // red cross
}

func TestFormatSessionLine_Orphan(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("orphaned", "Orphan", "", nil, 40)

	assert.Contains(t, line, "○") // yellow empty circle
}

func TestFormatSessionLine_Detached(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("idle", "Detached", "", nil, 40)

	assert.Contains(t, line, "◆") // gray diamond
}

func TestFormatSessionLine_Unknown(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("new", "Unknown", "", nil, 40)

	assert.Contains(t, line, "?")
}

func TestFormatSessionLine_Remote(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("work", "Running", "srv1", nil, 40)

	assert.Contains(t, line, "srv1:work")
	assert.Contains(t, line, "●")
}

func TestFormatSessionLine_WithFlags(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("my-app", "Running", "", []string{"--resume"}, 40)

	assert.Contains(t, line, "R")
	assert.Contains(t, line, "●")
}

func TestFormatSessionLine_TruncateLongName(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("very-long-project-name-that-exceeds-width", "Running", "", nil, 30)

	assert.Contains(t, line, "~") // truncation marker
	assert.NotContains(t, line, "very-long-project-name-that-exceeds-width")
}

func TestFormatSessionLines(t *testing.T) {
	t.Parallel()
	names := []string{"app", "lib", "work"}
	statuses := []string{"Running", "Detached", "Running"}
	hosts := []string{"", "", "srv1"}
	flags := [][]string{nil, nil, nil}

	lines := presentation.FormatSessionLines(names, statuses, hosts, flags, 40)
	assert.Len(t, lines, 3)
	assert.Contains(t, lines[0], "app")
	assert.Contains(t, lines[1], "lib")
	assert.Contains(t, lines[2], "srv1:work")
}

func TestFormatSessionLine_NarrowWidth(t *testing.T) {
	t.Parallel()
	line := presentation.FormatSessionLine("app", "Running", "", nil, 10)

	assert.NotEmpty(t, line)
}

func TestFormatSessionLine_ANSIWidthHandling(t *testing.T) {
	t.Parallel()
	// Host prefix includes ANSI escapes — padding should use visual width, not byte length
	line := presentation.FormatSessionLine("app", "Running", "srv1", nil, 40)

	assert.Contains(t, line, "srv1:app")
	assert.Contains(t, line, "●")
	// Should NOT be truncated at width 40 (visual "srv1:app" is 8 chars)
	assert.NotContains(t, line, "~")
}

func TestServerStatusLine(t *testing.T) {
	t.Parallel()
	line := presentation.ServerStatusLine(7860, 2, "3h 24m")

	assert.Contains(t, line, ":7860")
	assert.Contains(t, line, "2")
	assert.Contains(t, line, "3h 24m")
}
