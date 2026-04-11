package mcp

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/any-context/lazyclaude/internal/core/shell"
)

// sshReadFile runs an optional-read on the remote host and returns the
// file contents, or "" when the file does not exist.
//
// The command shape is:
//
//	if [ -f <path> ]; then cat <path>; fi
//
// so that a missing file produces empty output (exit 0) rather than a
// hard error. Connection failures and other read errors are surfaced
// via a wrapped error.
//
// IMPORTANT: remotePath must be ALREADY quoted for shell consumption.
// The caller supplies one of two forms:
//
//   - shell.Quote(path) — single-quoted absolute path, e.g.
//     '/tmp/proj/.mcp.json'. Safe against shell meta characters.
//
//   - The literal string `"$HOME/.claude.json"` (double-quoted) so the
//     remote shell performs $HOME expansion at exec time. This is the
//     only reliable way to resolve the remote user's home directory
//     without a round-trip.
//
// No outer `sh -c '...'` wrapper is used: SSH passes the command
// directly to the remote user's shell which parses it once. Adding a
// wrapper would collide with shell.Quote's single-quoting and produce
// syntax errors.
//
// host is passed explicitly (rather than read from m.host) so that an
// async Refresh/ToggleDenied goroutine keeps targeting the host that
// was live at call entry. Reading m.host here would race with
// SetHost(...) from the GUI goroutine and silently redirect the
// operation to a different machine.
func (m *Manager) sshReadFile(ctx context.Context, host, remotePath string) (string, error) {
	cmd := fmt.Sprintf("if [ -f %s ]; then cat %s; fi", remotePath, remotePath)
	out, err := m.ssh.Run(ctx, host, cmd)
	if err != nil {
		return "", fmt.Errorf("ssh read %s: %w", remotePath, err)
	}
	return string(out), nil
}

// sshWriteFile writes content to remotePath via SSH. The content is
// base64-encoded before being embedded in the command so arbitrary bytes
// (including quotes, $, backticks, newlines) need no shell escaping.
// The parent directory is created with mkdir -p.
//
// remotePath must be pre-quoted in the same way as sshReadFile. host
// is also passed explicitly so that an async caller captures the value
// once and the helper cannot be redirected mid-flight by a concurrent
// SetHost.
func (m *Manager) sshWriteFile(ctx context.Context, host, remotePath, content string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	// $(dirname ...) is evaluated on the remote side. The encoded
	// payload is ASCII-safe (A-Za-z0-9+/=) so shell.Quote gives a
	// robust single-quoted literal. The printf format is explicitly
	// single-quoted as '%s' for portability across POSIX printf
	// implementations — an unquoted %s is interpreted literally by
	// some busybox variants.
	cmd := fmt.Sprintf(
		`mkdir -p "$(dirname %s)" && printf '%%s' %s | base64 -d > %s`,
		remotePath,
		shell.Quote(encoded),
		remotePath,
	)
	if _, err := m.ssh.Run(ctx, host, cmd); err != nil {
		return fmt.Errorf("ssh write %s: %w", remotePath, err)
	}
	return nil
}
