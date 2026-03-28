package server_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockManager_WriteAndRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lm := server.NewLockManager(filepath.Join(dir, "ide"))

	err := lm.Write(7860, "test-token-abc")
	require.NoError(t, err)

	lock, err := lm.Read(7860)
	require.NoError(t, err)
	assert.Equal(t, "test-token-abc", lock.AuthToken)
	assert.Equal(t, "ws", lock.Transport)
	assert.Greater(t, lock.PID, 0)
}

func TestLockManager_Exists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lm := server.NewLockManager(filepath.Join(dir, "ide"))

	assert.False(t, lm.Exists(7860))

	require.NoError(t, lm.Write(7860, "token"))
	assert.True(t, lm.Exists(7860))
}

func TestLockManager_Remove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lm := server.NewLockManager(filepath.Join(dir, "ide"))

	require.NoError(t, lm.Write(7860, "token"))
	assert.True(t, lm.Exists(7860))

	require.NoError(t, lm.Remove(7860))
	assert.False(t, lm.Exists(7860))
}

func TestLockManager_Remove_NotExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lm := server.NewLockManager(filepath.Join(dir, "ide"))

	err := lm.Remove(9999)
	assert.Error(t, err) // file doesn't exist
}

func TestLockManager_Read_NotExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lm := server.NewLockManager(filepath.Join(dir, "ide"))

	_, err := lm.Read(9999)
	assert.Error(t, err)
}

func TestLockManager_CleanStale_RemovesDeadPID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ideDir := filepath.Join(dir, "ide")
	lm := server.NewLockManager(ideDir)

	// Write a lock with a PID that doesn't exist (99999999).
	require.NoError(t, os.MkdirAll(ideDir, 0o700))
	staleData := `{"pid":99999999,"authToken":"stale","transport":"ws"}`
	require.NoError(t, os.WriteFile(filepath.Join(ideDir, "11111.lock"), []byte(staleData), 0o600))

	// Write a lock with our own PID (alive).
	require.NoError(t, lm.Write(22222, "alive-token"))

	removed := lm.CleanStale()
	assert.Equal(t, 1, removed, "should remove 1 stale lock")
	assert.False(t, lm.Exists(11111), "stale lock should be removed")
	assert.True(t, lm.Exists(22222), "alive lock should remain")
}

func TestLockManager_CleanStale_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lm := server.NewLockManager(filepath.Join(dir, "ide"))

	removed := lm.CleanStale()
	assert.Equal(t, 0, removed)
}

func TestLockManager_FilePermissions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ideDir := filepath.Join(dir, "ide")
	lm := server.NewLockManager(ideDir)

	require.NoError(t, lm.Write(7860, "secret-token"))

	// Lock file should be user-only readable (0600)
	path := filepath.Join(ideDir, "7860.lock")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}