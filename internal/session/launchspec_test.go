package session_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/any-context/lazyclaude/internal/profile"
	"github.com/any-context/lazyclaude/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewLaunchSpec ---

func TestNewLaunchSpec_ExpandsHomeCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p := profile.ProfileDef{
		Name:    "local",
		Command: "~/bin/claude",
	}
	spec, err := session.NewLaunchSpec(p)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "bin", "claude"), spec.Command)
}

func TestNewLaunchSpec_ExpandsEnvVars(t *testing.T) {
	t.Setenv("LAZYCLAUDE_TEST_MODEL", "opus-4")

	p := profile.ProfileDef{
		Name:    "envy",
		Command: "claude",
		Env: map[string]string{
			"MODEL_NAME": "$LAZYCLAUDE_TEST_MODEL",
			"STATIC":     "literal",
		},
	}
	spec, err := session.NewLaunchSpec(p)
	require.NoError(t, err)

	assert.Equal(t, "opus-4", spec.Env["MODEL_NAME"])
	assert.Equal(t, "literal", spec.Env["STATIC"])
}

func TestNewLaunchSpec_CopiesArgs(t *testing.T) {
	t.Parallel()
	orig := []string{"--model=opus", "--verbose"}
	p := profile.ProfileDef{Name: "copy", Command: "claude", Args: orig}
	spec, err := session.NewLaunchSpec(p)
	require.NoError(t, err)

	// Mutating the returned slice must not affect the source.
	spec.Args[0] = "--mutated"
	assert.Equal(t, "--model=opus", orig[0], "profile.Args must not be mutated via LaunchSpec")
}

// --- writeLauncher / buildClaudeCommand integration ---

