package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- mock SSH executor ---

// mockSSHCall captures a single Run invocation. Copy is not needed by
// the Manager today.
type mockSSHCall struct {
	host    string
	command string
}

// mockSSHExecutor is a scripted SSHExecutor for unit tests.
//
// Each Run call consumes one entry from responses (indexed by call
// order). If responses is empty the executor returns ("", nil) — the
// sshReadFile code path interprets that as "file does not exist", which
// is the most forgiving default for tests that only care about the
// emitted commands.
type mockSSHExecutor struct {
	mu        sync.Mutex
	calls     []mockSSHCall
	responses []mockSSHResponse // scripted replies; nil = empty output
}

type mockSSHResponse struct {
	out []byte
	err error
}

func (m *mockSSHExecutor) Run(_ context.Context, host, command string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := len(m.calls)
	m.calls = append(m.calls, mockSSHCall{host: host, command: command})
	if idx < len(m.responses) {
		r := m.responses[idx]
		return r.out, r.err
	}
	return nil, nil
}

func (m *mockSSHExecutor) Copy(_ context.Context, _ /*host*/, _ /*localPath*/, _ /*remotePath*/ string) error {
	return nil
}

func (m *mockSSHExecutor) callsSnapshot() []mockSSHCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mockSSHCall, len(m.calls))
	copy(out, m.calls)
	return out
}

// newMockExec builds an executor and primes it with the given responses
// (one per expected Run call, in order).
func newMockExec(responses ...mockSSHResponse) *mockSSHExecutor {
	return &mockSSHExecutor{responses: responses}
}

// --- sshReadFile / sshWriteFile (unit tests) ---

func TestSSHReadFile_FileExistsReturnsContents(t *testing.T) {
	t.Parallel()
	exec := newMockExec(mockSSHResponse{out: []byte(`{"mcpServers":{}}`)})
	mgr := NewManager("/unused", exec)

	got, err := mgr.sshReadFile(context.Background(), "remote-host", `"$HOME/.claude.json"`)
	if err != nil {
		t.Fatalf("sshReadFile error = %v", err)
	}
	if got != `{"mcpServers":{}}` {
		t.Errorf("got %q, want JSON payload", got)
	}

	calls := exec.callsSnapshot()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].host != "remote-host" {
		t.Errorf("host = %q, want remote-host", calls[0].host)
	}
	// Assert the command is wrapper-less and uses the double-quoted
	// $HOME form so the remote shell performs expansion.
	want := `if [ -f "$HOME/.claude.json" ]; then cat "$HOME/.claude.json"; fi`
	if calls[0].command != want {
		t.Errorf("command = %q, want %q", calls[0].command, want)
	}
	if strings.Contains(calls[0].command, "sh -c") {
		t.Error("command must not use an outer sh -c wrapper")
	}
}

func TestSSHReadFile_MissingFileReturnsEmpty(t *testing.T) {
	t.Parallel()
	// File not found: remote `if [ -f ... ]; fi` yields empty output
	// with a zero exit code, so the mock returns nil out + nil err.
	exec := newMockExec(mockSSHResponse{out: nil})
	mgr := NewManager("/unused", exec)

	got, err := mgr.sshReadFile(context.Background(), "remote-host", `'/tmp/proj/.mcp.json'`)
	if err != nil {
		t.Fatalf("sshReadFile error = %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty for missing file", got)
	}
}

func TestSSHReadFile_SSHError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("ssh: connect: Connection refused")
	exec := newMockExec(mockSSHResponse{err: wantErr})
	mgr := NewManager("/unused", exec)

	_, err := mgr.sshReadFile(context.Background(), "dead-host", `"$HOME/.claude.json"`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err chain missing original: %v", err)
	}
}

