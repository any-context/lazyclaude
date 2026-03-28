package gui_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTreeNodes_Empty(t *testing.T) {
	t.Parallel()
	nodes := gui.BuildTreeNodes(nil)
	assert.Empty(t, nodes)
}

func TestBuildTreeNodes_SingleProjectExpanded(t *testing.T) {
	t.Parallel()
	pm := &gui.SessionItem{ID: "pm-1", Name: "pm", Role: "pm", Status: "Running"}
	projects := []gui.ProjectItem{
		{
			ID:       "proj-1",
			Name:     "lazyclaude",
			Expanded: true,
			PM:       pm,
			Sessions: []gui.SessionItem{
				{ID: "s1", Name: "feat-auth", Status: "Running"},
				{ID: "s2", Name: "fix-bug", Status: "Detached"},
			},
		},
	}

	nodes := gui.BuildTreeNodes(projects)
	require.Len(t, nodes, 4) // project + PM + 2 sessions

	assert.Equal(t, gui.ProjectNode, nodes[0].Kind)
	assert.Equal(t, "proj-1", nodes[0].ProjectID)
	assert.Equal(t, "lazyclaude", nodes[0].Project.Name)

	assert.Equal(t, gui.SessionNode, nodes[1].Kind)
	assert.Equal(t, "pm-1", nodes[1].Session.ID)

	assert.Equal(t, gui.SessionNode, nodes[2].Kind)
	assert.Equal(t, "feat-auth", nodes[2].Session.Name)

	assert.Equal(t, gui.SessionNode, nodes[3].Kind)
	assert.Equal(t, "fix-bug", nodes[3].Session.Name)
}

func TestBuildTreeNodes_CollapsedProject(t *testing.T) {
	t.Parallel()
	projects := []gui.ProjectItem{
		{
			ID:       "proj-1",
			Name:     "lazyclaude",
			Expanded: false,
			Sessions: []gui.SessionItem{
				{ID: "s1", Name: "feat-auth"},
			},
		},
	}

	nodes := gui.BuildTreeNodes(projects)
	require.Len(t, nodes, 1, "collapsed project shows only project row")
	assert.Equal(t, gui.ProjectNode, nodes[0].Kind)
}

func TestBuildTreeNodes_MultipleProjects(t *testing.T) {
	t.Parallel()
	projects := []gui.ProjectItem{
		{
			ID:       "proj-1",
			Name:     "lazyclaude",
			Expanded: true,
			Sessions: []gui.SessionItem{
				{ID: "s1", Name: "main"},
			},
		},
		{
			ID:       "proj-2",
			Name:     "my-api",
			Expanded: false,
			Sessions: []gui.SessionItem{
				{ID: "s2", Name: "app"},
			},
		},
	}

	nodes := gui.BuildTreeNodes(projects)
	require.Len(t, nodes, 3) // proj-1 + session + proj-2 (collapsed)

	assert.Equal(t, gui.ProjectNode, nodes[0].Kind)
	assert.Equal(t, "lazyclaude", nodes[0].Project.Name)

	assert.Equal(t, gui.SessionNode, nodes[1].Kind)
	assert.Equal(t, "main", nodes[1].Session.Name)

	assert.Equal(t, gui.ProjectNode, nodes[2].Kind)
	assert.Equal(t, "my-api", nodes[2].Project.Name)
}

func TestBuildTreeNodes_NoPM(t *testing.T) {
	t.Parallel()
	projects := []gui.ProjectItem{
		{
			ID:       "proj-1",
			Name:     "app",
			Expanded: true,
			PM:       nil,
			Sessions: []gui.SessionItem{
				{ID: "s1", Name: "main"},
			},
		},
	}

	nodes := gui.BuildTreeNodes(projects)
	require.Len(t, nodes, 2) // project + session (no PM)
}
