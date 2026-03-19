package session

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildSSHCommand constructs an SSH command string for remote Claude sessions.
// It sets up:
//   - SSH reverse tunnel (-R) so the remote Claude Code can reach the local MCP server
//   - A remote setup script that writes the IDE lock file for auto-connect
//   - Claude Code with CLAUDE_CODE_AUTO_CONNECT_IDE=true
//
// The command is designed to be run inside a tmux window via `exec`.
func buildSSHCommand(sess Session, mcpPort int, token string) string {
	host, port := splitHostPort(sess.Host)

	var args []string
	args = append(args, "exec", "ssh")
	args = append(args, "-t") // force PTY allocation
	args = append(args, "-o", "ServerAliveInterval=30")
	args = append(args, "-o", "ServerAliveCountMax=3")

	if port != "" {
		args = append(args, "-p", port)
	}

	// Reverse tunnel: remote:mcpPort → local:mcpPort
	args = append(args, "-R", fmt.Sprintf("%d:127.0.0.1:%d", mcpPort, mcpPort))

	args = append(args, shellQuote(host))

	// Remote command: setup lock file + start claude
	remoteCmd := buildRemoteCommand(sess, mcpPort, token)
	args = append(args, "--", "bash", "-c", shellQuote(remoteCmd))

	return strings.Join(args, " ")
}

// buildRemoteCommand creates the bash script run on the remote host.
// It writes the IDE lock file and starts Claude Code.
func buildRemoteCommand(sess Session, mcpPort int, token string) string {
	var parts []string

	// Create IDE lock file so Claude Code discovers the MCP server.
	// Lock file name encodes the port; content matches server.LockFile format.
	lockDir := "$HOME/.claude/ide"
	lockFile := fmt.Sprintf("%s/%d.lock", lockDir, mcpPort)
	lockContent := struct {
		PID       int    `json:"pid"`
		AuthToken string `json:"authToken"`
		Transport string `json:"transport"`
	}{PID: 0, AuthToken: token, Transport: "ws"}
	lockJSON, _ := json.Marshal(lockContent)

	parts = append(parts, fmt.Sprintf("mkdir -p %s", lockDir))
	parts = append(parts, fmt.Sprintf("printf '%%s' %s > %s", shellQuote(string(lockJSON)), lockFile))

	// Cleanup lock file on exit
	parts = append(parts, fmt.Sprintf("trap 'rm -f %s' EXIT", lockFile))

	// Build claude command with flags
	claudeCmd := "CLAUDE_CODE_AUTO_CONNECT_IDE=true exec claude"
	for _, f := range sess.Flags {
		claudeCmd += " " + shellQuote(f)
	}
	parts = append(parts, claudeCmd)

	return strings.Join(parts, " && ")
}

// splitHostPort separates "user@host:port" into ("user@host", "port").
// If no port is specified, returns (host, "").
// Handles: "host", "host:22", "user@host", "user@host:22", "[::1]".
func splitHostPort(hostSpec string) (string, string) {
	if strings.HasPrefix(hostSpec, "[") {
		return hostSpec, ""
	}

	// Find the last colon after the last @ (if any).
	// This avoids confusing colons in usernames or IPv6-like patterns.
	searchFrom := 0
	if atIdx := strings.LastIndex(hostSpec, "@"); atIdx >= 0 {
		searchFrom = atIdx + 1
	}
	colonIdx := strings.LastIndex(hostSpec[searchFrom:], ":")
	if colonIdx < 0 {
		return hostSpec, ""
	}
	colonIdx += searchFrom // absolute index in hostSpec

	port := hostSpec[colonIdx+1:]
	if port == "" {
		return hostSpec, ""
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return hostSpec, ""
		}
	}
	return hostSpec[:colonIdx], port
}
