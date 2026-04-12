package gui

import (
	"os"
	"os/exec"
	"strings"
)

// DetectSSHHost returns the SSH hostname if the originating pane is running ssh.
// Uses environment variables set by lazyclaude.tmux from the pane's tmux format variables.
func DetectSSHHost() string {
	paneCmd := os.Getenv("LAZYCLAUDE_PANE_CMD")
	if paneCmd != "ssh" {
		return ""
	}

	panePID := os.Getenv("LAZYCLAUDE_PANE_PID")
	if panePID == "" {
		return ""
	}

	// Try child ssh process of the pane's shell
	host := sshHostFromPID(panePID)
	if host != "" {
		return host
	}

	// Fallback: check the pane's TTY for ssh processes
	paneTTY := os.Getenv("LAZYCLAUDE_PANE_TTY")
	if paneTTY != "" {
		// Strip /dev/ prefix
		tty := strings.TrimPrefix(paneTTY, "/dev/")
		host = sshHostFromTTY(tty)
	}
	return host
}

// sshHostFromPID finds ssh children of the given PID and extracts the hostname.
func sshHostFromPID(pid string) string {
	out, err := exec.Command("pgrep", "-P", pid, "-x", "ssh").Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	sshPID := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	return sshHostFromProcessArgs(sshPID)
}

// sshHostFromTTY scans processes on the given TTY for ssh.
func sshHostFromTTY(tty string) string {
	out, err := exec.Command("ps", "-t", tty, "-o", "pid=,args=").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, " ssh ") || strings.HasSuffix(line, " ssh") {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				return parseSSHHost(parts[1])
			}
		}
	}
	return ""
}

// sshHostFromProcessArgs extracts SSH hostname from a PID's command line.
func sshHostFromProcessArgs(pid string) string {
	out, err := exec.Command("ps", "-p", pid, "-o", "args=").Output()
	if err != nil {
		return ""
	}
	return parseSSHHost(strings.TrimSpace(string(out)))
}

// parseSSHHost extracts the hostname from an ssh command line.
// Skips flags and their arguments.
func parseSSHHost(cmdLine string) string {
	// SSH options that take an argument
	flagsWithArg := "bcDEeFIiJlmopQRSWw"
	args := strings.Fields(cmdLine)
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") && len(arg) == 2 && strings.Contains(flagsWithArg, string(arg[1])) {
			i++ // skip the flag's argument
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		// First non-flag argument is the hostname
		return arg
	}
	return ""
}
