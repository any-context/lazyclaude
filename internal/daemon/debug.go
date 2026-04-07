package daemon

import (
	"fmt"
	"os"
	"time"
)

// debugLog appends a timestamped line to /tmp/lazyclaude-debug.log.
// Used for diagnosing remote connection issues. This is temporary
// debug instrumentation and should be removed after investigation.
func debugLog(format string, args ...any) {
	f, err := os.OpenFile("/tmp/lazyclaude-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}
