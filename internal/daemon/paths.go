package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

// DaemonInfoDir returns the directory for daemon runtime files.
// Uses /tmp/lazyclaude-$USER to avoid collisions between users.
func DaemonInfoDir() string {
	if u := os.Getenv("USER"); u != "" {
		return fmt.Sprintf("/tmp/lazyclaude-%s", u)
	}
	return "/tmp/lazyclaude"
}

// DaemonInfoPath returns the full path to the daemon.json file.
func DaemonInfoPath() string {
	return filepath.Join(DaemonInfoDir(), "daemon.json")
}
