package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ToolNotification represents a pending tool permission request from Claude Code.
type ToolNotification struct {
	ToolName    string    `json:"tool_name"`
	Input       string    `json:"input"`
	CWD         string    `json:"cwd,omitempty"`
	Window      string    `json:"window"`
	Timestamp   time.Time `json:"timestamp"`
	OldFilePath string    `json:"old_file_path,omitempty"` // set for Edit/Write diff
	NewContents string    `json:"new_contents,omitempty"`  // set for Edit/Write diff
}

// IsDiff returns true if this notification contains diff information.
func (n *ToolNotification) IsDiff() bool {
	return n.OldFilePath != ""
}

const queuePrefix = "lazyclaude-q-"

// Enqueue writes a notification as a timestamped file, preserving all pending notifications.
// File name includes nanosecond timestamp for strict ordering.
func Enqueue(runtimeDir string, n ToolNotification) error {
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	data, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	name := fmt.Sprintf("%s%020d.json", queuePrefix, time.Now().UnixNano())
	path := filepath.Join(runtimeDir, name)
	return os.WriteFile(path, data, 0o600)
}

// ReadAll reads and removes all queued notifications, returning them in creation order.
func ReadAll(runtimeDir string) ([]*ToolNotification, error) {
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read queue dir: %w", err)
	}

	// Collect queue files
	var files []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), queuePrefix) && strings.HasSuffix(e.Name(), ".json") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Sort by name (CreateTemp includes timestamp, so lexicographic = creation order)
	sort.Strings(files)

	var result []*ToolNotification
	for _, name := range files {
		path := filepath.Join(runtimeDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // file may have been claimed by another reader
		}
		_ = os.Remove(path) // best-effort; another reader may have already removed it

		var n ToolNotification
		if err := json.Unmarshal(data, &n); err != nil {
			continue
		}
		result = append(result, &n)
	}
	return result, nil
}
