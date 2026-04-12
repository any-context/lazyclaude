package presentation

import "strings"

// LogLevel classifies the severity of a log line.
type LogLevel int

const (
	LogLevelInfo  LogLevel = iota // default: informational
	LogLevelWarn                  // warning
	LogLevelError                 // error / failure
	LogLevelDebug                 // verbose / debug
)

// ANSI escape sequences for log-level coloring.
const (
	fgLogError = "\x1b[31m"       // red
	fgLogWarn  = "\x1b[33m"       // yellow
	fgLogDebug = "\x1b[38;5;242m" // dim gray
	// info: no color override (uses terminal default)
)

// ClassifyLogLine determines the log level from the message portion of a
// standard-library log line.  The expected format is
// "2006/01/02 15:04:05 <message>".  Classification uses the message prefix
// (text before the first ':' or space) to avoid false positives from
// substrings in file paths or other payload data:
//
//   - "server error:" → Error
//   - "warning:" → Warn
//   - "ws read/parse/marshal/write " → Debug (noisy per-frame logs)
//   - everything else → Info
func ClassifyLogLine(line string) LogLevel {
	// Skip the timestamp portion (first 20 chars: "2006/01/02 15:04:05 ").
	msg := line
	if len(msg) > 20 {
		msg = msg[20:]
	}
	lower := strings.ToLower(msg)

	// Error: known error-only prefixes
	if strings.HasPrefix(lower, "server error:") ||
		strings.HasPrefix(lower, "ws accept:") {
		return LogLevelError
	}

	// Warning: message starts with "warning:"
	if strings.HasPrefix(lower, "warning:") {
		return LogLevelWarn
	}

	// Debug: noisy per-connection websocket logs
	if strings.HasPrefix(lower, "ws read ") ||
		strings.HasPrefix(lower, "ws parse ") ||
		strings.HasPrefix(lower, "ws marshal ") ||
		strings.HasPrefix(lower, "ws write ") {
		return LogLevelDebug
	}

	return LogLevelInfo
}

// ColorizeLogLine wraps a log line with ANSI color escapes based on its
// classified level.  Info-level lines are returned unmodified.
func ColorizeLogLine(line string) string {
	level := ClassifyLogLine(line)
	switch level {
	case LogLevelError:
		return fgLogError + line + Reset
	case LogLevelWarn:
		return fgLogWarn + line + Reset
	case LogLevelDebug:
		return fgLogDebug + line + Reset
	default:
		return line
	}
}
