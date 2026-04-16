package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/any-context/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestProject(id, name, path string) session.Project {
	return session.Project{
		ID:        id,
		Name:      name,
		Path:      path,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Expanded:  true,
	}
}

func TestStore_Projects_Empty(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	assert.Empty(t, s.Projects())
}

func TestStore_AddSessionCreatesProject(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	sess := newTestSession("id-1", "my-app", "/home/user/my-app")
	s.Add(sess, "")

	projects := s.Projects()
	require.Len(t, projects, 1)
	assert.Equal(t, "/home/user/my-app", projects[0].Path)
	assert.Equal(t, "my-app", projects[0].Name)
	require.Len(t, projects[0].Sessions, 1)
	assert.Equal(t, "id-1", projects[0].Sessions[0].ID)
}

func TestStore_AddWorktreeSessionGroupsUnderProject(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	// Add a regular session
	s.Add(newTestSession("id-1", "main", "/home/user/lazyclaude"), "")
	// Add a worktree session under the same project
	s.Add(newTestSession("id-2", "feat-auth", "/home/user/lazyclaude/.lazyclaude/worktrees/feat-auth"), "")

	projects := s.Projects()
	require.Len(t, projects, 1, "worktree should belong to same project")
	assert.Equal(t, "/home/user/lazyclaude", projects[0].Path)
	require.Len(t, projects[0].Sessions, 2)
}

func TestStore_AddPMSessionGoesToSessions(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	pm := newTestSession("id-pm", "pm", "/home/user/lazyclaude")
	pm.Role = session.RolePM
	s.Add(pm, "")

	projects := s.Projects()
	require.Len(t, projects, 1)
	require.Len(t, projects[0].Sessions, 1, "PM should be in Sessions slice")
	assert.Equal(t, "id-pm", projects[0].Sessions[0].ID)
	assert.Equal(t, session.RolePM, projects[0].Sessions[0].Role)
	pmSess := projects[0].FindPM()
	require.NotNil(t, pmSess)
	assert.Equal(t, "id-pm", pmSess.ID)
}

func TestStore_MultipleProjects(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	s.Add(newTestSession("id-1", "app-a", "/home/user/project-a"), "")
	s.Add(newTestSession("id-2", "app-b", "/home/user/project-b"), "")

	projects := s.Projects()
	require.Len(t, projects, 2)
}

func TestStore_RemoveSessionFromProject(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	s.Add(newTestSession("id-1", "main", "/home/user/lazyclaude"), "")
	s.Add(newTestSession("id-2", "feat", "/home/user/lazyclaude/.lazyclaude/worktrees/feat"), "")

	ok := s.Remove("id-2")
	assert.True(t, ok)

	projects := s.Projects()
	require.Len(t, projects, 1)
	require.Len(t, projects[0].Sessions, 1)
	assert.Equal(t, "id-1", projects[0].Sessions[0].ID)
}

func TestStore_RemoveLastSessionRemovesProject(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")
	s.Add(newTestSession("id-1", "app", "/home/user/app"), "")

	s.Remove("id-1")
	assert.Empty(t, s.Projects())
}

func TestStore_RemovePMFromProject(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	pm := newTestSession("id-pm", "pm", "/home/user/lazyclaude")
	pm.Role = session.RolePM
	s.Add(pm, "")
	s.Add(newTestSession("id-1", "main", "/home/user/lazyclaude"), "")

	ok := s.Remove("id-pm")
	assert.True(t, ok)

	projects := s.Projects()
	require.Len(t, projects, 1)
	assert.Nil(t, projects[0].FindPM())
	require.Len(t, projects[0].Sessions, 1)
}

func TestStore_SaveAndLoad_ProjectFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s1 := session.NewStore(path)
	s1.Add(newTestSession("id-1", "main", "/home/user/lazyclaude"), "")
	pm := newTestSession("id-pm", "pm", "/home/user/lazyclaude")
	pm.Role = session.RolePM
	s1.Add(pm, "")
	s1.Add(newTestSession("id-2", "feat", "/home/user/lazyclaude/.lazyclaude/worktrees/feat"), "")
	require.NoError(t, s1.Save())

	s2 := session.NewStore(path)
	require.NoError(t, s2.Load())

	projects := s2.Projects()
	require.Len(t, projects, 1)
	assert.Equal(t, "/home/user/lazyclaude", projects[0].Path)
	pmSess := projects[0].FindPM()
	require.NotNil(t, pmSess)
	assert.Equal(t, "id-pm", pmSess.ID)
	// PM + main + feat = 3 sessions total
	require.Len(t, projects[0].Sessions, 3)
}

func TestStore_Load_LegacyFormat_Resets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write legacy format (flat []Session)
	legacy := `[{"id":"id-1","name":"my-app","path":"/path","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}]`
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o600))

	s := session.NewStore(path)
	require.NoError(t, s.Load())

	// Legacy format should be reset to empty
	assert.Empty(t, s.Projects())
	assert.Empty(t, s.All())
}

func TestStore_AllSessions_Flat(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	s.Add(newTestSession("id-1", "main", "/home/user/lazyclaude"), "")
	pm := newTestSession("id-pm", "pm", "/home/user/lazyclaude")
	pm.Role = session.RolePM
	s.Add(pm, "")
	s.Add(newTestSession("id-2", "app", "/home/user/other"), "")

	all := s.All()
	assert.Len(t, all, 3, "All() should return all sessions flat including PM")
}

func TestStore_FindByID_AcrossProjects(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	s.Add(newTestSession("id-1", "main", "/home/user/project-a"), "")
	s.Add(newTestSession("id-2", "feat", "/home/user/project-b/.lazyclaude/worktrees/feat"), "")

	found := s.FindByID("id-2")
	require.NotNil(t, found)
	assert.Equal(t, "feat", found.Name)
}

func TestStore_FindProjectByPath(t *testing.T) {
	t.Parallel()
	s := session.NewStore("")

	s.Add(newTestSession("id-1", "main", "/home/user/lazyclaude"), "")

	p := s.FindProjectByPath("/home/user/lazyclaude")
	require.NotNil(t, p)
	assert.Equal(t, "lazyclaude", p.Name)

	p2 := s.FindProjectByPath("/nonexistent")
	assert.Nil(t, p2)
}

func TestStore_SaveFormat_IsProjectArray(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := session.NewStore(path)
	s.Add(newTestSession("id-1", "app", "/home/user/app"), "")
	require.NoError(t, s.Save())

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// Should be a JSON object with "version" and "projects" keys
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	_, hasVersion := raw["version"]
	assert.True(t, hasVersion, "state.json should have version field")
	_, hasProjects := raw["projects"]
	assert.True(t, hasProjects, "state.json should have projects field")
}
