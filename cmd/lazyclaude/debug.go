package main

import "github.com/any-context/lazyclaude/internal/core/debuglog"

// debugLog is a package-local wrapper around debuglog.Log.
func debugLog(format string, args ...any) {
	debuglog.Log(format, args...)
}
