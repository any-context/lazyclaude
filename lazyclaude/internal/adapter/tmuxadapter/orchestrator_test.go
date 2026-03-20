package tmuxadapter

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/tmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPopupClient records DisplayPopup calls and blocks until released.
type mockPopupClient struct {
	tmux.Client
	mu     sync.Mutex
	calls  []tmux.PopupOpts
	gates  chan struct{} // send to release a blocked DisplayPopup
	doneCh chan string   // receives window after DisplayPopup returns
}

func newMockPopupClient() *mockPopupClient {
	return &mockPopupClient{
		gates:  make(chan struct{}, 10),
		doneCh: make(chan string, 10),
	}
}

func (m *mockPopupClient) FindActiveClient(_ context.Context) (*tmux.ClientInfo, error) {
	return &tmux.ClientInfo{Name: "/dev/pts/0"}, nil
}

func (m *mockPopupClient) DisplayPopup(_ context.Context, opts tmux.PopupOpts) error {
	m.mu.Lock()
	m.calls = append(m.calls, opts)
	m.mu.Unlock()

	// Block until released (simulates popup open)
	<-m.gates

	m.doneCh <- opts.Target
	return nil
}

func (m *mockPopupClient) getCalls() []tmux.PopupOpts {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]tmux.PopupOpts, len(m.calls))
	copy(cp, m.calls)
	return cp
}

func TestPopupQueue_SinglePopup(t *testing.T) {
	mock := newMockPopupClient()
	logger := log.New(os.Stderr, "test: ", 0)
	p := NewPopupOrchestrator("lazyclaude", mock, logger)

	p.SpawnToolPopup(context.Background(), "@1", "Bash", "{}", "/tmp")

	// Wait for popup to be spawned
	require.Eventually(t, func() bool {
		return len(mock.getCalls()) == 1
	}, time.Second, 10*time.Millisecond)

	// Release popup
	mock.gates <- struct{}{}

	// Should complete
	select {
	case w := <-mock.doneCh:
		assert.Equal(t, "@1", w)
	case <-time.After(time.Second):
		t.Fatal("popup did not complete")
	}
}

func TestPopupQueue_SequentialForSameWindow(t *testing.T) {
	mock := newMockPopupClient()
	logger := log.New(os.Stderr, "test: ", 0)
	p := NewPopupOrchestrator("lazyclaude", mock, logger)

	// Spawn 3 popups for the same window rapidly
	p.SpawnToolPopup(context.Background(), "@1", "Tool1", "{}", "/tmp")
	p.SpawnToolPopup(context.Background(), "@1", "Tool2", "{}", "/tmp")
	p.SpawnToolPopup(context.Background(), "@1", "Tool3", "{}", "/tmp")

	// Only 1 should be spawned immediately
	require.Eventually(t, func() bool {
		return len(mock.getCalls()) == 1
	}, time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, len(mock.getCalls()), "only 1 popup should be active")

	// Release first popup -> second should spawn
	mock.gates <- struct{}{}
	require.Eventually(t, func() bool {
		return len(mock.getCalls()) == 2
	}, 2*time.Second, 10*time.Millisecond)

	// Release second -> third should spawn
	mock.gates <- struct{}{}
	require.Eventually(t, func() bool {
		return len(mock.getCalls()) == 3
	}, 2*time.Second, 10*time.Millisecond)

	// Release third
	mock.gates <- struct{}{}

	// All 3 should have Tool1, Tool2, Tool3 in order (tool name is in env)
	calls := mock.getCalls()
	require.Len(t, calls, 3)
	assert.Equal(t, "Tool1", calls[0].Env["TOOL_NAME"])
	assert.Equal(t, "Tool2", calls[1].Env["TOOL_NAME"])
	assert.Equal(t, "Tool3", calls[2].Env["TOOL_NAME"])
}

func TestPopupQueue_DifferentWindowsConcurrent(t *testing.T) {
	mock := newMockPopupClient()
	logger := log.New(os.Stderr, "test: ", 0)
	p := NewPopupOrchestrator("lazyclaude", mock, logger)

	// Spawn popups for different windows
	p.SpawnToolPopup(context.Background(), "@1", "Tool1", "{}", "/tmp")
	p.SpawnToolPopup(context.Background(), "@2", "Tool2", "{}", "/tmp")

	// Both should spawn immediately (different windows)
	require.Eventually(t, func() bool {
		return len(mock.getCalls()) == 2
	}, time.Second, 10*time.Millisecond)

	// Release both
	mock.gates <- struct{}{}
	mock.gates <- struct{}{}
}

func TestPopupQueue_QueueLength(t *testing.T) {
	mock := newMockPopupClient()
	logger := log.New(os.Stderr, "test: ", 0)
	p := NewPopupOrchestrator("lazyclaude", mock, logger)

	assert.Equal(t, 0, p.QueueLen("@1"))

	p.SpawnToolPopup(context.Background(), "@1", "Tool1", "{}", "/tmp")
	require.Eventually(t, func() bool {
		return len(mock.getCalls()) == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, 0, p.QueueLen("@1"), "active popup, no queue")

	p.SpawnToolPopup(context.Background(), "@1", "Tool2", "{}", "/tmp")
	p.SpawnToolPopup(context.Background(), "@1", "Tool3", "{}", "/tmp")
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 2, p.QueueLen("@1"), "2 queued")

	// Release first
	mock.gates <- struct{}{}
	require.Eventually(t, func() bool {
		return p.QueueLen("@1") == 1
	}, 2*time.Second, 10*time.Millisecond)

	// Release second
	mock.gates <- struct{}{}
	require.Eventually(t, func() bool {
		return p.QueueLen("@1") == 0
	}, 2*time.Second, 10*time.Millisecond)

	// Release third
	mock.gates <- struct{}{}
}
