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

	"github.com/KEMSHlM/lazyclaude/internal/core/model"
)

const queuePrefix = "lazyclaude-q-"

// Enqueue writes a notification as a timestamped file, preserving all pending notifications.
// File name includes nanosecond timestamp for strict ordering.
func Enqueue(runtimeDir string, n model.ToolNotification) error {
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
func ReadAll(runtimeDir string) ([]*model.ToolNotification, error) {
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

	const maxAge = 30 * time.Second

	var result []*model.ToolNotification
	for _, name := range files {
		path := filepath.Join(runtimeDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // file may have been claimed by another reader
		}
		_ = os.Remove(path) // best-effort; another reader may have already removed it

		var n model.ToolNotification
		if err := json.Unmarshal(data, &n); err != nil {
			continue
		}
		// Skip stale notifications (Claude Code already moved on)
		if !n.Timestamp.IsZero() && time.Since(n.Timestamp) > maxAge {
			continue
		}
		result = append(result, &n)
	}
	return result, nil
}

