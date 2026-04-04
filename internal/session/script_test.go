package session

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- BuildScript: basic structure ---

func TestBuildScript_Shebang(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{SessionID: "test-1234"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"), "must start with bash shebang")
}

func TestBuildScript_SelfDelete(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{
		SessionID:  "test-1234",
		SelfDelete: true,
	})
	require.NoError(t, err)
	assert.Contains(t, script, `rm -f "$0"`)
}

func TestBuildScript_NoSelfDelete(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{SessionID: "test-1234"})
	require.NoError(t, err)
	assert.NotContains(t, script, `rm -f`)
}

// --- BuildScript: MCP lock file setup ---

func TestBuildScript_MCPLockFile(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{
		SessionID: "test-1234",
		MCP:       &MCPConfig{Port: 9876, Token: "secret-tok"},
	})
	require.NoError(t, err)
	assert.Contains(t, script, `mkdir -p "$HOME/.claude/ide"`)
	assert.Contains(t, script, "9876.lock")
	assert.Contains(t, script, `"authToken":"secret-tok"`)
	assert.Contains(t, script, "trap")
}

func TestBuildScript_NoMCP(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{SessionID: "test-1234"})
	require.NoError(t, err)
	assert.NotContains(t, script, ".claude/ide")
	assert.NotContains(t, script, "LOCKEOF")
}

// --- BuildScript: cd WorkDir ---

func TestBuildScript_WorkDir(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{
		SessionID: "test-1234",
		WorkDir:   "/home/user/my project",
	})
	require.NoError(t, err)
	// Must use POSIX single-quote escaping
	assert.Contains(t, script, "cd '/home/user/my project'")
}

func TestBuildScript_WorkDir_WithSingleQuote(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{
		SessionID: "test-1234",
		WorkDir:   "/home/user/it's a path",
	})
	require.NoError(t, err)
	assert.Contains(t, script, `cd '/home/user/it'\''s a path'`)
}

func TestBuildScript_WorkDir_Dot(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{
		SessionID: "test-1234",
		WorkDir:   ".",
	})
	require.NoError(t, err)
	assert.NotContains(t, script, "cd ")
}

func TestBuildScript_WorkDir_Empty(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{SessionID: "test-1234"})
	require.NoError(t, err)
	assert.NotContains(t, script, "cd ")
}

// --- BuildScript: hooks injection ---

func TestBuildScript_InlineHooks(t *testing.T) {
	t.Parallel()
	hooksJSON := `{"hooks":{"PreToolUse":[{"matcher":"*"}]}}`
	script, err := BuildScript(ScriptConfig{
		SessionID: "test-1234",
		HooksJSON: hooksJSON,
	})
	require.NoError(t, err)
	// Hooks JSON should be written via heredoc
	assert.Contains(t, script, "hooks-settings.json")
	assert.Contains(t, script, hooksJSON)
	// Claude command should reference the settings file
	assert.Contains(t, script, "--settings")
}

func TestBuildScript_NoHooks(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{SessionID: "test-1234"})
	require.NoError(t, err)
	assert.NotContains(t, script, "--settings")
	assert.NotContains(t, script, "hooks-settings")
}

// --- BuildScript: system prompt injection ---

func TestBuildScript_SystemPrompt(t *testing.T) {
	t.Parallel()
	prompt := "You are a Worker Claude Code session.\nNEVER modify files outside this worktree."
	script, err := BuildScript(ScriptConfig{
		SessionID:    "test-1234",
		SystemPrompt: prompt,
	})
	require.NoError(t, err)
	assert.Contains(t, script, "--append-system-prompt")
	// Prompt should be base64-decoded to a temp file
	assert.Contains(t, script, "base64 -d")
	assert.Contains(t, script, "sysprompt-$$.txt")
}

func TestBuildScript_SystemPrompt_WithQuotes(t *testing.T) {
	t.Parallel()
	prompt := `She said "hello" and it's fine`
	script, err := BuildScript(ScriptConfig{
		SessionID:    "test-1234",
		SystemPrompt: prompt,
	})
	require.NoError(t, err)
	assert.Contains(t, script, "--append-system-prompt")
	// Must not break the shell script
	assert.NotContains(t, script, `"hello"`)
	// The prompt is base64-encoded, so raw quotes don't appear
}

func TestBuildScript_SystemPrompt_WithBackticks(t *testing.T) {
	t.Parallel()
	prompt := "Run `git status` to check"
	script, err := BuildScript(ScriptConfig{
		SessionID:    "test-1234",
		SystemPrompt: prompt,
	})
	require.NoError(t, err)
	// Backticks must not cause command substitution
	assert.NotContains(t, script, "`git status`")
}

func TestBuildScript_NoSystemPrompt(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{SessionID: "test-1234"})
	require.NoError(t, err)
	assert.NotContains(t, script, "--append-system-prompt")
}

// --- BuildScript: user prompt ---

