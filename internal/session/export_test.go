package session

// ClaudeEnv exports claudeEnv for testing.
var ClaudeEnv = claudeEnv

// BuildClaudeCommand exports buildClaudeCommand for testing. Returns the
// shell command; the caller is responsible for invoking the cleanup function
// (second return of the underlying method) when appropriate.
func (m *Manager) BuildClaudeCommand(sess Session, spec LaunchSpec) (string, func(), error) {
	return m.buildClaudeCommand(sess, spec)
}

// HasSessionFlag exports hasSessionFlag for testing.
var HasSessionFlag = hasSessionFlag

// WriteLauncher exports writeLauncher for testing.
var WriteLauncher = writeLauncher

// LauncherOpts exports launcherOpts for testing.
type LauncherOpts = launcherOpts

// SplitOptions exports splitOptions for testing.
var SplitOptions = splitOptions
