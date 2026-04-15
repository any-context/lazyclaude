package profile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidate_Name(t *testing.T) {
	t.Parallel()
	valid := []string{"default", "opus", "a", "claude-beta", "claude_1", "A_B-9", strings.Repeat("x", 64), "0", "abc-def_ghi"}
	invalid := []string{"", " ", "has space", "bad!", "日本語", "has/slash", "has.dot", strings.Repeat("x", 65)}

	for _, name := range valid {
		p := ProfileDef{Name: name, Command: "claude"}
		if err := Validate(p); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range invalid {
		p := ProfileDef{Name: name, Command: "claude"}
		if err := Validate(p); err == nil {
			t.Errorf("Validate(%q) = nil, want error", name)
		}
	}
}

func TestValidate_CommandRequired(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		command string
		wantErr bool
	}{
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"simple", "claude", false},
		{"path", "/usr/bin/claude", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(ProfileDef{Name: "p", Command: tc.command})
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate(command=%q) err=%v, wantErr=%v", tc.command, err, tc.wantErr)
			}
		})
	}
}

func TestValidate_BannedFlags(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"--session-id", "abc"},
		{"--session-id=abc"},
		{"--resume"},
		{"--fork-session"},
		{"--settings", "foo.json"},
		{"--settings=foo.json"},
		{"--append-system-prompt", "x"},
	}
	for _, args := range cases {
		p := ProfileDef{Name: "p", Command: "claude", Args: args}
		if err := Validate(p); err == nil {
			t.Errorf("Validate(args=%v) = nil, want banned-flag error", args)
		}
	}

	// Allowed: unrelated flags, and flags that happen to contain banned
	// substrings but are not exact matches (e.g. `--resume-later`).
	ok := [][]string{
		{"--model=opus-4"},
		{"--resume-later"},
		{"--session-idk"},
		{"--dangerously-skip-permissions"},
	}
	for _, args := range ok {
		p := ProfileDef{Name: "p", Command: "claude", Args: args}
		if err := Validate(p); err != nil {
			t.Errorf("Validate(args=%v) = %v, want nil", args, err)
		}
	}
}

func TestValidate_EnvKey(t *testing.T) {
	t.Parallel()
	valid := []string{"A", "ABC", "ANTHROPIC_MODEL", "_X", "A1", "A_B_C"}
	invalid := []string{"", "a", "abc", "1A", "A-B", "A.B", " A", "A B"}

	for _, k := range valid {
		p := ProfileDef{Name: "p", Command: "c", Env: map[string]string{k: "v"}}
		if err := Validate(p); err != nil {
			t.Errorf("Validate(env=%q) = %v, want nil", k, err)
		}
	}
	for _, k := range invalid {
		p := ProfileDef{Name: "p", Command: "c", Env: map[string]string{k: "v"}}
		if err := Validate(p); err == nil {
			t.Errorf("Validate(env=%q) = nil, want error", k)
		}
	}
}

func TestLoad_FileAbsent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	cfg, profiles, err := Load(path)
	if err != nil {
		t.Fatalf("Load(absent) err=%v, want nil", err)
	}
	if cfg != nil {
		t.Errorf("Load(absent) cfg=%v, want nil", cfg)
	}
	if len(profiles) != 1 || profiles[0].Name != BuiltinDefaultName || !profiles[0].Builtin {
		t.Errorf("Load(absent) profiles=%#v, want single builtin default", profiles)
	}
}

func TestLoad_BrokenJSON_ReportsLineCol(t *testing.T) {
	t.Parallel()
	body := "{\n  \"version\": 1,\n  \"profiles\": [\n    {\"name\": \"opus\", }\n  ]\n}\n"
	path := writeFile(t, body)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("Load(broken) err=nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid JSON at line ") || !strings.Contains(msg, "col ") {
		t.Errorf("error %q missing line/col annotation", msg)
	}
}

func TestLoad_VersionMismatch(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `{"version": 2, "profiles": []}`)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("Load(version=2) err=nil, want error")
	}
	if !strings.Contains(err.Error(), "version 2 is not supported") {
		t.Errorf("error %q missing version message", err)
	}
}

