package daemon

import (
	"context"
	"encoding/json"
	"fmt"
)

// DaemonInfo holds the connection details for a running remote daemon.
type DaemonInfo struct {
	Host  string `json:"host,omitempty"`
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid,omitempty"`
}

// LifecycleManager handles starting, stopping, and discovering remote daemons.
type LifecycleManager struct {
	ssh SSHExecutor
}

// NewLifecycleManager creates a LifecycleManager that uses the given SSH executor.
func NewLifecycleManager(ssh SSHExecutor) *LifecycleManager {
	return &LifecycleManager{ssh: ssh}
}

// StartRemoteDaemon starts a lazyclaude daemon on the remote host.
// The daemon runs in the foreground, so we launch it with nohup in the
// background and then read daemon.json which contains the port and token.
func (lm *LifecycleManager) StartRemoteDaemon(ctx context.Context, host string) (*DaemonInfo, error) {
	// Try lazyclaude in PATH first, then common install locations.
	cmd := "LC_BIN=$(command -v lazyclaude || echo $HOME/.local/bin/lazyclaude); " +
		"nohup \"$LC_BIN\" daemon --port 0 > /tmp/lazyclaude-daemon.log 2>&1 & " +
		"for i in $(seq 1 20); do sleep 0.5 && [ -f /tmp/lazyclaude-$(whoami)/daemon.json ] && " +
		"cat /tmp/lazyclaude-$(whoami)/daemon.json && exit 0; done; exit 1"
	debugLog("StartRemoteDaemon: host=%q cmd=%q", host, cmd)
	output, err := lm.ssh.Run(ctx, host, cmd)
	debugLog("StartRemoteDaemon: output=%q err=%v", string(output), err)
	if err != nil {
		return nil, fmt.Errorf("lazyclaude is not installed on %s: %w", host, err)
	}

	var info DaemonInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("failed to parse daemon info on %s: %w", host, err)
	}
	info.Host = host
	return &info, nil
}

// StopRemoteDaemon stops the lazyclaude daemon on the remote host.
func (lm *LifecycleManager) StopRemoteDaemon(ctx context.Context, host string) error {
	_, err := lm.ssh.Run(ctx, host, "lazyclaude daemon stop")
	if err != nil {
		return fmt.Errorf("failed to stop daemon on %s: %w", host, err)
	}
	return nil
}

// DiscoverRemoteDaemon reads the daemon info file on the remote host.
// The daemon writes its connection details to /tmp/lazyclaude-$USER/daemon.json.
// Uses $(whoami) on the remote side so the path matches the daemon's DaemonInfoDir().
func (lm *LifecycleManager) DiscoverRemoteDaemon(ctx context.Context, host string) (*DaemonInfo, error) {
	debugLog("DiscoverRemoteDaemon: host=%q", host)
	output, err := lm.ssh.Run(ctx, host, "cat /tmp/lazyclaude-$(whoami)/daemon.json")
	debugLog("DiscoverRemoteDaemon: output=%q err=%v", string(output), err)
	if err != nil {
		return nil, fmt.Errorf("no daemon found on %s: %w", host, err)
	}

	var info DaemonInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("invalid daemon info on %s: %w", host, err)
	}
	if info.Port == 0 {
		return nil, fmt.Errorf("invalid daemon info on %s: port is 0", host)
	}
	if info.Token == "" {
		return nil, fmt.Errorf("invalid daemon info on %s: empty token", host)
	}
	info.Host = host
	return &info, nil
}

