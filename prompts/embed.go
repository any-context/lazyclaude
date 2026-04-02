// Package prompts provides the default PM and Worker system prompt templates.
// These are embedded at compile time and serve as fallbacks when no custom
// prompt file is found in the project or worktree directory.
package prompts

import _ "embed"

//go:embed base.md
var defaultBase string

//go:embed pm.md
var defaultPM string

//go:embed worker.md
var defaultWorker string

// DefaultBase returns the embedded base communication reference template.
func DefaultBase() string { return defaultBase }

// DefaultPM returns the embedded PM system prompt template.
func DefaultPM() string { return defaultPM }

// DefaultWorker returns the embedded Worker system prompt template.
func DefaultWorker() string { return defaultWorker }
