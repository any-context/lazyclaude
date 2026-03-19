package choice

import (
	"fmt"
	"os"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
)

// Choice represents a user's selection in a diff/tool popup.
type Choice int

const (
	Accept Choice = 1 // y — accept
	Allow  Choice = 2 // a — allow always
	Reject Choice = 3 // n — reject
	Cancel Choice = 0 // Esc — cancel
)

// WriteFile writes the choice to a file for the MCP server to read.
func WriteFile(paths config.Paths, window string, c Choice) error {
	path := paths.ChoiceFile(window)
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", c)), 0o600)
}

// ReadFile reads and removes the choice file.
func ReadFile(paths config.Paths, window string) (Choice, error) {
	path := paths.ChoiceFile(window)

	data, err := os.ReadFile(path)
	if err != nil {
		return Cancel, err
	}

	_ = os.Remove(path) // best-effort cleanup

	var val int
	if _, err := fmt.Sscanf(string(data), "%d", &val); err != nil {
		return Cancel, fmt.Errorf("parse choice: %w", err)
	}

	return Choice(val), nil
}
