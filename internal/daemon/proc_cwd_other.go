//go:build !linux

package daemon

import "fmt"

// detectUserShellCWD is not supported on non-Linux platforms.
// The daemon is designed to run on Linux; on other platforms this
// returns an error so the caller can fall back to os.Getwd().
func detectUserShellCWD() (string, error) {
	return "", fmt.Errorf("proc-based CWD detection not supported on this platform")
}
