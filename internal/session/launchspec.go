package session

import (
	"fmt"
	"os"

	"github.com/any-context/lazyclaude/internal/profile"
)

// LaunchSpec is the fully-resolved launch configuration for a Claude Code
// session. It is derived from a profile.ProfileDef by expanding `~` and
// `$VAR` references in the command path and in env values. Session identity
// flags (--session-id, --resume, --settings, --append-system-prompt) are not
// represented here; they are injected by the launcher-script builder.
type LaunchSpec struct {
	// Command is the claude executable path (or alias) with ~/$VAR expanded.
	Command string
	// Args are extra CLI arguments in raw form. The launcher-script builder
	// applies shell.Quote at emit time; callers must not pre-quote.
	Args []string
	// Env is the environment variable map with values $VAR-expanded.
	Env map[string]string
}

// NewLaunchSpec converts a profile.ProfileDef into a LaunchSpec.
//
// The caller is responsible for having validated p via profile.Validate
// (Load() does this automatically). NewLaunchSpec does not revalidate.
func NewLaunchSpec(p profile.ProfileDef) (LaunchSpec, error) {
	cmd, err := profile.ExpandPath(p.Command)
	if err != nil {
		return LaunchSpec{}, fmt.Errorf("expand command %q: %w", p.Command, err)
	}
	var args []string
	if len(p.Args) > 0 {
		args = make([]string, len(p.Args))
		copy(args, p.Args)
	}
	var env map[string]string
	if len(p.Env) > 0 {
		env = make(map[string]string, len(p.Env))
		for k, v := range p.Env {
			env[k] = os.ExpandEnv(v)
		}
	}
	return LaunchSpec{Command: cmd, Args: args, Env: env}, nil
}
