package choice_test

import (
	"os"
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/gui/choice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPaths(t *testing.T) config.Paths {
	t.Helper()
	tmp := t.TempDir()
	p := config.TestPaths(tmp)
	os.MkdirAll(p.RuntimeDir, 0o755)
	return p
}

func TestWriteAndReadFile(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	err := choice.WriteFile(paths, "test-wr", choice.Accept)
	require.NoError(t, err)

	c, err := choice.ReadFile(paths, "test-wr")
	require.NoError(t, err)
	assert.Equal(t, choice.Accept, c)

	_, err = os.Stat(paths.ChoiceFile("test-wr"))
	assert.True(t, os.IsNotExist(err))
}

func TestWriteFile_AllChoices(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		c    choice.Choice
		want int
	}{
		{"accept", choice.Accept, 1},
		{"allow", choice.Allow, 2},
		{"reject", choice.Reject, 3},
		{"cancel", choice.Cancel, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			paths := testPaths(t)

			err := choice.WriteFile(paths, "test-"+tt.name, tt.c)
			require.NoError(t, err)

			c, err := choice.ReadFile(paths, "test-"+tt.name)
			require.NoError(t, err)
			assert.Equal(t, choice.Choice(tt.want), c)
		})
	}
}

func TestReadFile_NotExists(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)
	_, err := choice.ReadFile(paths, "nonexistent")
	assert.Error(t, err)
}

func TestWriteFile_Permissions(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	err := choice.WriteFile(paths, "test-perms", choice.Accept)
	require.NoError(t, err)

	info, err := os.Stat(paths.ChoiceFile("test-perms"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