func TestSSHWriteFile_Success(t *testing.T) {
	t.Parallel()
	exec := newMockExec(mockSSHResponse{}) // one Run call, empty output
	mgr := NewManager("/unused", exec)

	payload := "{\n  \"deniedMcpServers\": [\n    { \"serverName\": \"memory\" }\n  ]\n}\n"
	remotePath := `'/tmp/proj/.claude/settings.local.json'`
	if err := mgr.sshWriteFile(context.Background(), "remote-host", remotePath, payload); err != nil {
		t.Fatalf("sshWriteFile error = %v", err)
	}

	calls := exec.callsSnapshot()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	cmd := calls[0].command
	if strings.Contains(cmd, "sh -c") {
		t.Error("command must not use an outer sh -c wrapper")
	}
	// Must include mkdir -p $(dirname ...)
	if !strings.Contains(cmd, `mkdir -p "$(dirname `+remotePath+`)"`) {
		t.Errorf("command missing mkdir/dirname segment: %s", cmd)
	}
	// Must reference base64 -d and the final remote path.
	if !strings.Contains(cmd, "| base64 -d > "+remotePath) {
		t.Errorf("command missing base64 decode segment: %s", cmd)
	}
	// The encoded payload must be embeddable and decode back to the
	// original bytes. Extract the single-quoted literal and decode it.
	encoded := extractQuotedPayload(t, cmd)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(decoded) != payload {
		t.Errorf("decoded payload mismatch:\n got: %q\nwant: %q", decoded, payload)
	}
}

func TestSSHWriteFile_SSHError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("ssh: connect timeout")
	exec := newMockExec(mockSSHResponse{err: wantErr})
	mgr := NewManager("/unused", exec)

	err := mgr.sshWriteFile(context.Background(), "remote-host", `'/tmp/x'`, "data")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err chain missing original: %v", err)
	}
}

// --- Manager.Refresh / ToggleDenied (SSH branch) ---

func TestManagerRefresh_RemoteReadsAllThreeFiles(t *testing.T) {
	t.Parallel()
	userPayload := `{"mcpServers":{"github":{"command":"npx","args":["-y","gh"]}}}`
	projPayload := `{"mcpServers":{"db":{"command":"node","args":["db.js"]}}}`
	settingsPayload := `{"deniedMcpServers":[{"serverName":"github"}]}`
	exec := newMockExec(
		mockSSHResponse{out: []byte(userPayload)},
		mockSSHResponse{out: []byte(projPayload)},
		mockSSHResponse{out: []byte(settingsPayload)},
	)
	mgr := NewManager("/unused", exec)
	mgr.SetHost("AERO")
	mgr.SetProjectDir("/remote/proj")

	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh error = %v", err)
	}

	calls := exec.callsSnapshot()
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
	}
	for _, c := range calls {
		if c.host != "AERO" {
			t.Errorf("host = %q, want AERO", c.host)
		}
		if strings.Contains(c.command, "sh -c") {
			t.Errorf("command must not use sh -c wrapper: %s", c.command)
		}
	}

	// 1: user-level config — must use double-quoted $HOME form.
	wantUser := `if [ -f "$HOME/.claude.json" ]; then cat "$HOME/.claude.json"; fi`
	if calls[0].command != wantUser {
		t.Errorf("user cmd = %q, want %q", calls[0].command, wantUser)
	}
	// 2: project .mcp.json — single-quoted absolute path.
	wantProj := `if [ -f '/remote/proj/.mcp.json' ]; then cat '/remote/proj/.mcp.json'; fi`
	if calls[1].command != wantProj {
		t.Errorf("project cmd = %q, want %q", calls[1].command, wantProj)
	}
	// 3: settings.local.json — single-quoted.
	wantSettings := `if [ -f '/remote/proj/.claude/settings.local.json' ]; then cat '/remote/proj/.claude/settings.local.json'; fi`
	if calls[2].command != wantSettings {
		t.Errorf("settings cmd = %q, want %q", calls[2].command, wantSettings)
	}

	// Merged view: github (user, denied) + db (project, allowed).
	servers := mgr.Servers()
	if len(servers) != 2 {
		t.Fatalf("got %d servers, want 2", len(servers))
	}
	byName := serverMap(servers)
	if gh := byName["github"]; gh.Scope != "user" || !gh.Denied {
		t.Errorf("github: scope=%q denied=%v", gh.Scope, gh.Denied)
	}
	if db := byName["db"]; db.Scope != "project" || db.Denied {
		t.Errorf("db: scope=%q denied=%v", db.Scope, db.Denied)
	}
}

