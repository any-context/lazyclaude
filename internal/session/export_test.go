package session

// ClaudeEnv exports claudeEnv for testing.
var ClaudeEnv = claudeEnv

// BuildClaudeCommand exports buildClaudeCommand for testing.
func (m *Manager) BuildClaudeCommand(sess Session) string {
	return m.buildClaudeCommand(sess)
}
