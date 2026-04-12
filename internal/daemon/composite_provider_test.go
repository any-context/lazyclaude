package daemon

import (
	"testing"

	"github.com/any-context/lazyclaude/internal/core/model"
)

// stubProvider is a minimal SessionProvider for composite_provider tests.
// Only methods required by the tests are implemented; all others panic.
type stubProvider struct {
	host     string
	sessions []SessionInfo
	connSt   ConnectionState

	// scrollbackCalls / historySizeCalls track capture routing invocations
	// so tests can assert which backend served a given session.
	scrollbackCalls  []string
	historySizeCalls []string
}

func (s *stubProvider) HasSession(id string) bool {
	for _, sess := range s.sessions {
		if sess.ID == id {
			return true
		}
	}
	return false
}

// LocalSessionHost returns the Host field of a session in this provider's
// cache. Matches the production localDaemonProvider semantics: ("", true)
// for a local session, (host, true) for a remote mirror entry, and
// ("", false) when the id is unknown.
func (s *stubProvider) LocalSessionHost(id string) (string, bool) {
	for _, sess := range s.sessions {
		if sess.ID == id {
			return sess.Host, true
		}
	}
	return "", false
}

func (s *stubProvider) Host() string                     { return s.host }
func (s *stubProvider) Sessions() ([]SessionInfo, error) { return s.sessions, nil }

func (s *stubProvider) Create(string) error            { panic("not implemented") }
func (s *stubProvider) Delete(string) error            { panic("not implemented") }
func (s *stubProvider) Rename(string, string) error    { panic("not implemented") }
func (s *stubProvider) PurgeOrphans() (int, error)     { panic("not implemented") }
func (s *stubProvider) CapturePreview(string, int, int) (*PreviewResponse, error) {
	return &PreviewResponse{}, nil
}
func (s *stubProvider) CaptureScrollback(id string, _, _, _ int) (*ScrollbackResponse, error) {
	s.scrollbackCalls = append(s.scrollbackCalls, id)
	return &ScrollbackResponse{Content: s.host + ":" + id}, nil
}
func (s *stubProvider) HistorySize(id string) (int, error) {
	s.historySizeCalls = append(s.historySizeCalls, id)
	return len(s.historySizeCalls), nil
}
func (s *stubProvider) SendChoice(string, int) error       { panic("not implemented") }
func (s *stubProvider) AttachSession(string) error         { panic("not implemented") }
func (s *stubProvider) LaunchLazygit(string) error         { panic("not implemented") }
func (s *stubProvider) CreateWorktree(string, string, string) error {
	panic("not implemented")
}
func (s *stubProvider) ResumeWorktree(string, string, string) error {
	panic("not implemented")
}
func (s *stubProvider) ResumeSession(string, string, string) error {
	panic("not implemented")
}
func (s *stubProvider) ListWorktrees(string) ([]WorktreeInfo, error) { panic("not implemented") }
func (s *stubProvider) CreatePMSession(string) error                 { panic("not implemented") }
func (s *stubProvider) CreateWorkerSession(string, string, string) error {
	panic("not implemented")
}
func (s *stubProvider) ConnectionState() ConnectionState { return s.connSt }

// stubNotifyProvider embeds stubProvider and adds notification draining.
type stubNotifyProvider struct {
	stubProvider
	notifications []*model.ToolNotification
}

func (s *stubNotifyProvider) PendingNotifications() []*model.ToolNotification {
	result := s.notifications
	s.notifications = nil
	return result
}

func TestCompositeProvider_PendingNotifications_RemapsWindow(t *testing.T) {
	local := &stubProvider{connSt: Connected}
	cp := NewCompositeProvider(local, nil)

	remote := &stubNotifyProvider{
		stubProvider: stubProvider{host: "srv", connSt: Connected},
		notifications: []*model.ToolNotification{
			{ToolName: "Edit", Window: "lc-abcd1234"},
			{ToolName: "Write", Window: "lc-efgh5678"},
		},
	}
	cp.AddRemote("srv", remote)

	got := cp.PendingNotifications()
	if len(got) != 2 {
		t.Fatalf("got %d notifications, want 2", len(got))
	}
	if got[0].Window != "rm-abcd1234" {
		t.Errorf("got[0].Window=%q, want rm-abcd1234", got[0].Window)
	}
	if got[1].Window != "rm-efgh5678" {
		t.Errorf("got[1].Window=%q, want rm-efgh5678", got[1].Window)
	}
	// Original notification must not be mutated.
	if got[0].ToolName != "Edit" {
		t.Errorf("got[0].ToolName=%q, want Edit", got[0].ToolName)
	}
}

func TestCompositeProvider_PendingNotifications_SkipsDisconnected(t *testing.T) {
	local := &stubProvider{connSt: Connected}
	cp := NewCompositeProvider(local, nil)

	disconnected := &stubNotifyProvider{
		stubProvider: stubProvider{host: "down", connSt: Disconnected},
		notifications: []*model.ToolNotification{
			{ToolName: "Bash", Window: "lc-aaaa0000"},
		},
	}
	cp.AddRemote("down", disconnected)

	got := cp.PendingNotifications()
	if len(got) != 0 {
		t.Fatalf("got %d notifications from disconnected provider, want 0", len(got))
	}
}

