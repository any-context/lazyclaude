package gui

import (
	"github.com/KEMSHlM/lazyclaude/internal/core/choice"
	"github.com/KEMSHlM/lazyclaude/internal/core/config"
)

// Choice aliases for backward compatibility.
type Choice = choice.Choice

const (
	ChoiceAccept = choice.Accept
	ChoiceAllow  = choice.Allow
	ChoiceReject = choice.Reject
	ChoiceCancel = choice.Cancel
)

// WriteChoiceFile delegates to choice.WriteFile.
func WriteChoiceFile(paths config.Paths, window string, c Choice) error {
	return choice.WriteFile(paths, window, c)
}

// ReadChoiceFile delegates to choice.ReadFile.
func ReadChoiceFile(paths config.Paths, window string) (Choice, error) {
	return choice.ReadFile(paths, window)
}
