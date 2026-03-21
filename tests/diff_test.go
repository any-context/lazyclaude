package tests_test

import (
	"testing"
	"time"

	"github.com/ActiveState/termtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpOutput_Component(t *testing.T) {
	bin := ensureBinary(t)

	cp, err := termtest.New(termtest.Options{
		CmdName:        bin,
		Args:           []string{"--help"},
		DefaultTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer cp.Close()

	out, err := cp.Expect("terminal UI")
	require.NoError(t, err)
	assert.Contains(t, out, "lazyclaude")
}

func TestVersionOutput_Component(t *testing.T) {
	bin := ensureBinary(t)

	cp, err := termtest.New(termtest.Options{
		CmdName:        bin,
		Args:           []string{"--version"},
		DefaultTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer cp.Close()

	out, err := cp.Expect("lazyclaude version")
	require.NoError(t, err)
	assert.Contains(t, out, "lazyclaude")
}

func TestServerHelp_Component(t *testing.T) {
	bin := ensureBinary(t)

	cp, err := termtest.New(termtest.Options{
		CmdName:        bin,
		Args:           []string{"server", "--help"},
		DefaultTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer cp.Close()

	out, err := cp.Expect("MCP server")
	require.NoError(t, err)
	assert.Contains(t, out, "server")
}

func TestDiffMissingArgs_Component(t *testing.T) {
	bin := ensureBinary(t)

	cp, err := termtest.New(termtest.Options{
		CmdName:        bin,
		Args:           []string{"diff"},
		DefaultTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer cp.Close()

	out, err := cp.Expect("required")
	require.NoError(t, err)
	assert.Contains(t, out, "required")
}