func TestCompositeProvider_PendingNotifications_MultipleRemotes(t *testing.T) {
	local := &stubProvider{connSt: Connected}
	cp := NewCompositeProvider(local, nil)

	r1 := &stubNotifyProvider{
		stubProvider: stubProvider{host: "host1", connSt: Connected},
		notifications: []*model.ToolNotification{
			{ToolName: "Edit", Window: "lc-11111111"},
		},
	}
	r2 := &stubNotifyProvider{
		stubProvider: stubProvider{host: "host2", connSt: Connected},
		notifications: []*model.ToolNotification{
			{ToolName: "Write", Window: "lc-22222222"},
		},
	}
	cp.AddRemote("host1", r1)
	cp.AddRemote("host2", r2)

	got := cp.PendingNotifications()
	if len(got) != 2 {
		t.Fatalf("got %d notifications, want 2", len(got))
	}
	// Both should be remapped.
	for _, n := range got {
		if n.Window[:3] != "rm-" {
			t.Errorf("window %q not remapped", n.Window)
		}
	}
}

func TestCompositeProvider_PendingNotifications_Empty(t *testing.T) {
	local := &stubProvider{connSt: Connected}
	cp := NewCompositeProvider(local, nil)

	got := cp.PendingNotifications()
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestCompositeProvider_PendingNotifications_NonLCPrefix(t *testing.T) {
	local := &stubProvider{connSt: Connected}
	cp := NewCompositeProvider(local, nil)

	remote := &stubNotifyProvider{
		stubProvider: stubProvider{host: "srv", connSt: Connected},
		notifications: []*model.ToolNotification{
			{ToolName: "Bash", Window: "@3"},
		},
	}
	cp.AddRemote("srv", remote)

	got := cp.PendingNotifications()
	if len(got) != 1 {
		t.Fatalf("got %d notifications, want 1", len(got))
	}
	// Non-lc- prefix windows should pass through unchanged.
	if got[0].Window != "@3" {
		t.Errorf("got Window=%q, want @3", got[0].Window)
	}
}

func TestCompositeProvider_PendingNotifications_ClearsBuffer(t *testing.T) {
	local := &stubProvider{connSt: Connected}
	cp := NewCompositeProvider(local, nil)

	remote := &stubNotifyProvider{
		stubProvider: stubProvider{host: "srv", connSt: Connected},
		notifications: []*model.ToolNotification{
			{ToolName: "Edit", Window: "lc-abcd1234"},
		},
	}
	cp.AddRemote("srv", remote)

	got := cp.PendingNotifications()
	if len(got) != 1 {
		t.Fatalf("first call: got %d, want 1", len(got))
	}

	// Second call should return nil (buffer cleared by stubNotifyProvider).
	got = cp.PendingNotifications()
	if got != nil {
		t.Errorf("second call: got %v, want nil", got)
	}
}

// --- Capture routing tests (providerForCapture) ---

// newCaptureRouteFixture wires a CompositeProvider with a local stub that
// knows about both a local session (Host="") and a remote mirror session
// (Host="srv"), plus a remote stubProvider registered for host "srv".
func newCaptureRouteFixture(t *testing.T) (*CompositeProvider, *stubProvider, *stubProvider) {
	t.Helper()
	local := &stubProvider{
		connSt: Connected,
		sessions: []SessionInfo{
			{ID: "local-1", Host: ""},
			{ID: "remote-1", Host: "srv"},
		},
	}
	remote := &stubProvider{
		host:   "srv",
		connSt: Connected,
	}
	cp := NewCompositeProvider(local, nil)
	cp.AddRemote("srv", remote)
	return cp, local, remote
}

func TestCompositeProvider_CaptureScrollback_LocalSession(t *testing.T) {
	cp, local, remote := newCaptureRouteFixture(t)

	resp, err := cp.CaptureScrollback("local-1", 80, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != ":local-1" {
		t.Errorf("content=%q, want :local-1", resp.Content)
	}
	if len(local.scrollbackCalls) != 1 || local.scrollbackCalls[0] != "local-1" {
		t.Errorf("local scrollback calls = %v, want [local-1]", local.scrollbackCalls)
	}
	if len(remote.scrollbackCalls) != 0 {
		t.Errorf("remote scrollback calls = %v, want none", remote.scrollbackCalls)
	}
}

func TestCompositeProvider_CaptureScrollback_RemoteSession(t *testing.T) {
	cp, local, remote := newCaptureRouteFixture(t)

	resp, err := cp.CaptureScrollback("remote-1", 80, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "srv:remote-1" {
		t.Errorf("content=%q, want srv:remote-1", resp.Content)
	}
	if len(remote.scrollbackCalls) != 1 || remote.scrollbackCalls[0] != "remote-1" {
		t.Errorf("remote scrollback calls = %v, want [remote-1]", remote.scrollbackCalls)
	}
	if len(local.scrollbackCalls) != 0 {
		t.Errorf("local scrollback calls = %v, want none (remote mirror must not use local tmux)", local.scrollbackCalls)
	}
}

func TestCompositeProvider_CaptureScrollback_RemoteSessionNoProvider(t *testing.T) {
	// Session is tagged with host="ghost" but no remote provider is
	// registered for that host. Expect a routing error.
	local := &stubProvider{
		connSt: Connected,
		sessions: []SessionInfo{
			{ID: "orphan-1", Host: "ghost"},
		},
	}
	cp := NewCompositeProvider(local, nil)

	_, err := cp.CaptureScrollback("orphan-1", 80, 0, 10)
	if err == nil {
		t.Fatal("expected routing error, got nil")
	}
}

func TestCompositeProvider_CaptureScrollback_UnknownSession_FallsBack(t *testing.T) {
	// The session is not in the local cache, but the remote provider's
	// HasSession returns true. providerForCapture must fall back to
	// providerForSession and resolve the remote provider.
	local := &stubProvider{connSt: Connected}
	remote := &stubProvider{
		host:   "srv",
		connSt: Connected,
		sessions: []SessionInfo{
			{ID: "only-on-remote", Host: "srv"},
		},
	}
	cp := NewCompositeProvider(local, nil)
	cp.AddRemote("srv", remote)

	if _, err := cp.CaptureScrollback("only-on-remote", 80, 0, 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remote.scrollbackCalls) != 1 {
		t.Errorf("remote scrollback calls = %v, want 1", remote.scrollbackCalls)
	}
}

func TestCompositeProvider_HistorySize_LocalSession(t *testing.T) {
	cp, local, remote := newCaptureRouteFixture(t)
	if _, err := cp.HistorySize("local-1"); err != nil {
		t.Fatal(err)
	}
	if len(local.historySizeCalls) != 1 {
		t.Errorf("local history calls = %v, want [local-1]", local.historySizeCalls)
	}
	if len(remote.historySizeCalls) != 0 {
		t.Errorf("remote history calls = %v, want none", remote.historySizeCalls)
	}
}

func TestCompositeProvider_HistorySize_RemoteSession(t *testing.T) {
	cp, local, remote := newCaptureRouteFixture(t)
	if _, err := cp.HistorySize("remote-1"); err != nil {
		t.Fatal(err)
	}
	if len(remote.historySizeCalls) != 1 {
		t.Errorf("remote history calls = %v, want [remote-1]", remote.historySizeCalls)
	}
	if len(local.historySizeCalls) != 0 {
		t.Errorf("local history calls = %v, want none", local.historySizeCalls)
	}
}

func TestCompositeProvider_HistorySize_RemoteSessionNoProvider(t *testing.T) {
	local := &stubProvider{
		connSt: Connected,
		sessions: []SessionInfo{
			{ID: "orphan-1", Host: "ghost"},
		},
	}
	cp := NewCompositeProvider(local, nil)
	if _, err := cp.HistorySize("orphan-1"); err == nil {
		t.Fatal("expected routing error, got nil")
	}
}

// Regression guard: CapturePreview is NOT re-routed by Phase 2 and must
// still go through providerForSession (which returns the local provider
// for remote mirror sessions so that the mirror window's tmux pane is
// used). If this test starts failing, check whether CapturePreview got
// mistakenly pointed at providerForCapture.
func TestCompositeProvider_CapturePreview_RemoteStillUsesLocal(t *testing.T) {
	local := &stubProvider{
		connSt: Connected,
		sessions: []SessionInfo{
			// providerForSession calls local.HasSession first; the mirror
			// session is in the local store with Host="srv", so local must
			// win the lookup.
			{ID: "remote-1", Host: "srv"},
		},
	}
	remote := &stubProvider{
		host:   "srv",
		connSt: Connected,
	}
	cp := NewCompositeProvider(local, nil)
	cp.AddRemote("srv", remote)

	if _, err := cp.CapturePreview("remote-1", 80, 24); err != nil {
		t.Fatal(err)
	}
	// Remote provider must not have been touched for preview routing.
	if len(remote.scrollbackCalls) != 0 || len(remote.historySizeCalls) != 0 {
		t.Errorf("remote provider should not serve preview; got scrollback=%v history=%v", remote.scrollbackCalls, remote.historySizeCalls)
	}
}

func TestRemapRemoteWindow(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"lc-abcd1234", "rm-abcd1234"},
		{"lc-x", "rm-x"},
		{"rm-already", "rm-already"},
		{"@3", "@3"},
		{"", ""},
		{"lc-", "rm-"},
	}
	for _, tt := range tests {
		got := remapRemoteWindow(tt.input)
		if got != tt.want {
			t.Errorf("remapRemoteWindow(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
