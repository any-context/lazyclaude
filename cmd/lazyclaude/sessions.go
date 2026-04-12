package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/server"
	"github.com/spf13/cobra"
)

func newSessionsCmd() *cobra.Command {
	var (
		jsonOutput bool
		verbose    bool
	)

	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List active sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := config.DefaultPaths()
			disc, err := server.DiscoverServer(paths.IDEDir)
			if err != nil {
				return fmt.Errorf("discover server: %w", err)
			}

			client := server.NewClient(disc.Port, disc.Token)
			sessions, err := client.Sessions(cmd.Context())
			if err != nil {
				return fmt.Errorf("fetch sessions: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(sessions)
			}

			return printSessionsTable(cmd.OutOrStdout(), sessions, verbose)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output raw JSON")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show window column")

	cmd.AddCommand(newSessionsResumeCmd())

	return cmd
}

func newSessionsResumeCmd() *cobra.Command {
	var (
		name   string
		prompt string
	)

	cmd := &cobra.Command{
		Use:   "resume <session-id>",
		Short: "Resume a session by ID (supports GC'd sessions via --name)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]

			paths := config.DefaultPaths()
			disc, err := server.DiscoverServer(paths.IDEDir)
			if err != nil {
				return fmt.Errorf("discover server: %w", err)
			}

			client := server.NewClient(disc.Port, disc.Token)
			result, err := client.ResumeSession(cmd.Context(), sessionID, prompt, name)
			if err != nil {
				return fmt.Errorf("resume session: %w", err)
			}

			if result.Session != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Resumed session %s (id=%s, role=%s, path=%s)\n",
					result.Session.Name, result.Session.ID, result.Session.Role, result.Session.Path)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Session resumed (status=%s)\n", result.Status)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "worktree name (required for GC'd sessions)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "initial prompt for the resumed session")

	return cmd
}

// printSessionsTable prints sessions in aligned table format.
func printSessionsTable(out io.Writer, sessions []server.SessionInfo, verbose bool) error {
	if len(sessions) == 0 {
		fmt.Fprintln(out, "No sessions found.")
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)

	if verbose {
		fmt.Fprintln(w, "ID\tNAME\tROLE\tSTATUS\tACTIVITY\tHOST\tWINDOW\tPATH")
		for _, s := range sessions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				s.ID, s.Name, s.Role, s.Status, s.Activity, s.Host, s.Window, s.Path)
		}
	} else {
		fmt.Fprintln(w, "ID\tNAME\tROLE\tSTATUS\tACTIVITY\tHOST\tPATH")
		for _, s := range sessions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				s.ID, s.Name, s.Role, s.Status, s.Activity, s.Host, s.Path)
		}
	}

	return w.Flush()
}
