package profile

import (
	"fmt"
	"os"
	"strings"
)

// expandPath expands a leading `~` to the user's home directory and
// substitutes `$VAR` / `${VAR}` references using the process environment.
// The input is never passed to a shell; no command execution occurs.
// A leading `~` is only expanded at position 0; `~` elsewhere is preserved
// (e.g. `/tmp/~foo` stays untouched).
func expandPath(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	out := s
	if strings.HasPrefix(out, "~") {
		rest := out[1:]
		if rest == "" || rest[0] == '/' {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("expand ~: %w", err)
			}
			out = home + rest
		}
	}
	return os.ExpandEnv(out), nil
}
