package tmuxadapter_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/stretchr/testify/assert"
)

// --- Benchmarks ---

var benchPaneContent = ` Bash command

   for i in $(seq 1 10); do echo "line $i"; done && ls /tmp && ps aux | head -5 && echo "done"
   Long shell script with loop, ls, ps

 Command contains $() command substitution

 Do you want to proceed?
 ❯ 1. Yes
   2. No

 Esc to cancel · Tab to amend · ctrl+e to explain`

func BenchmarkDetectMaxOption(b *testing.B) {
	for b.Loop() {
		tmuxadapter.DetectMaxOption(benchPaneContent)
	}
}

func BenchmarkDetectMaxOption_LargePane(b *testing.B) {
	// Simulate a 200-line pane with options at the bottom
	var lines string
	for i := 0; i < 190; i++ {
		lines += "output line with some text and numbers 42\n"
	}
	lines += benchPaneContent
	for b.Loop() {
		tmuxadapter.DetectMaxOption(lines)
	}
}

func TestDetectMaxOption(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name: "3-option dialog (standard)",
			input: ` Do you want to create hello.txt?
 > 1. Yes
   2. Yes, allow all edits during this session (shift+tab)
   3. No`,
			expected: 3,
		},
		{
			name: "2-option dialog",
			input: ` Allow this action?
 > 1. Yes
   2. No`,
			expected: 2,
		},
		{
			name: "no options found",
			input: `Some random output
with no numbered options`,
			expected: 3, // default
		},
		{
			name:     "empty string",
			input:    "",
			expected: 3, // default
		},
		{
			name: "options with dot separator",
			input: ` 1. Accept
 2. Reject`,
			expected: 2,
		},
		{
			name: "options with paren separator",
			input: ` 1) Accept
 2) Allow
 3) Reject`,
			expected: 3,
		},
		{
			name: "cursor arrow on option",
			input: `> 1. Yes
  2. Yes, allow always
  3. No`,
			expected: 3,
		},
		{
			name: "mixed content with numbers in text",
			input: `File has 42 lines
 > 1. Yes
   2. No
Some other text with number 99`,
			expected: 2,
		},
		{
			name: "unicode marker ❯",
			input: ` ❯ 1. Yes
   2. Yes, allow all edits during this session (shift+tab)
   3. No`,
			expected: 3,
		},
		{
			name: "unicode marker ➜",
			input: ` ➜ 1. Yes
   2. No`,
			expected: 2,
		},
		// JS parity: stale output before the current dialog
		{
			name: "stale output then current dialog (uses last block)",
			input: `Previous output
  1. Old yes
  2. Old no
  3. Old always

Current dialog:
  1. Yes
  2. No`,
			expected: 2, // last consecutive block starting from 1
		},
		{
			name: "non-sequential numbers ignored",
			input: `  1. Yes
  3. No`,
			expected: 1, // 3 is non-sequential (no 2), so block is just [1]
		},
		{
			name: "ANSI escape codes in options",
			input: "  \x1b[1m❯\x1b[0m 1. Yes\n   2. No",
			expected: 2,
		},
		{
			name: "real Claude Bash permission dialog (2-option)",
			input: ` Bash command

   for i in $(seq 1 10); do echo "line $i"; done && ls /tmp && ps aux | head -5 && echo "done"
   Long shell script with loop, ls, ps

 Command contains $() command substitution

 Do you want to proceed?
 ❯ 1. Yes
   2. No

 Esc to cancel · Tab to amend · ctrl+e to explain`,
			expected: 2,
		},
		{
			name: "4-option dialog",
			input: `❯ 1) first
  2) second
  3) third
  4) fourth`,
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tmuxadapter.DetectMaxOption(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
