package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DiscoverResult holds the discovered server's port and auth token.
type DiscoverResult struct {
	Port  int
	Token string
}

// DiscoverServer scans IDE lock files to find a running lazyclaude server.
// It reads all *.lock files in ideDir, validates each via TCP dial, and
// returns the one with the highest port number (matching the hooks' behavior).
func DiscoverServer(ideDir string) (*DiscoverResult, error) {
	entries, err := os.ReadDir(ideDir)
	if err != nil {
		return nil, fmt.Errorf("read ide dir %s: %w", ideDir, err)
	}

	mgr := NewLockManager(ideDir)
	var best *DiscoverResult
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".lock" {
			continue
		}
		portStr := strings.TrimSuffix(e.Name(), ".lock")
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 {
			continue
		}

		lock, err := mgr.Read(port)
		if err != nil {
			continue
		}
		// Only consider lazyclaude locks (skip VS Code, JetBrains, etc.).
		if lock.App != "" && lock.App != lockApp {
			continue
		}
		if !isServerAlive(port) {
			continue
		}
		if best == nil || port > best.Port {
			best = &DiscoverResult{Port: port, Token: lock.AuthToken}
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no running lazyclaude server found (checked %s)", ideDir)
	}
	return best, nil
}
