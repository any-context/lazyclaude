// mock-claude-client simulates Claude Code's MCP connection + hook behavior.
// It reads ~/.claude/ide/*.lock files to discover the MCP server,
// connects via WebSocket, sends initialize + ide_connected, and optionally
// triggers tool permission notifications (simulating PreToolUse + Notification hooks).
//
// Usage:
//
//	mock-claude-client                  # connect only
//	mock-claude-client --notify         # connect + send tool notification
//	mock-claude-client --tool Write     # connect + notify with custom tool name
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

func main() {
	notify := false
	toolName := "Bash"
	toolInput := `{"command":"echo hello"}`

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--notify":
			notify = true
		case "--tool":
			if i+1 < len(os.Args) {
				i++
				toolName = os.Args[i]
			}
		case "--input":
			if i+1 < len(os.Args) {
				i++
				toolInput = os.Args[i]
			}
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fatal("home dir: %v", err)
	}

	ideDir := filepath.Join(home, ".claude", "ide")
	entries, err := os.ReadDir(ideDir)
	if err != nil {
		fatal("read %s: %v", ideDir, err)
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(ideDir, e.Name()))
		if err != nil {
			continue
		}
		var lock struct {
			AuthToken string `json:"authToken"`
		}
		if json.Unmarshal(data, &lock) != nil || lock.AuthToken == "" {
			continue
		}

		port := strings.TrimSuffix(e.Name(), ".lock")
		if err := run(port, lock.AuthToken, notify, toolName, toolInput); err != nil {
			fatal("port %s: %v", port, err)
		}
		os.Exit(0)
	}

	fatal("no valid lock files in %s", ideDir)
}

func run(port, token string, notify bool, toolName, toolInput string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("ws://127.0.0.1:%s/", port)
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"X-Claude-Code-Ide-Authorization": []string{token},
		},
	})
	if err != nil {
		return fmt.Errorf("dial %s: %w", url, err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// 1) initialize
	initReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{"capabilities": map[string]any{}},
	})
	if err := conn.Write(ctx, websocket.MessageText, initReq); err != nil {
		return fmt.Errorf("write initialize: %w", err)
	}
	_, respData, err := conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("read initialize response: %w", err)
	}
	fmt.Printf("initialize: %s\n", string(respData))

	// 2) ide_connected
	pid := os.Getpid()
	ideReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "ide_connected",
		"params":  map[string]any{"pid": pid},
	})
	if err := conn.Write(ctx, websocket.MessageText, ideReq); err != nil {
		return fmt.Errorf("write ide_connected: %w", err)
	}
	fmt.Printf("MOCK_CLAUDE_CONNECTED port=%s pid=%d\n", port, pid)

	// 3) Optionally trigger tool notification (simulates hooks)
	if notify {
		// Wait for ide_connected to be processed by the server.
		// ide_connected is async (WebSocket notification), /notify is HTTP.
		// Without this delay, the HTTP request may arrive before the
		// WebSocket handler registers the PID→window mapping.
		time.Sleep(500 * time.Millisecond)

		base := fmt.Sprintf("http://127.0.0.1:%s", port)
		if err := sendNotify(base, token, pid, toolName, toolInput); err != nil {
			return fmt.Errorf("notify: %w", err)
		}
		fmt.Printf("MOCK_NOTIFY_SENT tool=%s\n", toolName)
	}

	return nil
}

// sendNotify simulates Claude Code's PreToolUse + Notification hooks.
// Phase 1: POST /notify type=tool_info (stores tool info)
// Phase 2: POST /notify type="" (triggers popup)
func sendNotify(base, token string, pid int, toolName, toolInput string) error {
	// Phase 1: tool_info
	body1, _ := json.Marshal(map[string]any{
		"type":       "tool_info",
		"pid":        pid,
		"tool_name":  toolName,
		"tool_input": json.RawMessage(toolInput),
		"cwd":        "/home/user/project",
	})
	resp1, err := doPost(base+"/notify", token, body1)
	if err != nil {
		return fmt.Errorf("phase1 tool_info: %w", err)
	}
	resp1.Body.Close()
	fmt.Printf("NOTIFY_PHASE1 status=%d\n", resp1.StatusCode)
	if resp1.StatusCode != http.StatusOK {
		return fmt.Errorf("phase1 tool_info: HTTP %d", resp1.StatusCode)
	}

	// Small delay between phases (realistic hook timing)
	time.Sleep(50 * time.Millisecond)

	// Phase 2: permission_prompt
	body2, _ := json.Marshal(map[string]any{
		"pid":     pid,
		"message": fmt.Sprintf("Allow %s?", toolName),
	})
	resp2, err := doPost(base+"/notify", token, body2)
	if err != nil {
		return fmt.Errorf("phase2 permission: %w", err)
	}
	resp2.Body.Close()
	fmt.Printf("NOTIFY_PHASE2 status=%d\n", resp2.StatusCode)
	if resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("phase2 permission: HTTP %d", resp2.StatusCode)
	}

	return nil
}

func doPost(url, token string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Claude-Code-Ide-Authorization", token)
	return http.DefaultClient.Do(req)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mock-claude-client: "+format+"\n", args...)
	os.Exit(1)
}
