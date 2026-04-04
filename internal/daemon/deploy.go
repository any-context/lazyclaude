package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// SSHExecutor abstracts SSH and SCP command execution for testability.
type SSHExecutor interface {
	// Run executes a command on the remote host and returns its output.
	Run(ctx context.Context, host, command string) ([]byte, error)
	// Copy transfers a local file to the remote host at the given path.
	Copy(ctx context.Context, host, localPath, remotePath string) error
}

// DeployConfig configures a binary deployment to a remote host.
type DeployConfig struct {
	Host       string // user@host or user@host:port
	BinaryPath string // pre-built binary path (empty = auto-build)
	RemoteDir  string // remote install directory (default: ~/.local/bin)
}

// DeployResult contains the outcome of a successful deployment.
type DeployResult struct {
	Arch        string // detected remote architecture (e.g. "amd64")
	RemotePath  string // full remote path where binary was installed
	Version     string // version string from remote binary verification
	PathWarning string // non-empty if RemoteDir is not in remote PATH
}

// Deploy deploys the lazyclaude binary to a remote host.
//
// Steps:
//  1. Detect remote architecture via "uname -m"
//  2. Build binary for the target (or use pre-built --bin)
//  3. Check that tmux is installed on remote
//  4. Ensure remote directory exists and is writable
//  5. Transfer binary via scp
//  6. Verify deployed binary runs
//  7. Check if remote PATH includes the install directory
func Deploy(ctx context.Context, cfg DeployConfig, ssh SSHExecutor) (*DeployResult, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if cfg.RemoteDir == "" {
		cfg.RemoteDir = "~/.local/bin"
	}

	// 1. Detect remote architecture.
	archRaw, err := ssh.Run(ctx, cfg.Host, "uname -m")
	if err != nil {
		return nil, fmt.Errorf("failed to detect architecture on %s: %w", cfg.Host, err)
	}
	goarch, err := mapArch(strings.TrimSpace(string(archRaw)))
	if err != nil {
		return nil, fmt.Errorf("failed to detect architecture on %s: %w", cfg.Host, err)
	}

	// 2. Determine binary to deploy.
	binPath := cfg.BinaryPath
	if binPath == "" {
		// Auto-build if local arch differs or we're not on linux.
		if runtime.GOOS != "linux" || runtime.GOARCH != goarch {
			built, buildErr := crossBuild(ctx, goarch)
			if buildErr != nil {
				return nil, buildErr
			}
			binPath = built
			defer os.Remove(binPath)
		} else {
			// Same arch: use the running binary.
			binPath, err = os.Executable()
			if err != nil {
				return nil, fmt.Errorf("cannot find current executable: %w", err)
			}
		}
	}

	// Validate binary exists before proceeding.
	if _, err := os.Stat(binPath); err != nil {
		return nil, fmt.Errorf("binary not found %s: %w", binPath, err)
	}

	// 3. Check tmux on remote.
	if _, err := ssh.Run(ctx, cfg.Host, "which tmux"); err != nil {
		return nil, fmt.Errorf("tmux is not installed on %s", cfg.Host)
	}

	// 4. Ensure remote directory exists.
	quotedDir := posixQuote(cfg.RemoteDir)
	mkdirCmd := fmt.Sprintf("mkdir -p %s && test -w %s", quotedDir, quotedDir)
	if _, err := ssh.Run(ctx, cfg.Host, mkdirCmd); err != nil {
		return nil, fmt.Errorf("cannot write to %s on %s: %w", cfg.RemoteDir, cfg.Host, err)
	}

	// 5. Transfer binary.
	remotePath := path.Join(cfg.RemoteDir, "lazyclaude")
	if err := ssh.Copy(ctx, cfg.Host, binPath, remotePath); err != nil {
		return nil, fmt.Errorf("failed to transfer binary to %s: %w", cfg.Host, err)
	}

	// Make executable.
	quotedPath := posixQuote(remotePath)
	if _, err := ssh.Run(ctx, cfg.Host, fmt.Sprintf("chmod +x %s", quotedPath)); err != nil {
		return nil, fmt.Errorf("failed to set executable permission on %s: %w", cfg.Host, err)
	}

	// 6. Verify deployed binary.
	verifyOut, err := ssh.Run(ctx, cfg.Host, quotedPath+" --version")
	if err != nil {
		return nil, fmt.Errorf("deployed binary failed to start on %s: %w", cfg.Host, err)
	}
	version := strings.TrimSpace(string(verifyOut))

	// 7. Check PATH.
	var pathWarning string
	pathOut, err := ssh.Run(ctx, cfg.Host, "echo $PATH")
	if err == nil {
		pathStr := strings.TrimSpace(string(pathOut))
		expandedDir := cfg.RemoteDir
		if strings.HasPrefix(expandedDir, "~/") {
			if homeOut, homeErr := ssh.Run(ctx, cfg.Host, "echo $HOME"); homeErr == nil {
				expandedDir = strings.TrimSpace(string(homeOut)) + expandedDir[1:]
			}
		}
		if !pathContains(pathStr, expandedDir) {
			pathWarning = fmt.Sprintf("%s is not in PATH on %s; add to your shell profile: export PATH=\"%s:$PATH\"",
				cfg.RemoteDir, cfg.Host, cfg.RemoteDir)
		}
	}

	return &DeployResult{
		Arch:        goarch,
		RemotePath:  remotePath,
		Version:     version,
		PathWarning: pathWarning,
	}, nil
}

// mapArch converts uname -m output to GOARCH values.
func mapArch(uname string) (string, error) {
	switch uname {
	case "x86_64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	case "armv7l":
		return "arm", nil
	case "i686", "i386":
		return "386", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", uname)
	}
}

// crossBuild compiles the lazyclaude binary for linux/goarch.
func crossBuild(ctx context.Context, goarch string) (string, error) {
	outPath := filepath.Join(os.TempDir(), fmt.Sprintf("lazyclaude-deploy-%s", goarch))
	cmd := exec.CommandContext(ctx, "go", "build", "-o", outPath, "./cmd/lazyclaude")
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to build binary for linux/%s: %w", goarch, err)
	}
	return outPath, nil
}

// pathContains checks whether dir appears in a colon-separated PATH string.
func pathContains(pathStr, dir string) bool {
	for _, p := range strings.Split(pathStr, ":") {
		if p == dir {
			return true
		}
	}
	return false
}

// posixQuote wraps a string in single quotes, escaping embedded single quotes.
// This prevents shell interpretation of metacharacters in remote commands.
func posixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// splitHostPort separates "user@host:port" into ("user@host", "port").
// If no port is specified, returns (host, "").
// Handles: "host", "host:22", "user@host", "user@host:22",
// "[::1]", "[::1]:22".
func splitHostPort(hostSpec string) (string, string) {
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

// ExecSSHExecutor implements SSHExecutor using real ssh/scp commands.
type ExecSSHExecutor struct{}

func (e *ExecSSHExecutor) Run(ctx context.Context, host, command string) ([]byte, error) {
	sshHost, port := splitHostPort(host)
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
	if port != "" {
		args = append(args, "-p", port)
	}
	args = append(args, sshHost, command)
	return exec.CommandContext(ctx, "ssh", args...).Output()
}

func (e *ExecSSHExecutor) Copy(ctx context.Context, host, localPath, remotePath string) error {
	sshHost, port := splitHostPort(host)
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
	if port != "" {
		args = append(args, "-P", port)
	}
	args = append(args, localPath, sshHost+":"+remotePath)
	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