func TestBuildScript_UserPrompt(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{
		SessionID:    "test-1234",
		SystemPrompt: "system",
		UserPrompt:   "Fix the bug in auth.go",
	})
	require.NoError(t, err)
	// User prompt should also be base64-encoded
	assert.Contains(t, script, "base64 -d")
}

func TestBuildScript_UserPrompt_RequiresSystemPrompt(t *testing.T) {
	t.Parallel()
	// UserPrompt without SystemPrompt: user prompt becomes positional arg
	script, err := BuildScript(ScriptConfig{
		SessionID:  "test-1234",
		UserPrompt: "Fix the bug",
	})
	require.NoError(t, err)
	// Should still include the user prompt
	assert.Contains(t, script, "base64 -d")
}

// --- BuildScript: auth tokens ---

func TestBuildScript_AuthTokens(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-test-token")
	script, err := BuildScript(ScriptConfig{SessionID: "test-1234"})
	require.NoError(t, err)
	assert.Contains(t, script, "CLAUDE_CODE_AUTO_CONNECT_IDE=true")
	// Token must be quoted to prevent shell metacharacter expansion
	assert.Contains(t, script, "CLAUDE_CODE_OAUTH_TOKEN=")
}

// --- BuildScript: flags ---

func TestBuildScript_Flags(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{
		SessionID: "test-1234",
		Flags:     []string{"--resume", "--working-dir=/tmp"},
	})
	require.NoError(t, err)
	assert.Contains(t, script, "--resume")
	assert.Contains(t, script, "--working-dir=/tmp")
}

// --- BuildScript: exec line structure ---

func TestBuildScript_ExecLine(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{SessionID: "test-1234"})
	require.NoError(t, err)
	// Must use exec "$SHELL" -lic for login shell PATH loading
	assert.Contains(t, script, `exec "$SHELL" -lic`)
	assert.Contains(t, script, "exec claude")
}

// --- BuildScript: full SSH worktree scenario ---

func TestBuildScript_SSHWorktree_Full(t *testing.T) {
	t.Parallel()
	hooksJSON := `{"hooks":{}}`
	script, err := BuildScript(ScriptConfig{
		SessionID:    "abcd1234-ef56-7890",
		WorkDir:      "/home/user/project/.lazyclaude/worktrees/fix-bug",
		MCP:          &MCPConfig{Port: 9876, Token: "mcp-token"},
		HooksJSON:    hooksJSON,
		SystemPrompt: "You are a Worker session.\nSession ID: abcd1234",
		UserPrompt:   "Fix the authentication bug",
	})
	require.NoError(t, err)

	// Structure checks
	assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
	assert.NotContains(t, script, `rm -f "$0"`) // SSH: no self-delete
	assert.Contains(t, script, "9876.lock")      // MCP lock file
	assert.Contains(t, script, "mcp-token")
	assert.Contains(t, script, "cd '/home/user/project/.lazyclaude/worktrees/fix-bug'")
	assert.Contains(t, script, "hooks-settings.json")
	assert.Contains(t, script, "--settings")
	assert.Contains(t, script, "--append-system-prompt")
	assert.Contains(t, script, `exec "$SHELL" -lic`)

	// lazyclaude installed as executable script in PATH
	assert.Contains(t, script, "lazyclaude()")
	assert.Contains(t, script, "LCBINEOF")
	assert.Contains(t, script, "chmod +x '/tmp/lazyclaude/bin/lazyclaude'")
	assert.Contains(t, script, "SETUPEOF")
	assert.Contains(t, script, "export _LC_MCP_PORT=9876")

	// No raw quotes from prompts (all base64-encoded to files)
	assert.NotContains(t, script, "You are a Worker")
	assert.NotContains(t, script, "Fix the authentication")

	// Exec line sources setup.sh inside login shell, then execs claude
	assert.Contains(t, script, `exec "$SHELL" -lic '. /tmp/lazyclaude/setup.sh; exec claude`)
}

// --- BuildScript: full local worktree scenario ---

func TestBuildScript_LocalWorktree_Full(t *testing.T) {
	t.Parallel()
	script, err := BuildScript(ScriptConfig{
		SessionID:    "local-1234",
		SelfDelete:   true,
		HooksJSON:    `{"hooks":{}}`,
		SystemPrompt: "You are a Worker.",
		UserPrompt:   "Implement feature X",
	})
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
	assert.Contains(t, script, `rm -f "$0"`)
	assert.NotContains(t, script, "LOCKEOF")     // No MCP for local
	assert.Contains(t, script, "hooks-settings")  // Hooks still injected
	assert.Contains(t, script, "--settings")
	assert.Contains(t, script, "--append-system-prompt")

	// Local sessions: no lazyclaude shell function
	assert.NotContains(t, script, "lazyclaude()")
	assert.NotContains(t, script, "_LC_MCP_PORT")
}
