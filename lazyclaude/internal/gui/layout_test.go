package gui_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/stretchr/testify/assert"
)

func TestSideTabs(t *testing.T) {
	t.Parallel()
	tabs := gui.SideTabs()

	assert.Len(t, tabs, 2)
	assert.Equal(t, "Sessions", tabs[0].Label)
	assert.Equal(t, "sessions", tabs[0].Name)
	assert.Equal(t, "Server", tabs[1].Label)
	assert.Equal(t, "server", tabs[1].Name)
}

func TestTabBar_FirstActive(t *testing.T) {
	t.Parallel()
	tabs := gui.SideTabs()
	bar := gui.TabBar(tabs, 0)

	assert.Equal(t, "[Sessions]  Server", bar)
}

func TestTabBar_SecondActive(t *testing.T) {
	t.Parallel()
	tabs := gui.SideTabs()
	bar := gui.TabBar(tabs, 1)

	assert.Equal(t, "Sessions  [Server]", bar)
}

func TestTabBar_SingleTab(t *testing.T) {
	t.Parallel()
	tabs := []gui.SideTab{{Label: "Only", Name: "only"}}
	bar := gui.TabBar(tabs, 0)

	assert.Equal(t, "[Only]", bar)
}
