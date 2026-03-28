package gui

import (
	"bytes"
	"fmt"

	"github.com/KEMSHlM/lazyclaude/internal/gui/presentation"
)

// RenderSessionListForTest renders sessions to a string buffer for testing.
// Uses the shared sessionDisplayName/sessionStatusIcon helpers.
func RenderSessionListForTest(items []SessionItem, cursor int) string {
	if len(items) == 0 {
		return ""
	}

	var buf bytes.Buffer
	for _, item := range items {
		name := sessionDisplayName(&item)
		icon := sessionStatusIcon(&item)
		fmt.Fprintf(&buf, "%-20s%s\n", name, icon)
	}
	return buf.String()
}

// RenderTreeForTest renders the tree view to a string buffer for testing.
func RenderTreeForTest(nodes []TreeNode, cursor int) string {
	if len(nodes) == 0 {
		return ""
	}

	var buf bytes.Buffer
	for i, node := range nodes {
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}

		switch node.Kind {
		case ProjectNode:
			expandIcon := presentation.IconProjectCollapsed
			if node.Project.Expanded {
				expandIcon = presentation.IconProjectExpanded
			}
			fmt.Fprintf(&buf, "%s%s %s\n", prefix, expandIcon, node.Project.Name)

		case SessionNode:
			name := sessionDisplayName(node.Session)
			icon := sessionStatusIcon(node.Session)
			fmt.Fprintf(&buf, "%s  %-18s%s\n", prefix, name, icon)
		}
	}
	return buf.String()
}
