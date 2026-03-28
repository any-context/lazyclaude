package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadClaudeSettings_Empty(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")

	settings, err := config.ReadClaudeSettings(path)
	require.NoError(t, err)
	assert.Empty(t, settings)
}

func TestReadClaudeSettings_Existing(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"foo":"bar"}`), 0o600))

	settings, err := config.ReadClaudeSettings(path)
	require.NoError(t, err)
	assert.Equal(t, "bar", settings["foo"])
}

func TestReadClaudeSettings_Malformed(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{invalid`), 0o600))

	_, err := config.ReadClaudeSettings(path)
	assert.Error(t, err)
}

func TestHasLazyClaudeHooks_Empty(t *testing.T) {
	t.Parallel()
	assert.False(t, config.HasLazyClaudeHooks(map[string]any{}))
}

func TestHasLazyClaudeHooks_Present(t *testing.T) {
	t.Parallel()
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{"type": "command", "command": "curl -s http://127.0.0.1:8080/notify"},
					},
				},
			},
		},
	}
	assert.True(t, config.HasLazyClaudeHooks(settings))
}

func TestHasLazyClaudeHooks_OtherHooks(t *testing.T) {
	t.Parallel()
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{"type": "command", "command": "some-other-tool"},
					},
				},
			},
		},
	}
	assert.False(t, config.HasLazyClaudeHooks(settings))
}

func TestSetLazyClaudeHooks_EmptySettings(t *testing.T) {
	t.Parallel()
	settings := map[string]any{}

	result := config.SetLazyClaudeHooks(settings)

	// Should have hooks.PreToolUse and hooks.Notification
	hooks, ok := result["hooks"].(map[string]any)
	require.True(t, ok, "hooks key should exist")
	assert.Contains(t, hooks, "PreToolUse")
	assert.Contains(t, hooks, "Notification")
}

func TestSetLazyClaudeHooks_PreservesExisting(t *testing.T) {
	t.Parallel()
	settings := map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"Stop": []any{
				map[string]any{"matcher": "*", "hooks": []any{
					map[string]any{"type": "command", "command": "my-custom-hook"},
				}},
			},
		},
	}

	result := config.SetLazyClaudeHooks(settings)

	// theme preserved
	assert.Equal(t, "dark", result["theme"])

	// Stop hook preserved
	hooks := result["hooks"].(map[string]any)
	assert.Contains(t, hooks, "Stop")
	assert.Contains(t, hooks, "PreToolUse")
	assert.Contains(t, hooks, "Notification")
}

func TestWriteClaudeSettings(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")

	settings := map[string]any{"foo": "bar"}
	require.NoError(t, config.WriteClaudeSettings(path, settings))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var read map[string]any
	require.NoError(t, json.Unmarshal(data, &read))
	assert.Equal(t, "bar", read["foo"])
}

// TestHookCommand_ValidatesLockPID verifies that hook commands check PID liveness
// before using a lock file, preventing connection to stale/dead servers.
func TestHookCommand_ValidatesLockPID(t *testing.T) {
	t.Parallel()
	settings := config.SetLazyClaudeHooks(map[string]any{})
	hooks := settings["hooks"].(map[string]any)

	for _, hookType := range []string{"PreToolUse", "Notification"} {
		entries := hooks[hookType].([]any)
		entry := entries[0].(map[string]any)
		hookList := entry["hooks"].([]any)
		hook := hookList[0].(map[string]any)
		cmd := hook["command"].(string)

		// Hook must validate PID of lock file owner before using it.
		// process.kill(pid, 0) is the Node.js way to check PID liveness.
		if !strings.Contains(cmd, "process.kill") && !strings.Contains(cmd, "kill(") {
			t.Errorf("%s hook must validate lock PID liveness (process.kill(pid, 0))", hookType)
		}
	}
}

func TestSetLazyClaudeHooks_Roundtrip(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "settings.json")

	// Start empty, set hooks, write, read back, verify
	settings := map[string]any{}
	result := config.SetLazyClaudeHooks(settings)
	require.NoError(t, config.WriteClaudeSettings(path, result))

	readBack, err := config.ReadClaudeSettings(path)
	require.NoError(t, err)
	assert.True(t, config.HasLazyClaudeHooks(readBack))
}
