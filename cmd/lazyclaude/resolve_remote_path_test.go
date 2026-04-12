package main

import (
	"testing"

	"github.com/any-context/lazyclaude/internal/daemon"
	"github.com/stretchr/testify/assert"
)

func TestResolveRemotePath_NonLocalPath_Passthrough(t *testing.T) {
	t.Parallel()
	a := &guiCompositeAdapter{
		cp:               daemon.NewCompositeProvider(nil, nil),
		localProjectRoot: "/local/project",
	}
	// A path that is neither "." nor the local project root is assumed to
	// be an existing remote project path (e.g. selected from the session
	// tree) and must be passed through unchanged.
	assert.Equal(t, "/home/user/other-project", a.resolveRemotePath("/home/user/other-project", "remote"))
}

func TestResolveRemotePath_DotPath_NoProvider_FallbackDot(t *testing.T) {
	t.Parallel()
	a := &guiCompositeAdapter{
		cp:               daemon.NewCompositeProvider(nil, nil),
		localProjectRoot: "/local/project",
	}
	// "." triggers a remote CWD query; with no provider, falls back to "."
	// so the daemon uses its own CWD.
	assert.Equal(t, ".", a.resolveRemotePath(".", "remote"))
}

func TestResolveRemotePath_LocalProjectRoot_NoProvider_FallbackDot(t *testing.T) {
	t.Parallel()
	a := &guiCompositeAdapter{
		cp:               daemon.NewCompositeProvider(nil, nil),
		localProjectRoot: "/local/project",
	}
	// localProjectRoot triggers a remote CWD query; with no provider, falls
	// back to "." because local paths are meaningless on the remote machine.
	assert.Equal(t, ".", a.resolveRemotePath("/local/project", "remote"))
}

func TestResolveRemotePath_EmptyLocalProjectRoot_Passthrough(t *testing.T) {
	t.Parallel()
	a := &guiCompositeAdapter{
		cp:               daemon.NewCompositeProvider(nil, nil),
		localProjectRoot: "",
	}
	// When localProjectRoot is unset, only "." is treated as local-origin.
	// Any concrete path must be passed through unchanged so that a real
	// path is never mistaken for a local fallback.
	assert.Equal(t, "/some/path", a.resolveRemotePath("/some/path", "remote"))
}
