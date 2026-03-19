package server

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const dialTimeout = 500 * time.Millisecond // loopback-only, should be instant

// EnsureOpts configures how the MCP server is ensured.
type EnsureOpts struct {
	Binary   string   // path to lazyclaude binary
	PortFile string   // path to port file
	ExtraEnv []string // additional environment variables (appended to parent env)
}

// EnsureResult reports what EnsureServer did.
type EnsureResult struct {
	Port    int  // listening port (0 if unknown)
	Started bool // true if a new server process was spawned
}

// EnsureServer starts the MCP server if not already running.
// It checks the port file and verifies the server is alive via TCP.
// If the server is dead (stale port file), it removes the file and restarts.
func EnsureServer(opts EnsureOpts) (EnsureResult, error) {
	// 1. Check port file
	if data, err := os.ReadFile(opts.PortFile); err == nil {
		port, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil && port > 0 {
			if isServerAlive(port) {
				return EnsureResult{Port: port, Started: false}, nil
			}
			// Stale port file — remove it
			if rmErr := os.Remove(opts.PortFile); rmErr != nil {
				return EnsureResult{}, fmt.Errorf("remove stale port file: %w", rmErr)
			}
		} else {
			// Invalid content — remove it
			if rmErr := os.Remove(opts.PortFile); rmErr != nil {
				return EnsureResult{}, fmt.Errorf("remove invalid port file: %w", rmErr)
			}
		}
	}

	// 2. Start new server (inherits parent environment + extra vars)
	cmd := exec.Command(opts.Binary, "server", "--port", "0")
	if len(opts.ExtraEnv) > 0 {
		cmd.Env = append(os.Environ(), opts.ExtraEnv...)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return EnsureResult{}, fmt.Errorf("start MCP server: %w", err)
	}
	cmd.Process.Release() // detach

	return EnsureResult{Started: true}, nil
}

// isServerAlive checks if a TCP server is listening on the given port.
func isServerAlive(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), dialTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
