// Package profile models user-defined launch profiles for Claude sessions.
//
// A profile specifies the executable and arguments used when spawning a
// session. Profiles are loaded from `$HOME/.lazyclaude/config.json` and
// resolved in the session/GUI/CLI layers. This package contains only pure
// data model, parsing, and validation logic; it does not touch shells,
// processes, or the filesystem beyond reading the config file.
package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// CurrentVersion is the only supported config.json schema version.
const CurrentVersion = 1

// BuiltinDefaultName is the reserved name for the built-in default profile.
const BuiltinDefaultName = "default"

// ProfileDef describes one launch profile.
//
// Builtin is not marshalled to JSON; it is set internally to mark the
// injected built-in default so that downstream consumers (CLI, GUI) can
// distinguish it from user definitions.
type ProfileDef struct {
	Name        string            `json:"name"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Description string            `json:"description,omitempty"`
	Default     bool              `json:"default,omitempty"`
	Builtin     bool              `json:"-"`
}

// Config is the on-disk representation of `$HOME/.lazyclaude/config.json`.
type Config struct {
	Version  int          `json:"version"`
	Profiles []ProfileDef `json:"profiles"`
}

// BuiltinDefault returns the built-in default profile used when the user
// has not defined one under the reserved name.
func BuiltinDefault() ProfileDef {
	return ProfileDef{
		Name:    BuiltinDefaultName,
		Command: "claude",
		Builtin: true,
	}
}

var (
	nameRE   = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	envKeyRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

	// bannedFlags are reserved for lazyclaude's session lifecycle and must
	// not be set via profile.args. Allowing them risks double-specification
	// or session identity collisions. Only long-form flags are listed
	// because the Claude CLI does not expose short-form aliases for any
	// of these at the time of writing; add short forms here if that
	// ever changes.
	bannedFlags = []string{
		"--session-id",
		"--resume",
		"--fork-session",
		"--settings",
		"--append-system-prompt",
	}
)

// ErrProfileNotFound is returned by callers when a requested profile name
// is not present in the loaded config.
var ErrProfileNotFound = errors.New("profile not found")

// Load reads and validates `path`. It returns the parsed Config (nil when
// the file does not exist) and the effective profile list with the
// built-in default injected when the user has not redefined `default`.
//
// When the file is absent, Load returns `(nil, []ProfileDef{BuiltinDefault()}, nil)`.
// JSON syntax errors are annotated with the offending line and column.
// Any unsupported `version` value is rejected.
func Load(path string) (*Config, []ProfileDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, []ProfileDef{BuiltinDefault()}, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, nil, annotateJSONError(data, err)
	}

	if cfg.Version == 0 {
		return nil, nil, errors.New("config.json missing required \"version\" field (expected 1)")
	}
	if cfg.Version != CurrentVersion {
		return nil, nil, fmt.Errorf("config.json version %d is not supported (expected %d)", cfg.Version, CurrentVersion)
	}

	seen := make(map[string]struct{}, len(cfg.Profiles))
	for i := range cfg.Profiles {
		p := cfg.Profiles[i]
		if err := Validate(p); err != nil {
			return nil, nil, fmt.Errorf("profile[%d] %q: %w", i, p.Name, err)
		}
		if _, dup := seen[p.Name]; dup {
			return nil, nil, fmt.Errorf("profile[%d] %q: duplicate name", i, p.Name)
		}
		seen[p.Name] = struct{}{}
	}

	effective := make([]ProfileDef, 0, len(cfg.Profiles)+1)
	for _, p := range cfg.Profiles {
		effective = append(effective, cloneProfileDef(p))
	}
	if _, hasDefault := seen[BuiltinDefaultName]; !hasDefault {
		effective = append(effective, BuiltinDefault())
	}

	return &cfg, effective, nil
}

// cloneProfileDef returns a deep copy so callers that mutate Args or Env
// on the effective slice cannot corrupt the parsed *Config.
func cloneProfileDef(p ProfileDef) ProfileDef {
	c := p
	if p.Args != nil {
		c.Args = make([]string, len(p.Args))
		copy(c.Args, p.Args)
	}
	if p.Env != nil {
		c.Env = make(map[string]string, len(p.Env))
		for k, v := range p.Env {
			c.Env[k] = v
		}
	}
	return c
}

// Validate checks that a single ProfileDef is well-formed. It does not
// expand paths or touch the filesystem.
func Validate(p ProfileDef) error {
	if !nameRE.MatchString(p.Name) {
		return fmt.Errorf("name %q: must match %s", p.Name, nameRE)
	}
	if strings.TrimSpace(p.Command) == "" {
		return errors.New("command: must not be empty")
	}
	for i, a := range p.Args {
		if err := checkBannedFlag(a); err != nil {
			return fmt.Errorf("args[%d]: %w", i, err)
		}
	}
	for k := range p.Env {
		if !envKeyRE.MatchString(k) {
			return fmt.Errorf("env key %q: must match %s", k, envKeyRE)
		}
	}
	return nil
}

// ResolveDefault returns the first profile with Default=true in `profiles`.
// Additional default:true entries are reported via warning strings so that
// callers can surface them in debug logs without affecting control flow.
// When no profile is marked default, the built-in default is returned.
func ResolveDefault(profiles []ProfileDef) (ProfileDef, []string) {
	var (
		chosen     ProfileDef
		found      bool
		warnings   []string
		otherNames []string
	)
	for _, p := range profiles {
		if !p.Default {
			continue
		}
		if !found {
			chosen = p
			found = true
			continue
		}
		otherNames = append(otherNames, p.Name)
	}
	if !found {
		return BuiltinDefault(), nil
	}
	if len(otherNames) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"multiple profiles marked default: using %q, ignoring %s",
			chosen.Name, strings.Join(quoteAll(otherNames), ", "),
		))
	}
	return chosen, warnings
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// checkBannedFlag rejects args that would collide with lazyclaude's own
// session-lifecycle flags, whether written as `--flag` or `--flag=value`.
func checkBannedFlag(arg string) error {
	for _, f := range bannedFlags {
		if arg == f || strings.HasPrefix(arg, f+"=") {
			return fmt.Errorf("flag %q is reserved by lazyclaude and must not appear in profile.args", f)
		}
	}
	return nil
}

// annotateJSONError rewrites json.SyntaxError (and adjacent cases) into a
// message that carries the offending line and column so the user can find
// the mistake in their config.json.
func annotateJSONError(data []byte, err error) error {
	var synErr *json.SyntaxError
	if errors.As(err, &synErr) {
		line, col := offsetToLineCol(data, synErr.Offset)
		return fmt.Errorf("invalid JSON at line %d, col %d: %w", line, col, synErr)
	}
	var typErr *json.UnmarshalTypeError
	if errors.As(err, &typErr) {
		line, col := offsetToLineCol(data, typErr.Offset)
		return fmt.Errorf("invalid JSON at line %d, col %d: %w", line, col, typErr)
	}
	return fmt.Errorf("invalid JSON: %w", err)
}

func offsetToLineCol(data []byte, offset int64) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	line, col := 1, 1
	for i := int64(0); i < offset; i++ {
		if data[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}
