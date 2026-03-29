package keyhandler_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/gui/keyhandler"
)

func TestPanelManager_FocusNext_Wraps(t *testing.T) {
	pm := keyhandler.NewPanelManager(&keyhandler.SessionsPanel{}, &keyhandler.LogsPanel{})

	if pm.FocusIdx() != 0 || pm.ActivePanel().Name() != "sessions" {
		t.Fatal("initial state wrong")
	}
	pm.FocusNext()
	if pm.FocusIdx() != 1 || pm.ActivePanel().Name() != "logs" {
		t.Fatal("after FocusNext should be logs")
	}
	pm.FocusNext()
	if pm.FocusIdx() != 0 || pm.ActivePanel().Name() != "sessions" {
		t.Fatal("should wrap to sessions")
	}
}

func TestPanelManager_FocusPrev_Wraps(t *testing.T) {
	pm := keyhandler.NewPanelManager(&keyhandler.SessionsPanel{}, &keyhandler.LogsPanel{})

	pm.FocusPrev()
	if pm.FocusIdx() != 1 {
		t.Fatalf("FocusPrev from 0 should wrap to 1, got %d", pm.FocusIdx())
	}
	pm.FocusPrev()
	if pm.FocusIdx() != 0 {
		t.Fatalf("FocusPrev from 1 should be 0, got %d", pm.FocusIdx())
	}
}

func TestPanelManager_PanelCount(t *testing.T) {
	pm := keyhandler.NewPanelManager(&keyhandler.SessionsPanel{}, &keyhandler.LogsPanel{})
	if pm.PanelCount() != 2 {
		t.Fatalf("PanelCount = %d, want 2", pm.PanelCount())
	}
}

func TestPanel_TabSupport(t *testing.T) {
	s := &keyhandler.SessionsPanel{}
	if s.TabCount() != 1 {
		t.Errorf("SessionsPanel TabCount = %d, want 1", s.TabCount())
	}
	labels := s.TabLabels()
	if len(labels) != 1 || labels[0] != "Sessions" {
		t.Errorf("SessionsPanel TabLabels = %v", labels)
	}
	// OptionsBarForTab ignores tabIdx for single-tab panels
	if s.OptionsBarForTab(0) == "" {
		t.Error("OptionsBarForTab(0) should not be empty")
	}
}
