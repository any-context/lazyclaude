package presentation

import (
	"fmt"
	"strings"
)

// DiffLineKind classifies a line in a unified diff.
type DiffLineKind int

const (
	DiffContext  DiffLineKind = iota // unchanged line
	DiffAdd                         // added line
	DiffDel                         // deleted line
	DiffHunk                        // hunk header (@@ ... @@)
	DiffHeader                      // diff header (diff --git, ---, +++)
	DiffFilePath                    // file path line (rendered dim)
)

// DiffLine represents a single line in a parsed diff.
type DiffLine struct {
	Kind    DiffLineKind
	Content string   // raw content (without +/- prefix)
	OldNum  int      // line number in old file (0 if not applicable)
	NewNum  int      // line number in new file (0 if not applicable)
}

// ParseUnifiedDiff parses git unified diff output into structured lines.
func ParseUnifiedDiff(raw string) []DiffLine {
	if raw == "" {
		return nil
	}

	rawLines := strings.Split(raw, "\n")
	// Trim trailing empty segments produced by strings.Split on trailing newline.
	for len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}

	var lines []DiffLine
	var oldNum, newNum int

	for _, line := range rawLines {
		// Skip "no newline at end of file" marker.
		if strings.HasPrefix(line, `\ `) {
			continue
		}

		switch {
		case strings.HasPrefix(line, "diff --git"):
			lines = append(lines, DiffLine{Kind: DiffHeader, Content: line})

		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			lines = append(lines, DiffLine{Kind: DiffHeader, Content: line})

		case strings.HasPrefix(line, "@@"):
			old, new_ := parseHunkHeader(line)
			oldNum = old
			newNum = new_
			lines = append(lines, DiffLine{Kind: DiffHunk, Content: line})

		case strings.HasPrefix(line, "+"):
			lines = append(lines, DiffLine{
				Kind:    DiffAdd,
				Content: line[1:],
				NewNum:  newNum,
			})
			newNum++

		case strings.HasPrefix(line, "-"):
			lines = append(lines, DiffLine{
				Kind:    DiffDel,
				Content: line[1:],
				OldNum:  oldNum,
			})
			oldNum++

		default:
			// Context line (starts with space) or empty
			content := line
			if len(line) > 0 && line[0] == ' ' {
				content = line[1:]
			}
			lines = append(lines, DiffLine{
				Kind:    DiffContext,
				Content: content,
				OldNum:  oldNum,
				NewNum:  newNum,
			})
			oldNum++
			newNum++
		}
	}

	return lines
}

// parseHunkHeader extracts starting line numbers from "@@ -old,len +new,len @@"
func parseHunkHeader(line string) (oldStart, newStart int) {
	// Find the range part between @@ markers
	parts := strings.SplitN(line, "@@", 3)
	if len(parts) < 2 {
		return 1, 1
	}
	rangePart := strings.TrimSpace(parts[1])

	// Parse "-old,len +new,len"
	for _, token := range strings.Fields(rangePart) {
		if strings.HasPrefix(token, "-") {
			fmt.Sscanf(token, "-%d", &oldStart)
		} else if strings.HasPrefix(token, "+") {
			fmt.Sscanf(token, "+%d", &newStart)
		}
	}
	if oldStart == 0 {
		oldStart = 1
	}
	if newStart == 0 {
		newStart = 1
	}
	return
}

// FormatDiffLine renders a diff line for display with line numbers.
func FormatDiffLine(dl DiffLine, numWidth int) string {
	switch dl.Kind {
	case DiffHeader:
		return dl.Content
	case DiffHunk:
		return dl.Content
	case DiffAdd:
		return fmt.Sprintf("%*s %*d + %s", numWidth, "", numWidth, dl.NewNum, dl.Content)
	case DiffDel:
		return fmt.Sprintf("%*d %*s - %s", numWidth, dl.OldNum, numWidth, "", dl.Content)
	case DiffContext:
		old := ""
		new_ := ""
		if dl.OldNum > 0 {
			old = fmt.Sprintf("%*d", numWidth, dl.OldNum)
		} else {
			old = strings.Repeat(" ", numWidth)
		}
		if dl.NewNum > 0 {
			new_ = fmt.Sprintf("%*d", numWidth, dl.NewNum)
		} else {
			new_ = strings.Repeat(" ", numWidth)
		}
		return fmt.Sprintf("%s %s   %s", old, new_, dl.Content)
	default:
		return dl.Content
	}
}

// FormatInlineDiffLine renders a diff line in clean inline format
// without line numbers, using - / + / (space) prefix style.
func FormatInlineDiffLine(dl DiffLine) string {
	switch dl.Kind {
	case DiffFilePath:
		return "File: " + dl.Content
	case DiffHunk:
		return dl.Content
	case DiffAdd:
		return "+ " + dl.Content
	case DiffDel:
		return "- " + dl.Content
	case DiffContext:
		return "  " + dl.Content
	default:
		return dl.Content
	}
}
