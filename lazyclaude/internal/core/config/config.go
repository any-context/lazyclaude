package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Paths holds all filesystem paths used by lazyclaude.
// All paths are configurable to enable test isolation.
type Paths struct {
	IDEDir     string // lock files: <IDEDir>/<port>.lock
	DataDir    string // state file: <DataDir>/state.json
	RuntimeDir string // choice files, port file
}

// DefaultPaths returns production paths.
// Environment variables override defaults:
//   - LAZYCLAUDE_DATA_DIR    → DataDir
//   - LAZYCLAUDE_RUNTIME_DIR → RuntimeDir
//   - LAZYCLAUDE_IDE_DIR     → IDEDir
//
// Panics if home directory cannot be determined.
func DefaultPaths() Paths {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("cannot determine home directory: %v", err))
	}
	p := Paths{
		IDEDir:     filepath.Join(home, ".claude", "ide"),
		DataDir:    filepath.Join(home, ".local", "share", "lazyclaude"),
		RuntimeDir: os.TempDir(),
	}
	if v := os.Getenv("LAZYCLAUDE_DATA_DIR"); v != "" {
		p.DataDir = v
	}
	if v := os.Getenv("LAZYCLAUDE_RUNTIME_DIR"); v != "" {
		p.RuntimeDir = v
	}
	if v := os.Getenv("LAZYCLAUDE_IDE_DIR"); v != "" {
		p.IDEDir = v
	}
	return p
}

// PopupMode controls how tool permission popups are displayed.
type PopupMode int

const (
	PopupModeAuto    PopupMode = iota // display-popup if tmux available, overlay fallback
	PopupModeTmux                     // always display-popup
	PopupModeOverlay                  // always gocui overlay (P5 behavior)
)

// ParsePopupMode parses a string into a PopupMode. Defaults to Auto.
func ParsePopupMode(s string) PopupMode {
	switch strings.ToLower(s) {
	case "tmux":
		return PopupModeTmux
	case "overlay":
		return PopupModeOverlay
	default:
		return PopupModeAuto
	}
}

// TestPaths returns isolated paths under a temporary directory.
// Use this in tests to avoid affecting production state.
func TestPaths(tmpDir string) Paths {
	return Paths{
		IDEDir:     filepath.Join(tmpDir, "ide"),
		DataDir:    filepath.Join(tmpDir, "data"),
		RuntimeDir: filepath.Join(tmpDir, "run"),
	}
}

// StateFile returns the path to the session state file.
func (p Paths) StateFile() string {
	return filepath.Join(p.DataDir, "state.json")
}

// PortFile returns the path to the MCP server port file.
func (p Paths) PortFile() string {
	return filepath.Join(p.RuntimeDir, "lazyclaude-mcp.port")
}

// ChoiceFile returns the path to a choice file for a window.
func (p Paths) ChoiceFile(window string) string {
	return filepath.Join(p.RuntimeDir, "lazyclaude-choice-"+window+".txt")
}

// LockFile returns the path to an IDE lock file for a port.
func (p Paths) LockFile(port int) string {
	return filepath.Join(p.IDEDir, fmt.Sprintf("%d.lock", port))
}
