package daemon

import (
	"fmt"
	"os"
)

// DaemonInfoDir returns the directory for daemon runtime files.
// Uses /tmp/lazyclaude-$USER to avoid collisions between users.
func DaemonInfoDir() string {
	if u := os.Getenv("USER"); u != "" {
		return fmt.Sprintf("/tmp/lazyclaude-%s", u)
	}
	return "/tmp/lazyclaude"
}

