package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/stretchr/testify/assert"
)

func TestDefaultPaths_UsesHomeDir(t *testing.T) {
	t.Parallel()
	p := config.DefaultPaths()

	home, _ := os.UserHomeDir()
	assert.Contains(t, p.IDEDir, filepath.Join(home, ".claude", "ide"))
	assert.Contains(t, p.DataDir, "lazyclaude")
}

func TestTestPaths_FullyIsolated(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	p := config.TestPaths(tmp)

	// All paths must be under the tmp directory
	assert.True(t, isUnder(p.IDEDir, tmp), "IDEDir should be under tmp")
	assert.True(t, isUnder(p.DataDir, tmp), "DataDir should be under tmp")
	assert.True(t, isUnder(p.RuntimeDir, tmp), "RuntimeDir should be under tmp")
}

func TestTestPaths_NoOverlapWithDefault(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	prod := config.DefaultPaths()
	test := config.TestPaths(tmp)

	// None of the test paths should equal production paths
	assert.NotEqual(t, prod.IDEDir, test.IDEDir)
	assert.NotEqual(t, prod.DataDir, test.DataDir)
	assert.NotEqual(t, prod.RuntimeDir, test.RuntimeDir)
}

func TestPaths_StateFile(t *testing.T) {
	t.Parallel()
	p := config.TestPaths("/tmp/test")
	assert.Equal(t, "/tmp/test/data/state.json", p.StateFile())
}

func TestPaths_PortFile(t *testing.T) {
	t.Parallel()
	p := config.TestPaths("/tmp/test")
	assert.Equal(t, "/tmp/test/run/lazyclaude-mcp.port", p.PortFile())
}

func TestPaths_ChoiceFile(t *testing.T) {
	t.Parallel()
	p := config.TestPaths("/tmp/test")
	assert.Equal(t, "/tmp/test/run/lazyclaude-choice-lc-abc.txt", p.ChoiceFile("lc-abc"))
}

func TestPaths_LockFile(t *testing.T) {
	t.Parallel()
	p := config.TestPaths("/tmp/test")
	assert.Equal(t, "/tmp/test/ide/7860.lock", p.LockFile(7860))
}

func TestDefaultPaths_EnvOverride_DataDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LAZYCLAUDE_DATA_DIR", filepath.Join(tmp, "custom-data"))

	p := config.DefaultPaths()
	assert.Equal(t, filepath.Join(tmp, "custom-data"), p.DataDir)
}

func TestDefaultPaths_EnvOverride_RuntimeDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LAZYCLAUDE_RUNTIME_DIR", filepath.Join(tmp, "custom-runtime"))

	p := config.DefaultPaths()
	assert.Equal(t, filepath.Join(tmp, "custom-runtime"), p.RuntimeDir)
}

func TestParsePopupMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected config.PopupMode
	}{
		{"auto", config.PopupModeAuto},
		{"tmux", config.PopupModeTmux},
		{"overlay", config.PopupModeOverlay},
		{"AUTO", config.PopupModeAuto},
		{"", config.PopupModeAuto},
		{"invalid", config.PopupModeAuto},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, config.ParsePopupMode(tt.input))
		})
	}
}

func TestDefaultPaths_EnvOverride_IDEDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LAZYCLAUDE_IDE_DIR", filepath.Join(tmp, "custom-ide"))

	p := config.DefaultPaths()
	assert.Equal(t, filepath.Join(tmp, "custom-ide"), p.IDEDir)
}

func isUnder(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}
