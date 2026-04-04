package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DaemonInfo holds the connection details for a running remote daemon.
type DaemonInfo struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Token string `json:"token"`
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
// It runs "lazyclaude daemon --port 0" which prints a JSON line
// containing the assigned port and auth token.
func (lm *LifecycleManager) StartRemoteDaemon(ctx context.Context, host string) (*DaemonInfo, error) {
	output, err := lm.ssh.Run(ctx, host, "lazyclaude daemon --port 0")
	if err != nil {
		return nil, fmt.Errorf("failed to start daemon on %s: %w", host, err)
	}

	info, err := parseDaemonOutput(string(output))
	if err != nil {
		return nil, fmt.Errorf("failed to parse daemon output on %s: %w", host, err)
	}
	info.Host = host
	return info, nil
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
func (lm *LifecycleManager) DiscoverRemoteDaemon(ctx context.Context, host string) (*DaemonInfo, error) {
	output, err := lm.ssh.Run(ctx, host, "cat /tmp/lazyclaude-$(whoami)/daemon.json")
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

// parseDaemonOutput extracts DaemonInfo from the daemon's stdout.
// The daemon prints a JSON line like: {"port":12345,"token":"abc..."}
func parseDaemonOutput(output string) (*DaemonInfo, error) {
	var lastParseErr error
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var info DaemonInfo
		if err := json.Unmarshal([]byte(line), &info); err != nil {
			lastParseErr = err
			continue
		}
		if info.Port > 0 && info.Token != "" {
			return &info, nil
		}
	}
	if lastParseErr != nil {
		return nil, fmt.Errorf("no valid daemon info in output (last parse error: %w)", lastParseErr)
	}
	return nil, fmt.Errorf("no valid daemon info in output: %s", output)
}
