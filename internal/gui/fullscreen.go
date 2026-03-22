package gui

import "time"

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
		keyQueue: make(chan keyCmd, 64),
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

// EnqueueKey adds a key to the serial forwarding queue.
// Non-blocking: if the queue is full, the key is dropped.
func (fs *FullScreenState) EnqueueKey(target, key string) {
	select {
	case fs.keyQueue <- keyCmd{target: target, key: key}:
	default:
	}
}

// RunKeyForwarder drains the key queue serially, preserving order.
func (fs *FullScreenState) RunKeyForwarder(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case cmd := <-fs.keyQueue:
			if fs.forwarder != nil {
				fs.forwarder.ForwardKey(cmd.target, cmd.key)
			}
		}
	}
}

// DrainQueue processes all pending keys synchronously (for testing).
func (fs *FullScreenState) DrainQueue() {
	for {
		select {
		case cmd := <-fs.keyQueue:
			if fs.forwarder != nil {
				fs.forwarder.ForwardKey(cmd.target, cmd.key)
			}
		default:
			return
		}
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
