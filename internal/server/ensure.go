package server

import (
	"encoding/json"
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

// RestartServer kills any existing server and starts a new one.
func RestartServer(opts EnsureOpts) (EnsureResult, error) {
	// Kill existing server by reading port file and finding the process.
	if data, err := os.ReadFile(opts.PortFile); err == nil {
		port, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil && port > 0 {
			killServerOnPort(port)
		}
		os.Remove(opts.PortFile)
	}
	return startServer(opts)
}

// StopDaemon reads the port file, kills the running daemon, and removes
// the port file so a new server can bind. It is a no-op if no server is running.
func StopDaemon(portFile string) {
	data, err := os.ReadFile(portFile)
	if err != nil {
		return
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || port <= 0 {
		return
	}
	killServerOnPort(port)
	os.Remove(portFile)
}

// killServerOnPort finds and kills the server process listening on the given port.
func killServerOnPort(port int) {
	// Find PID from lock file
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	lockPath := fmt.Sprintf("%s/.claude/ide/%d.lock", home, port)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return
	}
	var lock struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(data, &lock) == nil && lock.PID > 0 {
		if p, err := os.FindProcess(lock.PID); err == nil {
			p.Signal(os.Interrupt)
		}
	}
	os.Remove(lockPath)
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

	return startServer(opts)
}

func startServer(opts EnsureOpts) (EnsureResult, error) {
	cmd := exec.Command(opts.Binary, "server", "--port", "0")
	if len(opts.ExtraEnv) > 0 {
		cmd.Env = append(os.Environ(), opts.ExtraEnv...)
	}
	cmd.Stdout = nil
	// Log server output to file so it persists after detach.
	logFile, err := os.OpenFile("/tmp/lazyclaude/server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return EnsureResult{}, fmt.Errorf("start MCP server: %w", err)
	}
	// Close parent's copy — child inherited the fd via Start().
	if logFile != nil {
		logFile.Close()
	}
	cmd.Process.Release() // detach

	return EnsureResult{Started: true}, nil
}

// IsAlive checks if a server is running by reading the port file and dialing.
func IsAlive(portFile string) bool {
	data, err := os.ReadFile(portFile)
	if err != nil {
		return false
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || port <= 0 {
		return false
	}
	return isServerAlive(port)
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
