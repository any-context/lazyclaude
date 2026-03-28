package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// LockFile represents the contents of an IDE lock file.
type LockFile struct {
	PID       int    `json:"pid"`
	AuthToken string `json:"authToken"`
	Transport string `json:"transport"`
}

// LockManager handles IDE lock file lifecycle.
type LockManager struct {
	ideDir string
}

// NewLockManager creates a lock manager.
// ideDir is typically ~/.claude/ide/
func NewLockManager(ideDir string) *LockManager {
	return &LockManager{ideDir: ideDir}
}

// Write creates a lock file at <ideDir>/<port>.lock.
func (m *LockManager) Write(port int, token string) error {
	if err := os.MkdirAll(m.ideDir, 0o700); err != nil {
		return fmt.Errorf("create ide dir: %w", err)
	}

	lock := LockFile{
		PID:       os.Getpid(),
		AuthToken: token,
		Transport: "ws",
	}
	data, err := json.Marshal(lock)
	if err != nil {
		return fmt.Errorf("marshal lock: %w", err)
	}

	path := m.lockPath(port)
	return os.WriteFile(path, data, 0o600)
}

// Read reads a lock file.
func (m *LockManager) Read(port int) (*LockFile, error) {
	data, err := os.ReadFile(m.lockPath(port))
	if err != nil {
		return nil, err
	}
	var lock LockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse lock: %w", err)
	}
	return &lock, nil
}

// Remove deletes a lock file.
func (m *LockManager) Remove(port int) error {
	return os.Remove(m.lockPath(port))
}

// Exists checks if a lock file exists for a port.
func (m *LockManager) Exists(port int) bool {
	_, err := os.Stat(m.lockPath(port))
	return err == nil
}

// CleanStale removes lock files whose PID is no longer alive.
// Returns the number of removed files.
func (m *LockManager) CleanStale() int {
	entries, err := os.ReadDir(m.ideDir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".lock" {
			continue
		}
		path := filepath.Join(m.ideDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var lock LockFile
		if err := json.Unmarshal(data, &lock); err != nil {
			continue
		}
		if lock.PID <= 0 {
			os.Remove(path)
			removed++
			continue
		}
		proc, err := os.FindProcess(lock.PID)
		if err != nil {
			os.Remove(path)
			removed++
			continue
		}
		// Signal 0 checks if process exists without sending a signal.
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			os.Remove(path)
			removed++
		}
	}
	return removed
}

func (m *LockManager) lockPath(port int) string {
	return filepath.Join(m.ideDir, strconv.Itoa(port)+".lock")
}