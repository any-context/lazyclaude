package main

import (
	"fmt"
	"strings"

	"github.com/any-context/lazyclaude/internal/core/config"
	"github.com/any-context/lazyclaude/internal/server"
	"github.com/spf13/cobra"
)

func newMsgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "msg",
		Short: "Send messages between sessions",
	}

	cmd.AddCommand(newMsgSendCmd())
	return cmd
}

func newMsgSendCmd() *cobra.Command {
	var (
		msgType string
		from    string
	)

	cmd := &cobra.Command{
		Use:   "send <session-id> <message>",
		Short: "Send a message to a session by ID",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetID := args[0]
			body := strings.Join(args[1:], " ")

			paths := config.DefaultPaths()
			disc, err := server.DiscoverServer(paths.IDEDir)
			if err != nil {
				return fmt.Errorf("discover server: %w", err)
			}

			client := server.NewClient(disc.Port, disc.Token)

			if err := client.SendMessage(from, targetID, msgType, body); err != nil {
				return fmt.Errorf("send message: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Message sent to %s\n", targetID)
			return nil
		},
	}

	cmd.Flags().StringVar(&msgType, "type", "status", "message type (review_request, review_response, status, done)")
	cmd.Flags().StringVar(&from, "from", "cli", "sender session ID")

	return cmd
}
