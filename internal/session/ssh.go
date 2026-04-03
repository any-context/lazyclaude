package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// remoteScriptOpts configures additional arguments for the remote script.
// Used by worktree/PM sessions to inject --append-system-prompt safely.
type remoteScriptOpts struct {
	SystemPrompt string // passed via --append-system-prompt (heredoc-safe)
	UserPrompt   string // passed as positional argument (heredoc-safe)
}

// writeRemoteScript writes a plain bash script to a temp file.
// The script sets up the MCP lock file and starts Claude Code.
// No shell.Quote, no nested escaping — just a regular bash script with heredoc.
func writeRemoteScript(sess Session, mcpPort int, token string, opts *remoteScriptOpts) (string, error) {
	lockDir := "$HOME/.claude/ide"
	lockFile := fmt.Sprintf("%s/%d.lock", lockDir, mcpPort)

	lockContent := struct {
		PID       int    `json:"pid"`
		AuthToken string `json:"authToken"`
		Transport string `json:"transport"`
	}{PID: 0, AuthToken: token, Transport: "ws"}
	lockJSON, err := json.Marshal(lockContent)
	if err != nil {
		return "", fmt.Errorf("marshal lock content: %w", err)
	}

	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString(fmt.Sprintf("mkdir -p \"%s\"\n", lockDir))
	b.WriteString(fmt.Sprintf("cat > \"%s\" << 'LOCKEOF'\n", lockFile))
	b.WriteString(string(lockJSON) + "\n")
	b.WriteString("LOCKEOF\n")
	b.WriteString(fmt.Sprintf("trap 'rm -f \"%s\"' EXIT\n", lockFile))

	if sess.Path != "" && sess.Path != "." {
		// Use POSIX single-quote escaping (not Go %q) for shell safety.
		safePath := "'" + strings.ReplaceAll(sess.Path, "'", "'\\''") + "'"
		b.WriteString(fmt.Sprintf("cd %s\n", safePath))
	}

	claudeArgs := "claude"
	for _, f := range sess.Flags {
		claudeArgs += " " + f
	}

	// If systemPrompt/userPrompt are provided (worktree/PM sessions),
	// write them via heredoc to avoid quoting issues. The heredoc delimiter
	// uses single-quoted labels so no expansion occurs inside.
	hasPromptVars := opts != nil && opts.SystemPrompt != ""
	if hasPromptVars {
		b.WriteString("read -r -d '' _LC_SYSPROMPT << 'PROMPTEOF'\n")
		b.WriteString(opts.SystemPrompt + "\n")
		b.WriteString("PROMPTEOF\n")

		if strings.TrimSpace(opts.UserPrompt) != "" {
			b.WriteString("read -r -d '' _LC_USERPROMPT << 'UPROMPTEOF'\n")
			b.WriteString(opts.UserPrompt + "\n")
			b.WriteString("UPROMPTEOF\n")
		}
	}

	// Pass through auth tokens so Claude Code can authenticate on remote.
	// Only CLAUDE_CODE_AUTO_CONNECT_IDE is mandatory; auth vars are optional
	// (present when user has set them in the local environment).
	var envPrefix strings.Builder
	envPrefix.WriteString("CLAUDE_CODE_AUTO_CONNECT_IDE=true")
	for _, key := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_CODE_API_KEY"} {
		if val := os.Getenv(key); val != "" {
			// Use %q to prevent shell metacharacters in token values from
			// being interpreted. Go %q produces valid bash double-quoted strings
			// for the ASCII-safe base64 tokens used here.
			envPrefix.WriteString(fmt.Sprintf(" %s=%q", key, val))
		}
	}

	// exec $SHELL -lc runs in remote's login shell (loads .zprofile/.profile for PATH)
	// $SHELL is expanded on remote since this script is base64-encoded and eval'd there.
	//
	// When prompt variables are set (worktree/PM sessions), we use double quotes
	// for the -lic argument so $-variables expand. The claude binary name and
	// fixed flags are safe in double-quote context. When no prompt variables
	// are needed, the original single-quote form is used.
	if hasPromptVars {
		promptArgs := " --append-system-prompt \"$_LC_SYSPROMPT\""
		if opts != nil && strings.TrimSpace(opts.UserPrompt) != "" {
			promptArgs += " \"$_LC_USERPROMPT\""
		}
		b.WriteString(fmt.Sprintf("%s exec \"$SHELL\" -lic \"exec %s%s\"\n", envPrefix.String(), claudeArgs, promptArgs))
	} else {
		b.WriteString(fmt.Sprintf("%s exec \"$SHELL\" -lic 'exec %s'\n", envPrefix.String(), claudeArgs))
	}

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
func buildSSHCommand(sess Session, mcpPort int, token string, opts *remoteScriptOpts) (string, error) {
	scriptPath, err := writeRemoteScript(sess, mcpPort, token, opts)
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

// RunSSHCommand executes a command on a remote host via SSH and returns its output.
// Uses the same base64-encoding pattern as buildSSHCommand to avoid quoting issues.
func RunSSHCommand(ctx context.Context, host, command string) ([]byte, error) {
	encoded := base64.StdEncoding.EncodeToString([]byte(command))
	sshHost, port := splitHostPort(host)

	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, sshHost, fmt.Sprintf("eval \"$(echo %s | base64 -d)\"", encoded))

	cmd := exec.CommandContext(ctx, "ssh", args...)
	return cmd.Output()
}

// BuildLazygitSSHArgs returns the exec.Command arguments for launching lazygit
// on a remote host via SSH. The path is single-quoted and base64-encoded to
// prevent shell injection. Returns ("ssh", args).
func BuildLazygitSSHArgs(host, path string) (string, []string) {
	sshHost, port := splitHostPort(host)
	args := []string{"-t"}
	if port != "" {
		args = append(args, "-p", port)
	}
	safePath := "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
	remoteCmd := fmt.Sprintf("cd %s && lazygit", safePath)
	encoded := base64.StdEncoding.EncodeToString([]byte(remoteCmd))
	args = append(args, sshHost, fmt.Sprintf("eval \"$(echo %s | base64 -d)\"", encoded))
	return "ssh", args
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
