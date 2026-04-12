package gui

import (
	"bufio"
	"os"
	"sort"
	"strings"
)

// ParseSSHHosts reads an SSH config file and returns a sorted, deduplicated
// list of Host aliases. Wildcard patterns (containing * or ?) are skipped.
// Returns an empty slice (not an error) when the file does not exist.
func ParseSSHHosts(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	seen := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Match "Host" directive (case-insensitive).
		if !strings.HasPrefix(strings.ToLower(line), "host ") &&
			!strings.HasPrefix(strings.ToLower(line), "host\t") {
			continue
		}

		// Skip "Match" lines that happen to start with "Host" substring
		// (not applicable here since we already matched "Host " with space).

		// Extract patterns after "Host".
		rest := strings.TrimSpace(line[4:]) // len("Host") == 4
		for _, pattern := range strings.Fields(rest) {
			if strings.ContainsAny(pattern, "*?") {
				continue
			}
			if pattern == "" {
				continue
			}
			seen[pattern] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	hosts := make([]string, 0, len(seen))
	for h := range seen {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts, nil
}
