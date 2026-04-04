package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveRemotePath_NoPendingRemotePath_Passthrough(t *testing.T) {
	t.Parallel()
	a := &guiCompositeAdapter{localProjectRoot: "/Users/kenshin/project"}
	assert.Equal(t, "/Users/kenshin/project", a.resolveRemotePath("/Users/kenshin/project"))
}

func TestResolveRemotePath_DotPath(t *testing.T) {
	t.Parallel()
	a := &guiCompositeAdapter{
		pendingRemotePath: "/home/user/project",
		localProjectRoot:  "/Users/kenshin/project",
	}
	assert.Equal(t, "/home/user/project", a.resolveRemotePath("."))
}

func TestResolveRemotePath_LocalProjectRoot(t *testing.T) {
	t.Parallel()
	a := &guiCompositeAdapter{
		pendingRemotePath: "/home/user/project",
		localProjectRoot:  "/Users/kenshin/project",
	}
	assert.Equal(t, "/home/user/project", a.resolveRemotePath("/Users/kenshin/project"))
}

func TestResolveRemotePath_RemotePath_Passthrough(t *testing.T) {
	t.Parallel()
	a := &guiCompositeAdapter{
		pendingRemotePath: "/home/user/project",
		localProjectRoot:  "/Users/kenshin/project",
	}
	// A path from an existing remote session should pass through unchanged.
	assert.Equal(t, "/home/user/other-project", a.resolveRemotePath("/home/user/other-project"))
}

func TestResolveRemotePath_NoPendingRemotePath(t *testing.T) {
	t.Parallel()
	a := &guiCompositeAdapter{
		pendingRemotePath: "",
		localProjectRoot:  "/Users/kenshin/project",
	}
	// No remote path detected: passthrough even for local paths.
	assert.Equal(t, "/Users/kenshin/project", a.resolveRemotePath("/Users/kenshin/project"))
	assert.Equal(t, ".", a.resolveRemotePath("."))
}
