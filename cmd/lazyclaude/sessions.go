package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
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
			sessions, err := client.Sessions()
			if err != nil {
				return fmt.Errorf("fetch sessions: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(sessions)
			}

			return printSessionsTable(os.Stdout, sessions, verbose)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output raw JSON")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show ID, window, and absolute path")

	return cmd
}

// printSessionsTable prints sessions in aligned table format.
func printSessionsTable(out io.Writer, sessions []server.SessionInfo, verbose bool) error {
	if len(sessions) == 0 {
		fmt.Fprintln(out, "No sessions found.")
		return nil
	}

	// Detect common project root for relative path display.
	projectRoot := detectProjectRoot(sessions)

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)

	if verbose {
		fmt.Fprintln(w, "ID\tNAME\tROLE\tSTATUS\tWINDOW\tPATH")
		for _, s := range sessions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				s.ID, s.Name, s.Role, s.Status, s.Window, s.Path)
		}
	} else {
		fmt.Fprintln(w, "ID\tNAME\tROLE\tSTATUS\tPATH")
		for _, s := range sessions {
			relPath := relativePath(s.Path, projectRoot)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				s.ID, s.Name, s.Role, s.Status, relPath)
		}
	}

	return w.Flush()
}

// detectProjectRoot finds the common path prefix among sessions.
// Returns the deepest common directory, or empty string if no common root.
func detectProjectRoot(sessions []server.SessionInfo) string {
	if len(sessions) == 0 {
		return ""
	}
	paths := make([]string, 0, len(sessions))
	for _, s := range sessions {
		if s.Path != "" {
			paths = append(paths, s.Path)
		}
	}
	if len(paths) == 0 {
		return ""
	}
	root := paths[0]
	for _, p := range paths[1:] {
		root = commonPrefix(root, p)
		if root == "" {
			return ""
		}
	}
	return root
}

// commonPrefix returns the longest common prefix of two paths at directory boundaries.
func commonPrefix(a, b string) string {
	partsA := strings.Split(a, "/")
	partsB := strings.Split(b, "/")
	n := len(partsA)
	if len(partsB) < n {
		n = len(partsB)
	}
	common := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if partsA[i] != partsB[i] {
			break
		}
		common = append(common, partsA[i])
	}
	if len(common) == 0 {
		return ""
	}
	return strings.Join(common, "/")
}

// relativePath returns the path relative to root, or the absolute path if
// the path is not under root.
func relativePath(path, root string) string {
	if root == "" {
		return path
	}
	if path == root {
		return "."
	}
	// Ensure match is at a directory boundary (root + "/").
	prefix := root + "/"
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	return strings.TrimPrefix(path, prefix)
}
