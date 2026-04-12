//go:build linux

package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// isShellName reports whether the given command name is an interactive shell.
func isShellName(name string) bool {
	switch name {
	case "bash", "zsh", "fish", "sh":
		return true
	}
	return false
}

// detectUserShellCWD finds the CWD of the user's interactive shell by
// scanning /proc. It looks for shell processes owned by the current user
// that are attached to a PTY, excluding the daemon's own process tree.
// When multiple candidates exist, the one with the highest PID (most
// recently started) is returned.
func detectUserShellCWD() (string, error) {
	uid := os.Getuid()
	daemonPID := os.Getpid()

	daemonTree, err := buildProcessTree(daemonPID)
	if err != nil {
		return "", fmt.Errorf("build process tree: %w", err)
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "", fmt.Errorf("read /proc: %w", err)
	}

	var bestPID int
	var bestCWD string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if daemonTree[pid] {
			continue
		}
		if pid <= bestPID {
			continue
		}

		info, err := readProcInfo(pid)
		if err != nil {
			continue
		}
		if info.uid != uid {
			continue
		}
		if !info.hasPTY {
			continue
		}
		if !isShellName(info.comm) {
			continue
		}
		// Only consider session leaders (pid == sid) to exclude child
		// processes like gitstatus that may have CWD set to "/".
		if pid != info.sid {
			continue
		}

		cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
		if err != nil {
			continue
		}
		bestPID = pid
		bestCWD = cwd
	}

	if bestCWD == "" {
		return "", fmt.Errorf("no interactive shell found for uid %d", uid)
	}
	return bestCWD, nil
}

// procInfo holds parsed process metadata from /proc.
type procInfo struct {
	uid    int
	comm   string
	hasPTY bool
	sid    int // session ID from /proc/{pid}/stat
}

// readProcInfo reads uid, comm, and tty info for a given PID from /proc.
func readProcInfo(pid int) (procInfo, error) {
	// Read UID from /proc/{pid}/status
	statusData, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return procInfo{}, err
	}
	uid, err := parseUID(string(statusData))
	if err != nil {
		return procInfo{}, err
	}

	// Read comm from /proc/{pid}/comm
	commData, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return procInfo{}, err
	}
	comm := strings.TrimSpace(string(commData))

	// Parse tty_nr and session ID from /proc/{pid}/stat (single read).
	statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return procInfo{}, err
	}
	statLine := string(statData)
	hasPTY, err := parseTTYFromStat(statLine)
	if err != nil {
		return procInfo{}, err
	}
	sid, err := parseSIDFromStatLine(statLine)
	if err != nil {
		return procInfo{}, err
	}

	return procInfo{uid: uid, comm: comm, hasPTY: hasPTY, sid: sid}, nil
}

// parseUID extracts the real UID from /proc/{pid}/status content.
func parseUID(status string) (int, error) {
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, fmt.Errorf("malformed Uid line: %s", line)
			}
			return strconv.Atoi(fields[1]) // fields[1] is the real UID
		}
	}
	return 0, fmt.Errorf("Uid line not found")
}

// parseTTYFromStat extracts tty_nr from a /proc/{pid}/stat line and checks
// if it corresponds to a PTY (major device number 136).
// The stat format has the comm field in parentheses which may contain spaces,
// so we find the closing ')' and count fields from there.
func parseTTYFromStat(stat string) (bool, error) {
	// Find last ')' to skip past comm field (may contain spaces/parens)
	idx := strings.LastIndex(stat, ")")
	if idx < 0 {
		return false, fmt.Errorf("malformed stat: no closing paren")
	}
	rest := stat[idx+1:]
	fields := strings.Fields(rest)
	// After ')': state(0) ppid(1) pgrp(2) session(3) tty_nr(4) ...
	if len(fields) < 5 {
		return false, fmt.Errorf("malformed stat: too few fields after comm")
	}
	ttyNr, err := strconv.Atoi(fields[4])
	if err != nil {
		return false, fmt.Errorf("parse tty_nr: %w", err)
	}
	// Linux new_encode_dev: major occupies bits 19..8 (12 bits)
	major := (ttyNr >> 8) & 0xfff
	return major == 136, nil
}

// parseSIDFromStatLine extracts the session ID from a /proc/{pid}/stat line.
// After the closing ')' of the comm field, fields are:
// state(0) ppid(1) pgrp(2) session(3) ...
func parseSIDFromStatLine(stat string) (int, error) {
	idx := strings.LastIndex(stat, ")")
	if idx < 0 {
		return 0, fmt.Errorf("malformed stat: no closing paren")
	}
	fields := strings.Fields(stat[idx+1:])
	if len(fields) < 4 {
		return 0, fmt.Errorf("malformed stat: too few fields for sid")
	}
	sid, err := strconv.Atoi(fields[3])
	if err != nil {
		return 0, fmt.Errorf("parse sid: %w", err)
	}
	return sid, nil
}

// buildProcessTree returns a set of PIDs that are the given root PID or
// any of its descendants. This is used to exclude the daemon's own
// process tree from shell detection.
func buildProcessTree(rootPID int) (map[int]bool, error) {
	tree := map[int]bool{rootPID: true}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return tree, fmt.Errorf("read /proc: %w", err)
	}

	// Build parent->children map
	parentOf := make(map[int]int)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		ppid, err := readPPID(pid)
		if err != nil {
			continue
		}
		parentOf[pid] = ppid
	}

	// Walk all PIDs and mark those whose ancestor chain includes rootPID
	for pid := range parentOf {
		if tree[pid] {
			continue
		}
		chain := []int{pid}
		cur := pid
		for {
			pp, ok := parentOf[cur]
			if !ok || pp <= 0 {
				break
			}
			if tree[pp] {
				// All PIDs in the chain are descendants
				for _, p := range chain {
					tree[p] = true
				}
				break
			}
			chain = append(chain, pp)
			cur = pp
		}
	}

	return tree, nil
}

// readPPID reads the parent PID from /proc/{pid}/stat.
func readPPID(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	idx := strings.LastIndex(string(data), ")")
	if idx < 0 {
		return 0, fmt.Errorf("malformed stat")
	}
	fields := strings.Fields(string(data)[idx+1:])
	// After ')': state(0) ppid(1) ...
	if len(fields) < 2 {
		return 0, fmt.Errorf("malformed stat: too few fields")
	}
	return strconv.Atoi(fields[1])
}
