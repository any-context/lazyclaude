package daemon

import (
	"context"
	"fmt"
)

// mockSSH records calls and returns configured responses.
type mockSSH struct {
	runResults map[string]mockResult // command -> result
	copyErr    error
	runCalls   []string
	copyCalls  []copyCall
}

type mockResult struct {
	output []byte
	err    error
}

type copyCall struct {
	host, local, remote string
}

func newMockSSH() *mockSSH {
	return &mockSSH{
		runResults: make(map[string]mockResult),
	}
}

func (m *mockSSH) onRun(cmd string, output string, err error) {
	m.runResults[cmd] = mockResult{output: []byte(output), err: err}
}

func (m *mockSSH) Run(_ context.Context, _, command string) ([]byte, error) {
	m.runCalls = append(m.runCalls, command)
	if r, ok := m.runResults[command]; ok {
		return r.output, r.err
	}
	return nil, fmt.Errorf("unexpected command: %s", command)
}

func (m *mockSSH) Copy(_ context.Context, host, localPath, remotePath string) error {
	m.copyCalls = append(m.copyCalls, copyCall{host, localPath, remotePath})
	return m.copyErr
}
