package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/KEMSHlM/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/KEMSHlM/lazyclaude/internal/core/choice"
	"github.com/KEMSHlM/lazyclaude/internal/core/config"
	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
	"github.com/jesseduffield/gocui"
	"github.com/spf13/cobra"
)

func newToolCmd() *cobra.Command {
	var window string
	var sendKeys bool

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

			return runToolPopup(window, toolName, toolInput, toolCWD, sendKeys)
		},
	}

	cmd.Flags().StringVar(&window, "window", "", "tmux window name")
	cmd.Flags().BoolVar(&sendKeys, "send-keys", false, "send choice key to Claude pane on exit")

	return cmd
}

func runToolPopup(window, toolName, toolInput, toolCWD string, sendKeys bool) error {
	td := presentation.ParseToolInput(toolName, toolInput, toolCWD)
	bodyLines := presentation.FormatToolLines(td)

	// Detect dialog option count from Claude's pane (before rendering our popup)
	maxOption := 3
	if window != "" {
		client := tmux.NewExecClient()
		target := window
		if !strings.Contains(window, ":") {
			target = "lazyclaude:" + window
		}
		if content, err := client.CapturePaneANSI(context.Background(), target); err == nil {
			maxOption = tmuxadapter.DetectMaxOption(content)
		}
	}

	g, err := gocui.NewGui(gocui.NewGuiOpts{OutputMode: gocui.OutputTrue})
	if err != nil {
		return fmt.Errorf("init gocui: %w", err)
	}
	defer g.Close()

	choiceVal := choice.Cancel

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
		if maxOption <= 2 {
			fmt.Fprint(v2, " y: yes  n: no  Esc: cancel")
		} else {
			fmt.Fprint(v2, " y: yes  a: allow always  n: no  Esc: cancel")
		}

		g.SetCurrentView("content")
		return nil
	})

	makeChoice := func(c choice.Choice) func(*gocui.Gui, *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			choiceVal = c
			return gocui.ErrQuit
		}
	}

	bind := func(key interface{}, handler func(*gocui.Gui, *gocui.View) error) error {
		switch k := key.(type) {
		case rune:
			return g.SetKeybinding("", k, gocui.ModNone, handler)
		case gocui.Key:
			return g.SetKeybinding("", k, gocui.ModNone, handler)
		}
		return nil
	}

	for _, b := range []struct {
		key     interface{}
		handler func(*gocui.Gui, *gocui.View) error
		cond    bool
	}{
		{'y', makeChoice(choice.Accept), true},
		{'a', makeChoice(choice.Allow), maxOption > 2},
		{'n', makeChoice(choice.Reject), true},
		{gocui.KeyEsc, makeChoice(choice.Cancel), true},
		{gocui.KeyCtrlC, makeChoice(choice.Cancel), true},
	} {
		if b.cond {
			if err := bind(b.key, b.handler); err != nil {
				return fmt.Errorf("keybinding: %w", err)
			}
		}
	}

	if err := g.MainLoop(); err != nil && !strings.Contains(err.Error(), "quit") {
		return err
	}

	if window != "" {
		paths := config.DefaultPaths()
		if err := os.MkdirAll(paths.RuntimeDir, 0o700); err != nil {
			return fmt.Errorf("create runtime dir: %w", err)
		}
		if err := choice.WriteFile(paths, window, choiceVal); err != nil {
			return fmt.Errorf("write choice: %w", err)
		}
	}

	// Send the choice key directly to Claude Code's pane
	if sendKeys && window != "" && choiceVal != choice.Cancel {
		var client tmux.Client
		if s := os.Getenv("LAZYCLAUDE_TMUX_SOCKET"); s != "" {
			client = tmux.NewExecClientWithSocket(s)
		} else {
			client = tmux.NewExecClient()
		}
		if err := tmuxadapter.SendToPane(context.Background(), client, window, choiceVal); err != nil {
			fmt.Fprintf(os.Stderr, "send-keys: %v\n", err)
		}
	}

	return nil
}