func TestLoad_VersionMissing(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `{"profiles": []}`)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("Load(no version) err=nil, want error")
	}
	if !strings.Contains(err.Error(), `missing required "version" field`) {
		t.Errorf("error %q missing hint", err)
	}
}

func TestLoad_TypeMismatchError_ReportsLineCol(t *testing.T) {
	t.Parallel()
	body := "{\n  \"version\": \"one\",\n  \"profiles\": []\n}\n"
	path := writeFile(t, body)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("Load(type mismatch) err=nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid JSON at line ") || !strings.Contains(msg, "col ") {
		t.Errorf("error %q missing line/col annotation", msg)
	}
	var typErr *json.UnmarshalTypeError
	if !errors.As(err, &typErr) {
		t.Errorf("error chain does not contain *json.UnmarshalTypeError: %v", err)
	}
}

func TestLoad_UserDefinesDefault_NoBuiltinInjected(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `{"version":1,"profiles":[
		{"name":"default","command":"my-claude"},
		{"name":"opus","command":"claude","default":true}
	]}`)

	cfg, profiles, err := Load(path)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	if cfg == nil {
		t.Fatal("cfg=nil, want parsed")
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles len=%d, want 2", len(profiles))
	}
	for _, p := range profiles {
		if p.Builtin {
			t.Errorf("profile %q should not be builtin", p.Name)
		}
	}
	if profiles[0].Name != "default" || profiles[0].Command != "my-claude" {
		t.Errorf("user-defined default was overridden: %+v", profiles[0])
	}
}

func TestLoad_BuiltinInjectedWhenMissing(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `{"version":1,"profiles":[
		{"name":"opus","command":"claude","default":true}
	]}`)

	_, profiles, err := Load(path)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles len=%d, want 2 (user + builtin)", len(profiles))
	}
	last := profiles[len(profiles)-1]
	if last.Name != BuiltinDefaultName || !last.Builtin {
		t.Errorf("last profile=%+v, want builtin default", last)
	}
}

func TestLoad_EffectiveIsDeepCopy(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `{"version":1,"profiles":[
		{"name":"opus","command":"claude","args":["--model=opus"],"env":{"K":"v"}}
	]}`)

	cfg, effective, err := Load(path)
	if err != nil {
		t.Fatalf("Load err=%v", err)
	}
	if cfg == nil {
		t.Fatal("cfg=nil")
	}
	// Mutate effective; cfg must stay untouched.
	effective[0].Args[0] = "--evil"
	effective[0].Env["K"] = "mutated"

	if cfg.Profiles[0].Args[0] != "--model=opus" {
		t.Errorf("cfg Args aliased: %q", cfg.Profiles[0].Args[0])
	}
	if cfg.Profiles[0].Env["K"] != "v" {
		t.Errorf("cfg Env aliased: %q", cfg.Profiles[0].Env["K"])
	}
}

func TestLoad_DuplicateName(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `{"version":1,"profiles":[
		{"name":"opus","command":"a"},
		{"name":"opus","command":"b"}
	]}`)

	_, _, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("Load err=%v, want duplicate-name error", err)
	}
}

func TestLoad_UnknownField(t *testing.T) {
	t.Parallel()
	path := writeFile(t, `{"version":1,"profiles":[],"extras":{}}`)

	_, _, err := Load(path)
	if err == nil {
		t.Fatal("Load(unknown field) err=nil, want error")
	}
}

func TestResolveDefault_NoDefaultReturnsBuiltin(t *testing.T) {
	t.Parallel()
	profiles := []ProfileDef{
		{Name: "opus", Command: "claude"},
		{Name: "sonnet", Command: "claude"},
	}
	got, warns := ResolveDefault(profiles)
	if got.Name != BuiltinDefaultName || !got.Builtin {
		t.Errorf("ResolveDefault got=%+v, want builtin", got)
	}
	if len(warns) != 0 {
		t.Errorf("warns=%v, want none", warns)
	}
}