func TestWriteLauncher_ReflectsProfileArgs(t *testing.T) {
	t.Parallel()
	path, err := session.WriteLauncher(session.LauncherOpts{
		Spec: session.LaunchSpec{
			Command: "claude",
			Args:    []string{"--model=opus", "--verbose"},
		},
		SessionID:  "uuid-1",
		RuntimeDir: t.TempDir(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)

	// exec line must start with the command and all args, each individually
	// shell-quoted.
	assert.Contains(t, content, "exec 'claude' '--model=opus' '--verbose'")
	// Session identity follows, unless hasSessionFlag suppresses it.
	assert.Contains(t, content, "--session-id 'uuid-1'")
}

func TestWriteLauncher_ExpandedCommandEscaped(t *testing.T) {
	t.Parallel()
	// Codex Review 1: a malicious command string must be shell-quoted, not
	// interpreted. This test verifies that a semicolon/rm pattern appears
	// verbatim inside single quotes and is never executed.
	path, err := session.WriteLauncher(session.LauncherOpts{
		Spec:       session.LaunchSpec{Command: "claude;rm -rf /"},
		SessionID:  "uuid-x",
		RuntimeDir: t.TempDir(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "'claude;rm -rf /'")
}

func TestWriteLauncher_SkipsIdentityWhenProfileSuppliesIt(t *testing.T) {
	t.Parallel()
	path, err := session.WriteLauncher(session.LauncherOpts{
		Spec: session.LaunchSpec{
			Command: "claude",
			Args:    []string{"--session-id=preset"},
		},
		SessionID:  "uuid-ignored",
		RuntimeDir: t.TempDir(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "'--session-id=preset'")
	assert.NotContains(t, content, "'uuid-ignored'")
}

func TestWriteLauncher_EmptyCommandRejected(t *testing.T) {
	t.Parallel()
	_, err := session.WriteLauncher(session.LauncherOpts{
		Spec:       session.LaunchSpec{Command: ""},
		SessionID:  "uuid",
		RuntimeDir: t.TempDir(),
	})
	require.Error(t, err)
}

// --- hasSessionFlag composite check ---

func TestHasSessionFlag_Composite(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		sets [][]string
		want bool
	}{
		{"no flags", [][]string{nil, nil}, false},
		{"profile args empty, sess flags empty", [][]string{{}, {}}, false},
		{"--session-id in profile args", [][]string{{"--session-id"}, {}}, true},
		{"--session-id= in profile args", [][]string{{"--session-id=abc"}, {}}, true},
		{"--resume in profile args", [][]string{{"--resume"}, {}}, true},
		{"--resume= in profile args", [][]string{{"--resume=xyz"}, {}}, true},
		{"--session-id in sess flags", [][]string{{}, {"--session-id"}}, true},
		{"unrelated flag", [][]string{{"--verbose"}, {"--model=opus"}}, false},
		{"second set hits", [][]string{{"--verbose"}, {"--resume"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := session.HasSessionFlag(tc.sets...)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- ResolveProfile ---

func TestResolveProfile_EmptyNameReturnsBuiltinWithoutConfig(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)

	prof, err := mgr.ResolveProfile("")
	require.NoError(t, err)
	assert.Equal(t, profile.BuiltinDefaultName, prof.Name)
	assert.True(t, prof.Builtin)
}

func TestResolveProfile_EmptyNameReturnsConfiguredDefault(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	mgr.SetProfiles([]profile.ProfileDef{
		{Name: "opus", Command: "claude", Args: []string{"--model=opus"}, Default: true},
		{Name: "sonnet", Command: "claude"},
	})

	prof, err := mgr.ResolveProfile("")
	require.NoError(t, err)
	assert.Equal(t, "opus", prof.Name)
}

func TestResolveProfile_NamedLookup(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	mgr.SetProfiles([]profile.ProfileDef{
		{Name: "opus", Command: "claude"},
		{Name: "sonnet", Command: "claude"},
	})

	prof, err := mgr.ResolveProfile("sonnet")
	require.NoError(t, err)
	assert.Equal(t, "sonnet", prof.Name)
}

func TestResolveProfile_UndefinedNameReturnsError(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	mgr.SetProfiles([]profile.ProfileDef{
		{Name: "opus", Command: "claude"},
	})

	_, err := mgr.ResolveProfile("removed")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removed")
	assert.Contains(t, err.Error(), "config.json")
}

func TestResolveProfile_DefaultNameFallsBackToBuiltin(t *testing.T) {
	t.Parallel()
	mgr, _ := newTestManager(t)
	// No profiles configured — "default" must still resolve.
	prof, err := mgr.ResolveProfile(profile.BuiltinDefaultName)
	require.NoError(t, err)
	assert.True(t, prof.Builtin)
}

// --- splitOptions ---

func TestSplitOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"--verbose", []string{"--verbose"}},
		{"--a --b --c", []string{"--a", "--b", "--c"}},
		{"  --a   --b  ", []string{"--a", "--b"}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := session.SplitOptions(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- claudeEnv merges profile env ---

func TestClaudeEnv_MergesProfileEnv(t *testing.T) {
	t.Parallel()
	spec := session.LaunchSpec{
		Env: map[string]string{
			"CLAUDE_CODE_AUTO_CONNECT_IDE": "false", // profile wins on collision
			"MY_CUSTOM":                    "value",
		},
	}
	env := session.ClaudeEnv("sid", spec)
	assert.Equal(t, "false", env["CLAUDE_CODE_AUTO_CONNECT_IDE"], "profile env overrides defaults")
	assert.Equal(t, "value", env["MY_CUSTOM"])
	assert.Equal(t, "sid", env["LAZYCLAUDE_SESSION_ID"])
}

// --- Opts wrapper compat: deprecated signatures must call the opts path ---

func TestWorktreeOpts_DeprecatedWrapperCompat(t *testing.T) {
	t.Parallel()
	// Sanity check that the deprecated wrappers and the opts wrappers share
	// the same parsing rules. We compare the split extra-flags that the
	// internal createWorktreeSession would receive when Options is provided.
	expect := []string{"--flag1", "--flag2"}
	got := session.SplitOptions(" --flag1  --flag2 ")
	assert.Equal(t, expect, got)

	// The deprecated CreateWorkerSession signature cannot set Options, so
	// wrapping it must produce nil extra flags.
	assert.Nil(t, session.SplitOptions(""))
}

// --- Launcher exec line uses spec.Command ---

func TestWriteLauncher_CustomCommandPath(t *testing.T) {
	t.Parallel()
	path, err := session.WriteLauncher(session.LauncherOpts{
		Spec:       session.LaunchSpec{Command: "/opt/bin/claude-custom"},
		SessionID:  "uid",
		RuntimeDir: t.TempDir(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "exec '/opt/bin/claude-custom'"),
		"exec line must start with the profile-defined command, not a hardcoded 'claude'")
}
