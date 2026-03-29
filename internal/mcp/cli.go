package mcp

import "strings"

// HealthStatus represents the connection status of an MCP server.
type HealthStatus string

const (
	StatusConnected  HealthStatus = "connected"
	StatusFailed     HealthStatus = "failed"
	StatusAuthNeeded HealthStatus = "auth_needed"
	StatusUnknown    HealthStatus = "unknown"
)

// ParseListOutput parses the text output of `claude mcp list` and returns
// a map from server name to health status.
//
// Expected line format:
//
//	<name>: <command/url> ... - <icon> <status_text>
func ParseListOutput(text string) map[string]HealthStatus {
	result := make(map[string]HealthStatus)

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Extract server name before the first ':'
		colonIdx := strings.Index(line, ":")
		if colonIdx <= 0 {
			continue
		}
		name := line[:colonIdx]

		// Skip header lines like "Checking MCP server health..."
		if strings.Contains(name, " ") {
			continue
		}

		// Parse status from the end: look for "- <icon> <text>"
		status := parseStatus(line)
		result[name] = status
	}

	return result
}

func parseStatus(line string) HealthStatus {
	switch {
	case strings.Contains(line, "Connected"):
		return StatusConnected
	case strings.Contains(line, "Failed to connect"):
		return StatusFailed
	case strings.Contains(line, "Needs authentication"):
		return StatusAuthNeeded
	default:
		return StatusUnknown
	}
}