func TestResolveDefault_UserDefinedDefaultName_OverridesBuiltin(t *testing.T) {
	t.Parallel()
	profiles := []ProfileDef{
		{Name: "opus", Command: "claude"},
		{Name: BuiltinDefaultName, Command: "my-claude"},
	}
	got, warns := ResolveDefault(profiles)
	if got.Name != BuiltinDefaultName || got.Command != "my-claude" || got.Builtin {
		t.Errorf("ResolveDefault got=%+v, want user-defined \"default\" (my-claude, not builtin)", got)
	}
	if len(warns) != 0 {
		t.Errorf("warns=%v, want none", warns)
	}
}

func TestResolveDefault_SingleDefault(t *testing.T) {
	t.Parallel()
	profiles := []ProfileDef{
		{Name: "opus", Command: "claude", Default: true},
		{Name: "sonnet", Command: "claude"},
	}
	got, warns := ResolveDefault(profiles)
	if got.Name != "opus" {
		t.Errorf("got=%+v, want opus", got)
	}
	if len(warns) != 0 {
		t.Errorf("warns=%v, want none", warns)
	}
}

func TestResolveDefault_MultipleDefault_FirstWinsWithWarning(t *testing.T) {
	t.Parallel()
	profiles := []ProfileDef{
		{Name: "opus", Command: "claude", Default: true},
		{Name: "sonnet", Command: "claude", Default: true},
		{Name: "haiku", Command: "claude", Default: true},
	}
	got, warns := ResolveDefault(profiles)
	if got.Name != "opus" {
		t.Errorf("got=%+v, want opus (first)", got)
	}
	if len(warns) != 1 {
		t.Fatalf("warns=%v, want exactly one warning", warns)
	}
	if !strings.Contains(warns[0], `"opus"`) ||
		!strings.Contains(warns[0], `"sonnet"`) ||
		!strings.Contains(warns[0], `"haiku"`) {
		t.Errorf("warning missing names: %q", warns[0])
	}
}

func TestBuiltinDefault_Shape(t *testing.T) {
	t.Parallel()
	b := BuiltinDefault()
	if b.Name != BuiltinDefaultName || b.Command != "claude" || !b.Builtin {
		t.Errorf("BuiltinDefault=%+v", b)
	}
	if b.Default {
		t.Errorf("builtin.Default should be false; resolution relies on fallback")
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	t.Setenv("LAZYCLAUDE_PROFILE_TEST", "/tmp/fixture")

	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"~", home},
		{"~/bin/claude", filepath.Join(home, "bin/claude")},
		{"/abs/path", "/abs/path"},
		{"$LAZYCLAUDE_PROFILE_TEST/bin", "/tmp/fixture/bin"},
		{"${LAZYCLAUDE_PROFILE_TEST}/bin", "/tmp/fixture/bin"},
		{"/prefix/~/suffix", "/prefix/~/suffix"},
		{"~notuser/path", "~notuser/path"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ExpandPath(tc.in)
			if err != nil {
				t.Fatalf("ExpandPath(%q) err=%v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ExpandPath(%q)=%q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandPath_HomeOnly(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	t.Setenv("HOME", home) // ensure $HOME expansion matches os.UserHomeDir on this platform

	got, err := ExpandPath("$HOME/bin")
	if err != nil {
		t.Fatalf("expandPath err=%v", err)
	}
	want := home + "/bin"
	if got != want {
		t.Errorf("ExpandPath($HOME/bin)=%q, want %q", got, want)
	}

	got2, err := ExpandPath("${HOME}/bin")
	if err != nil {
		t.Fatalf("expandPath err=%v", err)
	}
	if got2 != want {
		t.Errorf("ExpandPath(${HOME}/bin)=%q, want %q", got2, want)
	}
}

func TestErrProfileNotFound(t *testing.T) {
	t.Parallel()
	if !errors.Is(ErrProfileNotFound, ErrProfileNotFound) {
		t.Fatal("sentinel identity")
	}
}

// writeFile writes body to a temp file and returns the path.
func writeFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
