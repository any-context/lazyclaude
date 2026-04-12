package daemon

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// SSHExecutor abstracts SSH and SCP command execution for testability.
type SSHExecutor interface {
	// Run executes a command on the remote host and returns its output.
	Run(ctx context.Context, host, command string) ([]byte, error)
	// Copy transfers a local file to the remote host at the given path.
	Copy(ctx context.Context, host, localPath, remotePath string) error
}

// ExecSSHExecutor implements SSHExecutor using real ssh/scp commands.
type ExecSSHExecutor struct {
	// AskpassScript is the path to the wrapper script set as SSH_ASKPASS.
	// SSH executes this script with the prompt as argv[1]. The script
	// invokes "lazyclaude askpass" which communicates via Unix socket.
	// When empty, BatchMode=yes is used instead (no interactive auth).
	AskpassScript string
	// AskpassSock is the Unix socket path for askpass communication.
	AskpassSock string
}

// SSHEnv returns the environment variables for SSH_ASKPASS integration.
// Returns nil when AskpassScript is not configured.
func (e *ExecSSHExecutor) SSHEnv() []string {
	if e.AskpassScript == "" {
		return nil
	}
	env := []string{
		"SSH_ASKPASS=" + e.AskpassScript,
		"SSH_ASKPASS_REQUIRE=prefer",
		"LAZYCLAUDE_ASKPASS_SOCK=" + e.AskpassSock,
	}
	// Only inject DISPLAY if not already set, to avoid overriding
	// legitimate X11 forwarding. Older SSH (<8.4) requires DISPLAY
	// for SSH_ASKPASS to trigger when no TTY is present.
	if os.Getenv("DISPLAY") == "" {
		env = append(env, "DISPLAY=:0")
	}
	return env
}

func (e *ExecSSHExecutor) Run(ctx context.Context, host, command string) ([]byte, error) {
	sshHost, port := SplitHostPort(host)
	args := []string{"-o", "ConnectTimeout=10", "-o", "ControlMaster=no", "-o", "ControlPath=none"}
	if e.AskpassScript == "" {
		// No askpass: fall back to BatchMode for non-interactive use.
		args = append([]string{"-o", "BatchMode=yes"}, args...)
	}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, sshHost, command)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	if env := e.SSHEnv(); env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.Output()
}

func (e *ExecSSHExecutor) Copy(ctx context.Context, host, localPath, remotePath string) error {
	sshHost, port := SplitHostPort(host)
	args := []string{"-o", "ConnectTimeout=10", "-o", "ControlMaster=no", "-o", "ControlPath=none"}
	if e.AskpassScript == "" {
		args = append([]string{"-o", "BatchMode=yes"}, args...)
	}
	if port != "" {
		args = append(args, "-P", port)
	}
	args = append(args, localPath, sshHost+":"+remotePath)
	cmd := exec.CommandContext(ctx, "scp", args...)
	if env := e.SSHEnv(); env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.Run()
}

// SplitHostPort separates "user@host:port" into ("user@host", "port").
// If no port is specified, returns (host, "").
// Handles: "host", "host:22", "user@host", "user@host:22",
// "[::1]", "[::1]:22".
func SplitHostPort(hostSpec string) (string, string) {
	// IPv6 bracket notation: [::1] or [::1]:port
	if strings.HasPrefix(hostSpec, "[") {
		closeBracket := strings.Index(hostSpec, "]")
		if closeBracket < 0 {
			return hostSpec, ""
		}
		after := hostSpec[closeBracket+1:]
		if strings.HasPrefix(after, ":") {
			port := after[1:]
			if port != "" && isNumeric(port) {
				return hostSpec[:closeBracket+1], port
			}
		}
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
	if port == "" || !isNumeric(port) {
		return hostSpec, ""
	}
	return hostSpec[:colonIdx], port
}

// isNumeric returns true if s is non-empty and contains only ASCII digits.
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