func TestManagerRefresh_RemoteOptionalFilesMissing(t *testing.T) {
	t.Parallel()
	// Only the user-level file exists; project-level files are absent
	// (empty output + nil err = sshReadFile treats as "not found").
	userPayload := `{"mcpServers":{"only":{"command":"a"}}}`
	exec := newMockExec(
		mockSSHResponse{out: []byte(userPayload)},
		mockSSHResponse{}, // .mcp.json missing
		mockSSHResponse{}, // settings.local.json missing
	)
	mgr := NewManager("/unused", exec)
	mgr.SetHost("remote")
	mgr.SetProjectDir("/proj")

	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh error = %v", err)
	}

	servers := mgr.Servers()
	if len(servers) != 1 || servers[0].Name != "only" || servers[0].Scope != "user" {
		t.Errorf("got %+v, want single user server 'only'", servers)
	}
}

func TestManagerRefresh_RemoteUserConfigSSHFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("ssh: handshake failed")
	exec := newMockExec(mockSSHResponse{err: wantErr})
	mgr := NewManager("/unused", exec)
	mgr.SetHost("broken")

	err := mgr.Refresh(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err chain missing original: %v", err)
	}
}

func TestManagerToggleDenied_RemotePreservesUnrelatedKeys(t *testing.T) {
	t.Parallel()
	// Initial refresh: one user server, no denies. Then toggle.
	// The existing settings.local.json has a "permissions" key that
	// must survive the write.
	initialSettings := `{"permissions":{"allow":["Bash(ls)"]}}`
	userJSON := `{"mcpServers":{"memory":{"command":"npx"}}}`
	exec := newMockExec(
		// Refresh call #1: user, .mcp.json (missing), settings.
		mockSSHResponse{out: []byte(userJSON)},
		mockSSHResponse{}, // .mcp.json missing
		mockSSHResponse{out: []byte(initialSettings)},
		// ToggleDenied: read settings, write settings.
		mockSSHResponse{out: []byte(initialSettings)}, // read before write
		mockSSHResponse{},                             // write
		// Final refresh after toggle.
		mockSSHResponse{out: []byte(userJSON)},
		mockSSHResponse{}, // .mcp.json missing
		mockSSHResponse{out: []byte(initialSettings)},
	)
	mgr := NewManager("/unused", exec)
	mgr.SetHost("remote")
	mgr.SetProjectDir("/proj")

	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatalf("initial Refresh error = %v", err)
	}
	if err := mgr.ToggleDenied(context.Background(), "memory"); err != nil {
		t.Fatalf("ToggleDenied error = %v", err)
	}

	calls := exec.callsSnapshot()
	// 3 (refresh) + 2 (toggle read+write) + 3 (refresh) = 8
	if len(calls) != 8 {
		t.Fatalf("got %d calls, want 8: %+v", len(calls), calls)
	}

	// Locate the write call (index 4: mkdir + base64 -d pattern).
	writeCmd := calls[4].command
	if !strings.Contains(writeCmd, "base64 -d > ") {
		t.Fatalf("expected write command at index 4, got %q", writeCmd)
	}
	encoded := extractQuotedPayload(t, writeCmd)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	// Parse the written JSON and assert it preserves permissions and
	// adds the new deniedMcpServers entry.
	var written map[string]json.RawMessage
	if err := json.Unmarshal(decoded, &written); err != nil {
		t.Fatalf("parse written JSON: %v\npayload: %s", err, decoded)
	}
	if _, ok := written["permissions"]; !ok {
		t.Error("permissions key must be preserved in write payload")
	}
	rawDenied, ok := written["deniedMcpServers"]
	if !ok {
		t.Fatal("deniedMcpServers key missing in write payload")
	}
	var entries []deniedEntry
	if err := json.Unmarshal(rawDenied, &entries); err != nil {
		t.Fatalf("parse deniedMcpServers: %v", err)
	}
	if len(entries) != 1 || entries[0].ServerName != "memory" {
		t.Errorf("deniedMcpServers = %+v, want [{memory}]", entries)
	}
}

// blockingSSHExecutor blocks every Run call on a shared channel so the
// test can inject a concurrent SetHost between the capture point and
// the actual SSH dispatch.
type blockingSSHExecutor struct {
	mu      sync.Mutex
	calls   []mockSSHCall
	release chan struct{}
}

