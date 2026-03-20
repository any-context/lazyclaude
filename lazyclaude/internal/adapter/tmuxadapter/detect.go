package tmuxadapter

import (
	"regexp"
	"strconv"
	"strings"
)

// optionPattern matches Claude Code's numbered dialog options.
// Handles formats: "1. Yes", "1) Yes", "> 1. Yes", "❯ 1. Yes", "➜ 1. Yes"
// Also strips ANSI escape sequences before matching.
var optionPattern = regexp.MustCompile(`^\s*(?:[>❯➜]\s+)?(\d+)[.)]\s+(.+)`)

// ansiEscape matches ANSI escape sequences for stripping.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

const defaultMaxOption = 3

// DetectMaxOption scans tmux capture-pane output for the last consecutive
// block of numbered dialog options starting from 1.
//
// This matches the JS captureDialogOptions logic:
// - Collects consecutive numbered options (1, 2, 3...)
// - Uses the LAST such block (ignores stale output from previous dialogs)
// - Non-sequential numbers break the block
// - Returns the count of options in the last block, or 3 as default.
func DetectMaxOption(paneContent string) int {
	var lastBlock []int
	var current []int

	for _, line := range strings.Split(paneContent, "\n") {
		// Strip ANSI escape sequences
		clean := ansiEscape.ReplaceAllString(line, "")

		matches := optionPattern.FindStringSubmatch(clean)
		if matches == nil {
			// Non-matching line: save current block if non-empty
			if len(current) > 0 {
				lastBlock = current
				current = nil
			}
			continue
		}

		n, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}

		if n == 1 {
			// Start a new block
			current = []int{n}
		} else if len(current) > 0 && n == len(current)+1 {
			// Sequential continuation
			current = append(current, n)
		}
		// else: non-sequential number — ignored
	}
	// Don't forget the last block if file ends without a non-matching line
	if len(current) > 0 {
		lastBlock = current
	}

	if len(lastBlock) == 0 {
		return defaultMaxOption
	}
	return len(lastBlock)
}
