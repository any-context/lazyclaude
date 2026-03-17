package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	var window, oldFile, newFile string

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show diff popup viewer",
		RunE: func(cmd *cobra.Command, args []string) error {
			if oldFile == "" || newFile == "" {
				return fmt.Errorf("--old and --new are required")
			}
			return runDiffPopup(window, oldFile, newFile)
		},
	}

	cmd.Flags().StringVar(&window, "window", "", "tmux window name")
	cmd.Flags().StringVar(&oldFile, "old", "", "old file path")
	cmd.Flags().StringVar(&newFile, "new", "", "new file contents path")

	return cmd
}

func runDiffPopup(window, oldFile, newFile string) error {
	// Generate diff using git diff
	diffOutput, err := generateDiff(oldFile, newFile)
	if err != nil {
		return fmt.Errorf("generate diff: %w", err)
	}

	lines := presentation.ParseUnifiedDiff(diffOutput)
	formattedLines := make([]string, len(lines))
	for i, dl := range lines {
		formattedLines[i] = presentation.FormatDiffLine(dl, 4)
	}

	// Create gocui app in popup mode
	g, err := gocui.NewGui(gocui.NewGuiOpts{OutputMode: gocui.OutputTrue})
	if err != nil {
		return fmt.Errorf("init gocui: %w", err)
	}
	defer g.Close()

	scrollY := 0
	choice := gui.ChoiceCancel

	g.SetManagerFunc(func(g *gocui.Gui) error {
		maxX, maxY := g.Size()

		// Title from file path
		title := fmt.Sprintf(" Diff: %s ", oldFile)
		if len(title) > maxX-4 {
			title = title[:maxX-7] + "... "
		}

		// Content view
		v, err := g.SetView("content", 0, 0, maxX-1, maxY-3, 0)
		if err != nil && !isUnknownViewErr(err) {
			return err
		}
		v.Title = title
		v.Clear()

		visibleLines := maxY - 5
		start := scrollY
		end := start + visibleLines
		if end > len(formattedLines) {
			end = len(formattedLines)
		}
		if start < 0 {
			start = 0
		}

		for i := start; i < end; i++ {
			dl := lines[i]
			line := formattedLines[i]
			switch dl.Kind {
			case presentation.DiffAdd:
				fmt.Fprintf(v, "\x1b[32m%s\x1b[0m\n", line)
			case presentation.DiffDel:
				fmt.Fprintf(v, "\x1b[31m%s\x1b[0m\n", line)
			case presentation.DiffHunk:
				fmt.Fprintf(v, "\x1b[36m%s\x1b[0m\n", line)
			case presentation.DiffHeader:
				fmt.Fprintf(v, "\x1b[1m%s\x1b[0m\n", line)
			default:
				fmt.Fprintln(v, line)
			}
		}

		// Action bar
		v2, err := g.SetView("actions", 0, maxY-2, maxX-1, maxY, 0)
		if err != nil && !isUnknownViewErr(err) {
			return err
		}
		v2.Frame = false
		v2.Clear()
		fmt.Fprint(v2, " y: yes  a: allow always  n: no  Esc: cancel")

		return nil
	})

	makeChoice := func(c gui.Choice) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			choice = c
			return gocui.ErrQuit
		}
	}

	// Keybindings
	g.SetKeybinding("", 'y', gocui.ModNone, makeChoice(gui.ChoiceAccept))
	g.SetKeybinding("", 'a', gocui.ModNone, makeChoice(gui.ChoiceAllow))
	g.SetKeybinding("", 'n', gocui.ModNone, makeChoice(gui.ChoiceReject))
	g.SetKeybinding("", gocui.KeyEsc, gocui.ModNone, makeChoice(gui.ChoiceCancel))
	g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, makeChoice(gui.ChoiceCancel))

	// Scroll
	g.SetKeybinding("", 'j', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if scrollY < len(formattedLines)-1 {
			scrollY++
		}
		return nil
	})
	g.SetKeybinding("", 'k', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if scrollY > 0 {
			scrollY--
		}
		return nil
	})
	g.SetKeybinding("", 'd', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		_, maxY := g.Size()
		scrollY += (maxY - 5) / 2
		if scrollY > len(formattedLines)-1 {
			scrollY = len(formattedLines) - 1
		}
		return nil
	})
	g.SetKeybinding("", 'u', gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		_, maxY := g.Size()
		scrollY -= (maxY - 5) / 2
		if scrollY < 0 {
			scrollY = 0
		}
		return nil
	})

	if err := g.MainLoop(); err != nil && !strings.Contains(err.Error(), "quit") {
		return err
	}

	// Write choice file
	if window != "" {
		paths := config.DefaultPaths()
		os.MkdirAll(paths.RuntimeDir, 0o755)
		if err := gui.WriteChoiceFile(paths, window, choice); err != nil {
			return fmt.Errorf("write choice: %w", err)
		}
	}

	return nil
}

func generateDiff(oldFile, newFile string) (string, error) {
	cmd := exec.Command("git", "diff", "--no-index", "--unified=3", "--", oldFile, newFile)
	out, err := cmd.Output()
	if err != nil {
		// git diff returns exit code 1 when files differ (not an error)
		if len(out) > 0 {
			return string(out), nil
		}
		return "", err
	}
	return string(out), nil
}

func isUnknownViewErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown view")
}