func (b *blockingSSHExecutor) Run(_ context.Context, host, command string) ([]byte, error) {
	b.mu.Lock()
	b.calls = append(b.calls, mockSSHCall{host: host, command: command})
	b.mu.Unlock()
	<-b.release
	return nil, nil
}

func (b *blockingSSHExecutor) Copy(_ context.Context, _, _, _ string) error { return nil }

func (b *blockingSSHExecutor) callsSnapshot() []mockSSHCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]mockSSHCall, len(b.calls))
	copy(out, b.calls)
	return out
}

func TestManagerRefresh_RemoteHostCaptured(t *testing.T) {
	t.Parallel()
	// Regression: an async Refresh must keep targeting the host that
	// was live when Refresh was invoked, even if the user navigates
	// to a different host (or back to local) mid-flight. The SSH
	// helpers must NOT re-read m.host inside the goroutine.
	exec := &blockingSSHExecutor{release: make(chan struct{}, 4)}
	mgr := NewManager("/unused", exec)
	mgr.SetHost("host-A")
	mgr.SetProjectDir("/proj")

	done := make(chan error, 1)
	go func() {
		done <- mgr.Refresh(context.Background())
	}()

	// Let the goroutine reach the blocking Run call, then swap the
	// manager's host out from under it.
	waitForCallCount(t, exec, 1)
	mgr.SetHost("host-B")
	// Release all pending SSH reads so Refresh can finish.
	close(exec.release)

	if err := <-done; err != nil {
		t.Fatalf("Refresh error = %v", err)
	}

	calls := exec.callsSnapshot()
	if len(calls) == 0 {
		t.Fatal("expected at least one SSH call")
	}
	for _, c := range calls {
		if c.host != "host-A" {
			t.Errorf("race: ssh call targeted %q, want host-A (the captured host)", c.host)
		}
	}
}

// waitForCallCount spins briefly until the blocking executor has
// recorded at least `want` calls. Used to sequence the test around the
// blocking Run without sleeps.
func waitForCallCount(t *testing.T, exec *blockingSSHExecutor, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(exec.callsSnapshot()) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout waiting for %d ssh call(s)", want)
}

func TestManagerSetHost_RoundTripToLocal(t *testing.T) {
	t.Parallel()
	// Build a local config on disk. After SetHost("") the manager
	// must return to the local code path and the mock SSHExecutor
	// must NOT be invoked.
	dir := t.TempDir()
	userCfg := dir + "/claude.json"
	writeJSON(t, userCfg, `{"mcpServers":{"local":{"command":"a"}}}`)

	exec := newMockExec() // empty responses; any call is an error
	mgr := NewManager(userCfg, exec)

	// Start remote, then reset.
	mgr.SetHost("remote")
	mgr.SetHost("")

	if err := mgr.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh error = %v", err)
	}

	if calls := exec.callsSnapshot(); len(calls) != 0 {
		t.Errorf("SSH must not be invoked after SetHost(\"\"): %d calls", len(calls))
	}

	servers := mgr.Servers()
	if len(servers) != 1 || servers[0].Name != "local" {
		t.Errorf("got %+v, want local server", servers)
	}
}

// extractQuotedPayload pulls the base64 payload out of an sshWriteFile
// command. The command shape is:
//
//	mkdir -p "$(dirname PATH)" && printf '%s' 'BASE64' | base64 -d > PATH
//
// The payload is the single-quoted literal between `printf '%s' ` and
// ` | base64 -d`. Because the payload is pure base64 (A-Za-z0-9+/=)
// the unquoting is unambiguous.
func extractQuotedPayload(t *testing.T, cmd string) string {
	t.Helper()
	const prefix = "printf '%s' '"
	i := strings.Index(cmd, prefix)
	if i < 0 {
		t.Fatalf("prefix %q not found in command: %s", prefix, cmd)
	}
	start := i + len(prefix)
	end := strings.Index(cmd[start:], "' | base64 -d")
	if end < 0 {
		t.Fatalf("closing delimiter not found in command: %s", cmd)
	}
	return cmd[start : start+end]
}

