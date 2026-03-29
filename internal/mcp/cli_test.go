package mcp

import "testing"

func TestParseListOutput(t *testing.T) {
	t.Parallel()

	input := `Checking MCP server health...

github: npx -y @modelcontextprotocol/server-github - ✓ Connected
filesystem: npx -y @modelcontextprotocol/server-filesystem /home/user/Projects - ✗ Failed to connect
memory: npx -y @modelcontextprotocol/server-memory - ✓ Connected
vercel: https://mcp.vercel.com (HTTP) - ! Needs authentication
cloudflare-docs: https://docs.mcp.cloudflare.com/mcp (HTTP) - ✓ Connected
railway: npx -y @railway/mcp-server - ✓ Connected`

	statuses := ParseListOutput(input)

	tests := []struct {
		name string
		want HealthStatus
	}{
		{"github", StatusConnected},
		{"filesystem", StatusFailed},
		{"memory", StatusConnected},
		{"vercel", StatusAuthNeeded},
		{"cloudflare-docs", StatusConnected},
		{"railway", StatusConnected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := statuses[tt.name]
			if !ok {
				t.Fatalf("missing status for %q", tt.name)
			}
			if got != tt.want {
				t.Errorf("status[%q] = %q, want %q", tt.name, got, tt.want)
			}
		})
	}

	if len(statuses) != 6 {
		t.Errorf("got %d entries, want 6", len(statuses))
	}
}

func TestParseListOutput_empty(t *testing.T) {
	t.Parallel()

	statuses := ParseListOutput("")
	if len(statuses) != 0 {
		t.Errorf("got %d entries, want 0", len(statuses))
	}
}

func TestParseListOutput_header_only(t *testing.T) {
	t.Parallel()

	statuses := ParseListOutput("Checking MCP server health...\n")
	if len(statuses) != 0 {
		t.Errorf("got %d entries, want 0", len(statuses))
	}
}
