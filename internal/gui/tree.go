package gui

// TreeNodeKind distinguishes project-level from session-level nodes.
type TreeNodeKind int

const (
	ProjectNode TreeNodeKind = iota
	SessionNode
)

// ProjectItem is a read-only view of a project for display.
type ProjectItem struct {
	ID       string
	Name     string
	Path     string
	Expanded bool
	PM       *SessionItem
	Sessions []SessionItem
}

// TreeNode is a single row in the flattened tree view.
// The cursor indexes into a flat []TreeNode.
type TreeNode struct {
	Kind      TreeNodeKind
	ProjectID string
	Project   *ProjectItem // non-nil for ProjectNode
	Session   *SessionItem // non-nil for SessionNode
}

// BuildTreeNodes flattens projects into a list of TreeNodes.
// Collapsed projects show only the project row; expanded projects
// also include PM and session rows.
func BuildTreeNodes(projects []ProjectItem) []TreeNode {
	var nodes []TreeNode
	for i := range projects {
		p := &projects[i]
		nodes = append(nodes, TreeNode{
			Kind:      ProjectNode,
			ProjectID: p.ID,
			Project:   p,
		})
		if !p.Expanded {
			continue
		}
		if p.PM != nil {
			nodes = append(nodes, TreeNode{
				Kind:      SessionNode,
				ProjectID: p.ID,
				Session:   p.PM,
			})
		}
		for j := range p.Sessions {
			nodes = append(nodes, TreeNode{
				Kind:      SessionNode,
				ProjectID: p.ID,
				Session:   &p.Sessions[j],
			})
		}
	}
	return nodes
}
