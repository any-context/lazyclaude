package integration_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	binaryPath string
	buildOnce  sync.Once
	buildErr   error
)

func ensureBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		root := filepath.Join("..", "..")
		binaryPath = filepath.Join(root, "bin", "lazyclaude-test")
		os.MkdirAll(filepath.Dir(binaryPath), 0o755)
		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/lazyclaude")
		cmd.Dir = root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		buildErr = cmd.Run()
	})
	if buildErr != nil {
		t.Fatalf("build: %v", buildErr)
	}
	abs, err := filepath.Abs(binaryPath)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatalf("testdata: %v", err)
	}
	return abs
}

// tmuxHelper manages an isolated tmux server for testing.
type tmuxHelper struct {
	socket string
	t      *testing.T
}

func newTmuxHelper(t *testing.T) *tmuxHelper {
	t.Helper()

	// Check tmux is available
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	socket := fmt.Sprintf("lc-test-%d", time.Now().UnixNano())
	h := &tmuxHelper{socket: socket, t: t}

	t.Cleanup(func() {
		exec.Command("tmux", "-L", socket, "kill-server").Run()
	})

	return h
}

func (h *tmuxHelper) run(args ...string) (string, error) {
	fullArgs := append([]string{"-L", h.socket}, args...)
	cmd := exec.Command("tmux", fullArgs...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (h *tmuxHelper) startSession(name string, width, height int) {
	h.t.Helper()
	_, err := h.run("-f", "/dev/null", "new-session", "-d",
		"-s", name,
		"-x", fmt.Sprintf("%d", width),
		"-y", fmt.Sprintf("%d", height))
	if err != nil {
		h.t.Fatalf("start session %s: %v", name, err)
	}
}

func (h *tmuxHelper) sendKeys(target string, keys ...string) {
	h.t.Helper()
	args := append([]string{"send-keys", "-t", target}, keys...)
	if _, err := h.run(args...); err != nil {
		h.t.Fatalf("send-keys %s: %v", target, err)
	}
}

func (h *tmuxHelper) capturePane(target string) string {
	h.t.Helper()
	out, err := h.run("capture-pane", "-p", "-t", target)
	if err != nil {
		h.t.Fatalf("capture-pane %s: %v", target, err)
	}
	return out
}

// waitForText polls capture-pane until text appears or timeout.
func (h *tmuxHelper) waitForText(target, text string, timeout time.Duration) bool {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		content := h.capturePane(target)
		if strings.Contains(content, text) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
