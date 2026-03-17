package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/gui"
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
	"github.com/spf13/cobra"
)

func newToolCmd() *cobra.Command {
	var window string

	cmd := &cobra.Command{
		Use:   "tool",
		Short: "Show tool confirmation popup",
		RunE: func(cmd *cobra.Command, args []string) error {
			toolName := os.Getenv("TOOL_NAME")
			toolInput := os.Getenv("TOOL_INPUT")
			toolCWD := os.Getenv("TOOL_CWD")

			if toolName == "" {
				toolName = "Unknown"
			}
			if toolInput == "" {
				toolInput = "{}"
			}

			return runToolPopup(window, toolName, toolInput, toolCWD)
		},
	}

	cmd.Flags().StringVar(&window, "window", "", "tmux window name")

	return cmd
}

func runToolPopup(window, toolName, toolInput, toolCWD string) error {
	td := presentation.ParseToolInput(toolName, toolInput, toolCWD)
	bodyLines := presentation.FormatToolLines(td)

	g, err := gocui.NewGui(gocui.NewGuiOpts{OutputMode: gocui.OutputTrue})
	if err != nil {
		return fmt.Errorf("init gocui: %w", err)
	}
	defer g.Close()

	choice := gui.ChoiceCancel

	g.SetManagerFunc(func(g *gocui.Gui) error {
		maxX, maxY := g.Size()

		title := fmt.Sprintf(" %s ", toolName)

		v, err := g.SetView("content", 0, 0, maxX-1, maxY-3, 0)
		if err != nil && !isUnknownViewErr(err) {
			return err
		}
		v.Title = title
		v.Clear()
		for _, line := range bodyLines {
			fmt.Fprintln(v, line)
		}

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

	g.SetKeybinding("", 'y', gocui.ModNone, makeChoice(gui.ChoiceAccept))
	g.SetKeybinding("", 'a', gocui.ModNone, makeChoice(gui.ChoiceAllow))
	g.SetKeybinding("", 'n', gocui.ModNone, makeChoice(gui.ChoiceReject))
	g.SetKeybinding("", gocui.KeyEsc, gocui.ModNone, makeChoice(gui.ChoiceCancel))
	g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, makeChoice(gui.ChoiceCancel))

	if err := g.MainLoop(); err != nil && !strings.Contains(err.Error(), "quit") {
		return err
	}

	if window != "" {
		paths := config.DefaultPaths()
		os.MkdirAll(paths.RuntimeDir, 0o755)
		if err := gui.WriteChoiceFile(paths, window, choice); err != nil {
			return fmt.Errorf("write choice: %w", err)
		}
	}

	return nil
}
