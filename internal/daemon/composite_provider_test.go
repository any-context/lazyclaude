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
}

func (s *stubProvider) HasSession(id string) bool {
	for _, sess := range s.sessions {
		if sess.ID == id {
			return true
		}
	}
	return false
}
func (s *stubProvider) Host() string                     { return s.host }
func (s *stubProvider) Sessions() ([]SessionInfo, error) { return s.sessions, nil }

func (s *stubProvider) Create(string) error            { panic("not implemented") }
func (s *stubProvider) Delete(string) error            { panic("not implemented") }
func (s *stubProvider) Rename(string, string) error    { panic("not implemented") }
func (s *stubProvider) PurgeOrphans() (int, error)     { panic("not implemented") }
func (s *stubProvider) CapturePreview(string, int, int) (*PreviewResponse, error) {
	panic("not implemented")
}
func (s *stubProvider) CaptureScrollback(string, int, int, int) (*ScrollbackResponse, error) {
	panic("not implemented")
}
func (s *stubProvider) HistorySize(string) (int, error)    { panic("not implemented") }
func (s *stubProvider) SendChoice(string, int) error       { panic("not implemented") }
func (s *stubProvider) AttachSession(string) error         { panic("not implemented") }
func (s *stubProvider) LaunchLazygit(string) error         { panic("not implemented") }
func (s *stubProvider) CreateWorktree(string, string, string) error {
	panic("not implemented")
}
func (s *stubProvider) ResumeWorktree(string, string, string) error {
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
