package session

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// writeRemoteScript writes a plain bash script to a temp file.
// The script sets up the MCP lock file and starts Claude Code.
// No shell.Quote, no nested escaping — just a regular bash script with heredoc.
func writeRemoteScript(sess Session, mcpPort int, token string) (string, error) {
	lockDir := "$HOME/.claude/ide"
	lockFile := fmt.Sprintf("%s/%d.lock", lockDir, mcpPort)

	lockContent := struct {
		PID       int    `json:"pid"`
		AuthToken string `json:"authToken"`
		Transport string `json:"transport"`
	}{PID: 0, AuthToken: token, Transport: "ws"}
	lockJSON, _ := json.Marshal(lockContent)

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString(fmt.Sprintf("mkdir -p \"%s\"\n", lockDir))
	b.WriteString(fmt.Sprintf("cat > \"%s\" << 'LOCKEOF'\n", lockFile))
	b.WriteString(string(lockJSON) + "\n")
	b.WriteString("LOCKEOF\n")
	b.WriteString(fmt.Sprintf("trap 'rm -f \"%s\"' EXIT\n", lockFile))

	if sess.Path != "" && sess.Path != "." {
		b.WriteString(fmt.Sprintf("cd %q\n", sess.Path))
	}

	claudeArgs := "claude"
	for _, f := range sess.Flags {
		claudeArgs += " " + f
	}
	// Pass through auth tokens so Claude Code can authenticate on remote.
	// Only CLAUDE_CODE_AUTO_CONNECT_IDE is mandatory; auth vars are optional
	// (present when user has set them in the local environment).
	var envPrefix strings.Builder
	envPrefix.WriteString("CLAUDE_CODE_AUTO_CONNECT_IDE=true")
	for _, key := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_CODE_API_KEY"} {
		if val := os.Getenv(key); val != "" {
			envPrefix.WriteString(fmt.Sprintf(" %s=%s", key, val))
		}
	}
	// exec $SHELL -lc runs in remote's login shell (loads .zprofile/.profile for PATH)
	// $SHELL is expanded on remote since this script is base64-encoded and eval'd there
	b.WriteString(fmt.Sprintf("%s exec \"$SHELL\" -lic 'exec %s'\n", envPrefix.String(), claudeArgs))

	scriptPath := fmt.Sprintf("/tmp/lazyclaude/ssh-%s.sh", sess.ID[:8])
	if err := os.WriteFile(scriptPath, []byte(b.String()), 0o755); err != nil {
		return "", fmt.Errorf("write remote script: %w", err)
	}
	return scriptPath, nil
}

// buildSSHCommand constructs an SSH command using base64-encoded script.
// The script is encoded to avoid all quoting issues. SSH receives a single
// eval command that decodes and executes the script. stdin remains free
// for interactive Claude Code use.
func buildSSHCommand(sess Session, mcpPort int, token string) (string, error) {
	scriptPath, err := writeRemoteScript(sess, mcpPort, token)
	if err != nil {
		return "", err
	}
	scriptContent, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("read remote script: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(scriptContent)

	host, port := splitHostPort(sess.Host)

	var args []string
	args = append(args, "exec", "ssh", "-t")
	args = append(args, "-o", "ServerAliveInterval=30")
	args = append(args, "-o", "ServerAliveCountMax=3")
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, "-R", fmt.Sprintf("%d:127.0.0.1:%d", mcpPort, mcpPort))
	args = append(args, host)
	// base64 string has no shell metacharacters — safe to pass directly.
	// eval runs the decoded script in the remote shell.
	args = append(args, fmt.Sprintf("eval \"$(echo %s | base64 -d)\"", encoded))

	return strings.Join(args, " "), nil
}

// splitHostPort separates "user@host:port" into ("user@host", "port").
// If no port is specified, returns (host, "").
// Handles: "host", "host:22", "user@host", "user@host:22", "[::1]".
func splitHostPort(hostSpec string) (string, string) {
	if strings.HasPrefix(hostSpec, "[") {
		return hostSpec, ""
	}

	searchFrom := 0
	if atIdx := strings.LastIndex(hostSpec, "@"); atIdx >= 0 {
		searchFrom = atIdx + 1
	}
	colonIdx := strings.LastIndex(hostSpec[searchFrom:], ":")
	if colonIdx < 0 {
		return hostSpec, ""
	}
	colonIdx += searchFrom

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
