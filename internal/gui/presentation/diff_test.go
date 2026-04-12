package presentation_test

import (
	"strings"
	"testing"

	"github.com/any-context/lazyclaude/internal/gui/presentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleDiff = `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -10,7 +10,7 @@ func main() {
 	fmt.Println("before")
-	fmt.Println("hello")
+	fmt.Println("hello, world")
 	fmt.Println("after")
`

// --- Benchmarks ---

func BenchmarkParseUnifiedDiff(b *testing.B) {
	for b.Loop() {
		presentation.ParseUnifiedDiff(sampleDiff)
	}
}

func BenchmarkFormatDiffLine(b *testing.B) {
	dl := presentation.DiffLine{Kind: presentation.DiffAdd, Content: "\tfmt.Println(\"hello, world\")", NewNum: 42}
	for b.Loop() {
		presentation.FormatDiffLine(dl, 4)
	}
}

func TestParseUnifiedDiff_Basic(t *testing.T) {
	t.Parallel()
	lines := presentation.ParseUnifiedDiff(sampleDiff)

	require.NotEmpty(t, lines)

	// Count by kind
	kinds := map[presentation.DiffLineKind]int{}
	for _, l := range lines {
		kinds[l.Kind]++
	}

	assert.Equal(t, 3, kinds[presentation.DiffHeader]) // diff --git, ---, +++
	assert.Equal(t, 1, kinds[presentation.DiffHunk])    // @@ ... @@
	assert.Equal(t, 1, kinds[presentation.DiffAdd])
	assert.Equal(t, 1, kinds[presentation.DiffDel])
	assert.GreaterOrEqual(t, kinds[presentation.DiffContext], 2) // before, after
}

func TestParseUnifiedDiff_LineNumbers(t *testing.T) {
	t.Parallel()
	lines := presentation.ParseUnifiedDiff(sampleDiff)

	// Find the add line
	var addLine presentation.DiffLine
	var delLine presentation.DiffLine
	for _, l := range lines {
		if l.Kind == presentation.DiffAdd {
			addLine = l
		}
		if l.Kind == presentation.DiffDel {
			delLine = l
		}
	}

	// Hunk starts at @@ -10,7 +10,7 @@
	// Context line "before" is old:10, new:10
	// Del line "hello" is old:11
	// Add line "hello, world" is new:11
	assert.Equal(t, 11, delLine.OldNum)
	assert.Equal(t, 11, addLine.NewNum)
}

func TestParseUnifiedDiff_Content(t *testing.T) {
	t.Parallel()
	lines := presentation.ParseUnifiedDiff(sampleDiff)

	var addLine, delLine presentation.DiffLine
	for _, l := range lines {
		if l.Kind == presentation.DiffAdd {
			addLine = l
		}
		if l.Kind == presentation.DiffDel {
			delLine = l
		}
	}

	// Content should NOT include the +/- prefix
	assert.Equal(t, "\tfmt.Println(\"hello, world\")", addLine.Content)
	assert.Equal(t, "\tfmt.Println(\"hello\")", delLine.Content)
}

func TestParseUnifiedDiff_Empty(t *testing.T) {
	t.Parallel()
	lines := presentation.ParseUnifiedDiff("")
	assert.Nil(t, lines)
}

func TestParseUnifiedDiff_MultipleHunks(t *testing.T) {
	t.Parallel()
	diff := `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,3 +1,3 @@
 line1
-old2
+new2
 line3
@@ -20,3 +20,4 @@
 line20
+inserted
 line21
 line22
`
	lines := presentation.ParseUnifiedDiff(diff)

	hunkCount := 0
	for _, l := range lines {
		if l.Kind == presentation.DiffHunk {
			hunkCount++
		}
	}
	assert.Equal(t, 2, hunkCount)

	// Second hunk should have correct line numbers
	var insertedLine presentation.DiffLine
	for _, l := range lines {
		if l.Kind == presentation.DiffAdd && l.Content == "inserted" {
			insertedLine = l
			break
		}
	}
	assert.Equal(t, 21, insertedLine.NewNum)
}

func TestFormatDiffLine_Add(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffAdd, Content: "new line", NewNum: 42}
	line := presentation.FormatDiffLine(dl, 4)

	assert.Contains(t, line, "42")
	assert.Contains(t, line, "+")
	assert.Contains(t, line, "new line")
}

func TestFormatDiffLine_Del(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffDel, Content: "old line", OldNum: 10}
	line := presentation.FormatDiffLine(dl, 4)

	assert.Contains(t, line, "10")
	assert.Contains(t, line, "-")
	assert.Contains(t, line, "old line")
}

func TestFormatDiffLine_Context(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffContext, Content: "unchanged", OldNum: 5, NewNum: 5}
	line := presentation.FormatDiffLine(dl, 4)

	// Should show both line numbers
	assert.Equal(t, 2, strings.Count(line, "5"))
	assert.Contains(t, line, "unchanged")
}

func TestFormatDiffLine_Header(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffHeader, Content: "diff --git a/main.go b/main.go"}
	line := presentation.FormatDiffLine(dl, 4)

	assert.Equal(t, "diff --git a/main.go b/main.go", line)
}

func TestFormatDiffLine_Hunk(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffHunk, Content: "@@ -10,7 +10,7 @@ func main() {"}
	line := presentation.FormatDiffLine(dl, 4)

	assert.Equal(t, "@@ -10,7 +10,7 @@ func main() {", line)
}

// --- FormatInlineDiffLine tests ---

func TestFormatInlineDiffLine_Add_WithLineNums(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffAdd, Content: "new line", NewNum: 42}
	// numWidth=2: "   42 │ + new line"
	assert.Equal(t, "   42 \u2502 + new line", presentation.FormatInlineDiffLine(dl, 2))
}

func TestFormatInlineDiffLine_Del_WithLineNums(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffDel, Content: "old line", OldNum: 10}
	// numWidth=2: "10    │ - old line"
	assert.Equal(t, "10    \u2502 - old line", presentation.FormatInlineDiffLine(dl, 2))
}

func TestFormatInlineDiffLine_Context_WithLineNums(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffContext, Content: "unchanged", OldNum: 5, NewNum: 5}
	// numWidth=2: " 5  5 │   unchanged"
	assert.Equal(t, " 5  5 \u2502   unchanged", presentation.FormatInlineDiffLine(dl, 2))
}

func TestFormatInlineDiffLine_Add_ZeroWidth(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffAdd, Content: "line", NewNum: 1}
	assert.Equal(t, "  + line", presentation.FormatInlineDiffLine(dl, 0))
}

func TestFormatInlineDiffLine_Del_ZeroWidth(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffDel, Content: "line", OldNum: 1}
	assert.Equal(t, "  - line", presentation.FormatInlineDiffLine(dl, 0))
}

func TestFormatInlineDiffLine_Context_ZeroWidth(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffContext, Content: "line", OldNum: 1, NewNum: 1}
	assert.Equal(t, "    line", presentation.FormatInlineDiffLine(dl, 0))
}

func TestFormatInlineDiffLine_Hunk_WithFuncContext(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffHunk, Content: "@@ -10,7 +10,7 @@ func main() {"}
	assert.Equal(t, "  @@ func main() {", presentation.FormatInlineDiffLine(dl, 3))
}

func TestFormatInlineDiffLine_Hunk_NoFuncContext(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffHunk, Content: "@@ -1,3 +1,3 @@"}
	assert.Equal(t, "  @@", presentation.FormatInlineDiffLine(dl, 2))
}

func TestFormatInlineDiffLine_FilePath(t *testing.T) {
	t.Parallel()
	dl := presentation.DiffLine{Kind: presentation.DiffFilePath, Content: "/tmp/myfile.go"}
	assert.Equal(t, "  File: /tmp/myfile.go", presentation.FormatInlineDiffLine(dl, 3))
}

// --- NumWidth / MaxLineNum tests ---

func TestNumWidth(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 1, presentation.NumWidth(0))
	assert.Equal(t, 1, presentation.NumWidth(9))
	assert.Equal(t, 2, presentation.NumWidth(10))
	assert.Equal(t, 2, presentation.NumWidth(99))
	assert.Equal(t, 3, presentation.NumWidth(100))
	assert.Equal(t, 4, presentation.NumWidth(1000))
}

func TestMaxLineNum(t *testing.T) {
	t.Parallel()
	lines := []presentation.DiffLine{
		{Kind: presentation.DiffContext, OldNum: 5, NewNum: 5},
		{Kind: presentation.DiffDel, OldNum: 42},
		{Kind: presentation.DiffAdd, NewNum: 100},
	}
	assert.Equal(t, 100, presentation.MaxLineNum(lines))
}

func TestMaxLineNum_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0, presentation.MaxLineNum(nil))
}

// --- ExtractHunkLabel tests ---

func TestExtractHunkLabel_WithFuncContext(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "@@ func (s *Server) Start()", presentation.ExtractHunkLabel("@@ -41,4 +41,6 @@ func (s *Server) Start()"))
}

func TestExtractHunkLabel_NoFuncContext(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "@@", presentation.ExtractHunkLabel("@@ -1,3 +1,3 @@"))
}

func TestExtractHunkLabel_EmptyTrailing(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "@@", presentation.ExtractHunkLabel("@@ -1 +1 @@  "))
}

func TestExtractHunkLabel_Malformed(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "@@", presentation.ExtractHunkLabel("not a hunk"))
}

// --- ParseUnifiedDiff: trailing empty line trimming ---

func TestParseUnifiedDiff_TrimsTrailingEmptyLines(t *testing.T) {
	t.Parallel()
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-old\n+new\n"
	lines := presentation.ParseUnifiedDiff(diff)
	// Last line should be the add line, not an empty context
	last := lines[len(lines)-1]
	assert.Equal(t, presentation.DiffAdd, last.Kind)
	assert.Equal(t, "new", last.Content)
}

// --- ParseUnifiedDiff: no-newline marker ---

func TestParseUnifiedDiff_SkipsNoNewlineMarker(t *testing.T) {
	t.Parallel()
	diff := "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-old\n+new\n\\ No newline at end of file\n"
	lines := presentation.ParseUnifiedDiff(diff)
	for _, l := range lines {
		assert.NotContains(t, l.Content, "No newline at end of file")
	}
}

// --- DiffFilePath kind ---

func TestDiffFilePath_KindValue(t *testing.T) {
	t.Parallel()
	// Ensure DiffFilePath is distinct from DiffHeader
	assert.NotEqual(t, presentation.DiffHeader, presentation.DiffFilePath)
}
