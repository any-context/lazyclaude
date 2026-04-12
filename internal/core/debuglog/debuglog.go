// Package debuglog provides a shared debug logging utility for lazyclaude.
// Logging is enabled by setting the LAZYCLAUDE_DEBUG environment variable to a non-empty value.
package debuglog

import (
	"fmt"
	"os"
	"time"
)

// Enabled reports whether debug logging is active.
var Enabled = os.Getenv("LAZYCLAUDE_DEBUG") != ""

// Log appends a timestamped line to /tmp/lazyclaude-debug.log.
// It is a no-op when Enabled is false.
func Log(format string, args ...any) {
	if !Enabled {
		return
	}
	f, err := os.OpenFile("/tmp/lazyclaude-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}
