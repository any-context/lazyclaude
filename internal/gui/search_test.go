package gui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterTreeNodes_EmptyQuery(t *testing.T) {
	nodes := []TreeNode{
		{Kind: ProjectNode, Project: &ProjectItem{Name: "proj1"}},
		{Kind: SessionNode, Session: &SessionItem{Name: "sess-alpha"}},
	}
	got := filterTreeNodes(nodes, "")
	assert.Equal(t, nodes, got)
}

func TestFilterTreeNodes_MatchProject_IncludesAllSessions(t *testing.T) {
	nodes := []TreeNode{
		{Kind: ProjectNode, ProjectID: "p1", Project: &ProjectItem{ID: "p1", Name: "my-project"}},
		{Kind: SessionNode, ProjectID: "p1", Session: &SessionItem{Name: "sess-alpha"}},
		{Kind: SessionNode, ProjectID: "p1", Session: &SessionItem{Name: "sess-beta"}},
		{Kind: ProjectNode, ProjectID: "p2", Project: &ProjectItem{ID: "p2", Name: "other"}},
	}
	got := filterTreeNodes(nodes, "my-proj")
	// Project name matches -> include project + all its sessions.
	assert.Len(t, got, 3)
	assert.Equal(t, "my-project", got[0].Project.Name)
	assert.Equal(t, "sess-alpha", got[1].Session.Name)
	assert.Equal(t, "sess-beta", got[2].Session.Name)
}

func TestFilterTreeNodes_MatchSession_IncludesParentProject(t *testing.T) {
	nodes := []TreeNode{
		{Kind: ProjectNode, ProjectID: "p1", Project: &ProjectItem{ID: "p1", Name: "proj"}},
		{Kind: SessionNode, ProjectID: "p1", Session: &SessionItem{Name: "sess-alpha"}},
		{Kind: SessionNode, ProjectID: "p1", Session: &SessionItem{Name: "sess-beta"}},
	}
	got := filterTreeNodes(nodes, "alpha")
	// Should include parent project header + matching session.
	assert.Len(t, got, 2)
	assert.Equal(t, ProjectNode, got[0].Kind)
	assert.Equal(t, "proj", got[0].Project.Name)
	assert.Equal(t, SessionNode, got[1].Kind)
	assert.Equal(t, "sess-alpha", got[1].Session.Name)
}

func TestFilterTreeNodes_CaseInsensitive(t *testing.T) {
	nodes := []TreeNode{
		{Kind: SessionNode, Session: &SessionItem{Name: "MySession"}},
	}
	got := filterTreeNodes(nodes, "mysession")
	assert.Len(t, got, 1)
}

func TestFilterTreeNodes_NoMatch(t *testing.T) {
	nodes := []TreeNode{
		{Kind: ProjectNode, ProjectID: "p1", Project: &ProjectItem{ID: "p1", Name: "proj"}},
		{Kind: SessionNode, ProjectID: "p1", Session: &SessionItem{Name: "sess"}},
	}
	got := filterTreeNodes(nodes, "nonexistent")
	assert.Len(t, got, 0)
}

func TestFilterLogLines_EmptyQuery(t *testing.T) {
	lines := []string{"line1", "line2"}
	got := filterLogLines(lines, "")
	assert.Equal(t, lines, got)
}

func TestFilterLogLines_Match(t *testing.T) {
	lines := []string{
		"2024-01-01 INFO server started",
		"2024-01-01 ERROR connection failed",
		"2024-01-01 INFO request handled",
	}
	got := filterLogLines(lines, "error")
	assert.Len(t, got, 1)
	assert.Contains(t, got[0], "ERROR")
}

func TestFilterLogLines_NoMatch(t *testing.T) {
	lines := []string{"hello", "world"}
	got := filterLogLines(lines, "xyz")
	assert.Len(t, got, 0)
}

func TestFilterPluginItems_EmptyQuery(t *testing.T) {
	items := []PluginItem{{ID: "plugin-a"}, {ID: "plugin-b"}}
	got := filterPluginItems(items, "")
	assert.Equal(t, items, got)
}

func TestFilterPluginItems_Match(t *testing.T) {
	items := []PluginItem{
		{ID: "my-plugin@1.0"},
		{ID: "other@2.0"},
	}
	got := filterPluginItems(items, "my-plug")
	assert.Len(t, got, 1)
	assert.Equal(t, "my-plugin@1.0", got[0].ID)
}

func TestFilterAvailablePlugins_MatchByName(t *testing.T) {
	items := []AvailablePluginItem{
		{Name: "Plugin Alpha", Description: "desc1"},
		{Name: "Plugin Beta", Description: "desc2"},
	}
	got := filterAvailablePlugins(items, "alpha")
	assert.Len(t, got, 1)
	assert.Equal(t, "Plugin Alpha", got[0].Name)
}

func TestFilterAvailablePlugins_MatchByDescription(t *testing.T) {
	items := []AvailablePluginItem{
		{Name: "Plugin A", Description: "handles authentication"},
		{Name: "Plugin B", Description: "handles logging"},
	}
	got := filterAvailablePlugins(items, "auth")
	assert.Len(t, got, 1)
	assert.Equal(t, "Plugin A", got[0].Name)
}

func TestFilterMCPItems_Match(t *testing.T) {
	items := []MCPItem{
		{Name: "context7"},
		{Name: "github"},
		{Name: "firecrawl"},
	}
	got := filterMCPItems(items, "git")
	assert.Len(t, got, 1)
	assert.Equal(t, "github", got[0].Name)
}

func TestFilterMCPItems_EmptyQuery(t *testing.T) {
	items := []MCPItem{{Name: "a"}, {Name: "b"}}
	got := filterMCPItems(items, "")
	assert.Equal(t, items, got)
}
