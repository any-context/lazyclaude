package gui

import (
	"strings"
	"time"
)

// keyQueueSize is the capacity of the serial key forwarding queue.
const keyQueueSize = 1024

// FullScreenState manages fullscreen mode: target session, scroll offset,
// key forwarding queue, and state transitions.
type FullScreenState struct {
	active    bool
	target    string         // session ID
	scrollY   int            // mouse scroll offset
	forwarder InputForwarder // key forwarding (injected)
	keyQueue  chan keyCmd     // serial forwarding queue
	preview   *PreviewCache  // for invalidation on transition
}

// NewFullScreenState creates a FullScreenState.
func NewFullScreenState(preview *PreviewCache) *FullScreenState {
	return &FullScreenState{
		keyQueue: make(chan keyCmd, keyQueueSize),
		preview:  preview,
	}
}

// IsActive returns true if fullscreen mode is active.
func (fs *FullScreenState) IsActive() bool { return fs.active }

// Target returns the session ID being viewed in fullscreen.
func (fs *FullScreenState) Target() string { return fs.target }

// ScrollY returns the mouse scroll offset.
func (fs *FullScreenState) ScrollY() int { return fs.scrollY }

// SetScrollY sets the scroll offset directly.
func (fs *FullScreenState) SetScrollY(y int) { fs.scrollY = y }

// ScrollDown increments scroll offset.
func (fs *FullScreenState) ScrollDown() { fs.scrollY++ }

// ScrollUp decrements scroll offset (clamped to 0).
func (fs *FullScreenState) ScrollUp() {
	if fs.scrollY > 0 {
		fs.scrollY--
	}
}

// SetForwarder injects the key forwarder.
func (fs *FullScreenState) SetForwarder(fwd InputForwarder) {
	fs.forwarder = fwd
}

// Enter transitions to fullscreen mode for the given session.
func (fs *FullScreenState) Enter(sessionID string) {
	if fs.active {
		return
	}
	fs.active = true
	fs.target = sessionID
	fs.scrollY = 0
	if fs.preview != nil {
		fs.preview.Invalidate()
	}
}

// Exit transitions out of fullscreen mode.
func (fs *FullScreenState) Exit() {
	if !fs.active {
		return
	}
	fs.active = false
	fs.target = ""
	if fs.preview != nil {
		fs.preview.Invalidate()
	}
}

// EnqueueKey adds a key name to the serial forwarding queue.
// Non-blocking: if the queue is full, the key is dropped.
func (fs *FullScreenState) EnqueueKey(target, key string) {
	select {
	case fs.keyQueue <- keyCmd{target: target, key: key}:
	default:
	}
}

// EnqueueLiteral adds literal text to the serial forwarding queue.
// Uses send-keys -l so the text is not interpreted as key names.
func (fs *FullScreenState) EnqueueLiteral(target, text string) {
	select {
	case fs.keyQueue <- keyCmd{target: target, key: text, literal: true}:
	default:
	}
}

// RunKeyForwarder drains the key queue serially, preserving order.
// Adjacent literal commands for the same target are batched into a single
// send-keys -l call to handle paste bursts efficiently.
func (fs *FullScreenState) RunKeyForwarder(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case cmd := <-fs.keyQueue:
			if cmd.literal {
				fs.dispatchBatch(cmd, done)
			} else {
				fs.dispatchKey(cmd)
			}
		}
	}
}

// dispatchBatch collects consecutive literal commands for the same target
// and sends them as a single ForwardLiteral call. Iterative (no recursion).
func (fs *FullScreenState) dispatchBatch(first keyCmd, done <-chan struct{}) {
	cmd := first
	for {
		var buf strings.Builder
		buf.WriteString(cmd.key)
		target := cmd.target

		continueBatch := false
		for {
			select {
			case <-done:
				fs.flushLiteral(target, &buf)
				return
			case next := <-fs.keyQueue:
				if next.literal && next.target == target {
					buf.WriteString(next.key)
					continue
				}
				fs.flushLiteral(target, &buf)
				if next.literal {
					cmd = next
					continueBatch = true
				} else {
					fs.dispatchKey(next)
				}
			default:
				fs.flushLiteral(target, &buf)
				return
			}
			break
		}
		if !continueBatch {
			return
		}
	}
}

// DrainQueue processes all pending keys synchronously (for testing).
// Adjacent literal commands are batched, matching RunKeyForwarder behavior.
func (fs *FullScreenState) DrainQueue() {
	for {
		select {
		case cmd := <-fs.keyQueue:
			if cmd.literal {
				fs.drainBatch(cmd)
			} else {
				fs.dispatchKey(cmd)
			}
		default:
			return
		}
	}
}

// drainBatch is the synchronous equivalent of dispatchBatch for testing.
// Iterative (no recursion).
func (fs *FullScreenState) drainBatch(first keyCmd) {
	cmd := first
	for {
		var buf strings.Builder
		buf.WriteString(cmd.key)
		target := cmd.target

		continueBatch := false
		for {
			select {
			case next := <-fs.keyQueue:
				if next.literal && next.target == target {
					buf.WriteString(next.key)
					continue
				}
				fs.flushLiteral(target, &buf)
				if next.literal {
					cmd = next
					continueBatch = true
				} else {
					fs.dispatchKey(next)
				}
			default:
				fs.flushLiteral(target, &buf)
				return
			}
			break
		}
		if !continueBatch {
			return
		}
	}
}

// flushLiteral sends the accumulated literal buffer if non-empty.
func (fs *FullScreenState) flushLiteral(target string, buf *strings.Builder) {
	if fs.forwarder != nil && buf.Len() > 0 {
		fs.forwarder.ForwardLiteral(target, buf.String())
	}
}

// dispatchKey sends a non-literal key command (key name like "Enter", "Space").
func (fs *FullScreenState) dispatchKey(cmd keyCmd) {
	if fs.forwarder != nil {
		fs.forwarder.ForwardKey(cmd.target, cmd.key)
	}
}

// TriggerRefresh resets scroll and invalidates preview after key input.
func (fs *FullScreenState) TriggerRefresh() {
	fs.scrollY = 0
	if fs.preview != nil {
		fs.preview.Lock()
		if !fs.preview.Busy() && fs.preview.Stale(50*time.Millisecond) {
			fs.preview.InvalidateTimestamp()
		}
		fs.preview.Unlock()
	}
}
