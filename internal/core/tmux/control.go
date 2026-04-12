package tmux

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ControlEventType classifies a tmux control mode line.
type ControlEventType int

const (
	EventOutput ControlEventType = iota // %output <pane-id> <data>
	EventBegin                          // %begin <time> <num> <flags>
	EventEnd                            // %end <time> <num> <flags>
	EventError                          // %error <time> <num> <flags>
	EventOther                          // any other notification
)

// ControlEvent is a parsed control mode line.
type ControlEvent struct {
	Type   ControlEventType
	PaneID string // set for EventOutput
	Data   string // payload
}

// ParseControlLine parses a single line from tmux control mode.
func ParseControlLine(line string) ControlEvent {
	if strings.HasPrefix(line, "%output ") {
		rest := line[len("%output "):]
		parts := strings.SplitN(rest, " ", 2)
		paneID := parts[0]
		data := ""
		if len(parts) > 1 {
			data = parts[1]
		}
		return ControlEvent{Type: EventOutput, PaneID: paneID, Data: data}
	}
	if strings.HasPrefix(line, "%begin ") {
		return ControlEvent{Type: EventBegin, Data: line[len("%begin "):]}
	}
	if strings.HasPrefix(line, "%end ") {
		return ControlEvent{Type: EventEnd, Data: line[len("%end "):]}
	}
	if strings.HasPrefix(line, "%error ") {
		return ControlEvent{Type: EventError, Data: line[len("%error "):]}
	}
	return ControlEvent{Type: EventOther, Data: line}
}

// pendingQuery is an in-flight control mode command awaiting its
// %begin/%end response block.
type pendingQuery struct {
	ch chan queryResult // response is delivered here
}

type queryResult struct {
	lines []string
	err   bool // true if %error instead of %end
}

// ControlClient maintains a tmux control mode connection.
// It receives %output events and can send commands through the connection.
type ControlClient struct {
	socket  string
	session string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	scanner *bufio.Scanner

	mu       sync.Mutex
	onOutput func(paneID string) // callback when pane has new output
	// queryQueue is a FIFO of pending Query calls. Control mode is
	// sequential: commands are processed in order, so the next %begin
	// always belongs to the oldest pending query.
	queryQueue []pendingQuery
	done       chan struct{}
	closed     bool
}

// NewControlClient creates and starts a control mode connection.
// onOutput is called (from a goroutine) when any pane produces output.
// Pass nil if output events are not needed.
func NewControlClient(socket, session string, onOutput func(paneID string)) (*ControlClient, error) {
	args := []string{"-u", "-C"}
	if socket != "" {
		args = append(args, "-L", socket)
	}
	args = append(args, "attach-session", "-t", session)

	cmd := exec.Command("tmux", args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start control mode: %w", err)
	}

	c := &ControlClient{
		socket:   socket,
		session:  session,
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		scanner:  bufio.NewScanner(stdout),
		onOutput: onOutput,
		done:     make(chan struct{}),
	}

	go c.readLoop()
	return c, nil
}

// SendKeys sends keystrokes to a target pane through the control connection.
// Much faster than spawning a subprocess for each keystroke.
func (c *ControlClient) SendKeys(target string, keys ...string) error {
	// Validate to prevent tmux command injection.
	// Target must not contain spaces (would split tmux args) or injection chars.
	if err := validateControlTarget(target); err != nil {
		return err
	}
	// Keys may contain spaces (e.g., " " for Space) but not injection chars.
	for _, k := range keys {
		if err := validateControlKey(k); err != nil {
			return err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("control client closed")
	}
	args := fmt.Sprintf("send-keys -t %s", target)
	for _, k := range keys {
		args += " " + k
	}
	_, err := fmt.Fprintf(c.stdin, "%s\n", args)
	return err
}

// SendKeysLiteral sends text literally through the control connection (send-keys -l).
// The text is double-quoted to prevent tmux from interpreting special characters
// (semicolons, spaces, etc.) as command separators or argument delimiters.
func (c *ControlClient) SendKeysLiteral(target string, text string) error {
	if err := validateControlTarget(target); err != nil {
		return err
	}
	// Newlines/carriage returns break the control mode line protocol.
	// NUL bytes would truncate the write at the C layer.
	for _, ch := range text {
		if ch == '\n' || ch == '\r' || ch == '\x00' {
			return fmt.Errorf("literal text contains unsafe character %q", ch)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("control client closed")
	}
	// TODO: The escaping below may need review for edge cases with tmux
	// control mode quoting (e.g., unusual Unicode, combining characters,
	// or tmux version-specific behavior).
	//
	// Quote the text so tmux control mode doesn't split on spaces or
	// interpret ; as a command separator. Escape embedded double quotes.
	escaped := strings.ReplaceAll(text, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	_, err := fmt.Fprintf(c.stdin, "send-keys -l -t %s -- \"%s\"\n", target, escaped)
	return err
}

// validateControlTarget rejects tmux command injection in target strings.
// Spaces are blocked because they would split the command into multiple args.
func validateControlTarget(s string) error {
	for _, c := range s {
		switch c {
		case '\n', '\r', ';', ' ':
			return fmt.Errorf("target contains unsafe character %q", c)
		}
	}
	return nil
}

// validateControlKey rejects injection chars in key strings.
// Spaces are allowed (e.g., " " for Space key).
func validateControlKey(s string) error {
	for _, c := range s {
		switch c {
		case '\n', '\r', ';':
			return fmt.Errorf("key contains unsafe character %q", c)
		}
	}
	return nil
}

// Closed returns whether the control mode connection has ended.
func (c *ControlClient) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Close terminates the control mode connection.
func (c *ControlClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	// Send detach + close stdin under lock to prevent double-close
	fmt.Fprintln(c.stdin, "")
	c.stdin.Close()
	c.mu.Unlock()

	// Wait for readLoop with timeout — force kill if tmux hangs
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
		c.cmd.Process.Kill()
		<-c.done
	}
	return c.cmd.Wait()
}

// PasteToPane is not supported via control mode because it requires file I/O
// (load-buffer) that is not available through the control mode stdin protocol.
func (c *ControlClient) PasteToPane(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("PasteToPane not supported via control mode")
}

func (c *ControlClient) readLoop() {
	defer close(c.done)
	defer func() {
		c.mu.Lock()
		c.closed = true
		// Drain any pending queries so callers don't hang.
		for _, pq := range c.queryQueue {
			close(pq.ch)
		}
		c.queryQueue = nil
		c.mu.Unlock()
	}()

	var (
		inBlock  bool
		blockNum string
		blockBuf []string
		blockPQ  *pendingQuery // the query this block belongs to
		isError  bool
	)

	for c.scanner.Scan() {
		line := c.scanner.Text()

		// Inside a %begin/%end response block: accumulate lines.
		if inBlock {
			ev := ParseControlLine(line)
			if (ev.Type == EventEnd || ev.Type == EventError) && extractNum(ev.Data) == blockNum {
				if blockPQ != nil {
					blockPQ.ch <- queryResult{lines: blockBuf, err: isError || ev.Type == EventError}
				}
				inBlock = false
				blockPQ = nil
				blockBuf = nil
				continue
			}
			blockBuf = append(blockBuf, line)
			continue
		}

		ev := ParseControlLine(line)
		switch ev.Type {
		case EventOutput:
			c.mu.Lock()
			fn := c.onOutput
			c.mu.Unlock()
			if fn != nil {
				fn(ev.PaneID)
			}
		case EventBegin:
			blockNum = extractNum(ev.Data)
			inBlock = true
			isError = false
			blockBuf = nil
			// Pop the oldest pending query (FIFO — control mode is sequential).
			c.mu.Lock()
			if len(c.queryQueue) > 0 {
				pq := c.queryQueue[0]
				c.queryQueue = c.queryQueue[1:]
				blockPQ = &pq
			} else {
				blockPQ = nil // unsolicited response (e.g. from attach)
			}
			c.mu.Unlock()
		default:
			// Ignore other notifications.
		}
	}
}

// extractNum extracts the command number from a %begin/%end data string.
// Format: "<time> <num> <flags>"
func extractNum(data string) string {
	parts := strings.Fields(data)
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// Query sends a tmux command through the control mode connection and
// returns the response lines. This avoids spawning a subprocess.
// Control mode processes commands sequentially, so concurrent Query
// calls are serialized by the mutex on write and the FIFO on read.
func (c *ControlClient) Query(ctx context.Context, command string) (string, error) {
	ch := make(chan queryResult, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return "", fmt.Errorf("control client closed")
	}
	// Enqueue before writing to guarantee readLoop sees our entry
	// before the %begin arrives.
	c.queryQueue = append(c.queryQueue, pendingQuery{ch: ch})
	_, err := fmt.Fprintf(c.stdin, "%s\n", command)
	if err != nil {
		// Remove the entry we just added.
		c.queryQueue = c.queryQueue[:len(c.queryQueue)-1]
		c.mu.Unlock()
		return "", fmt.Errorf("write command: %w", err)
	}
	c.mu.Unlock()

	select {
	case res, ok := <-ch:
		if !ok {
			return "", fmt.Errorf("control client closed during query")
		}
		if res.err {
			return "", fmt.Errorf("tmux error: %s", strings.Join(res.lines, "\n"))
		}
		return strings.Join(res.lines, "\n"), nil
	case <-ctx.Done():
		// Remove our entry from the queue to prevent FIFO misalignment.
		// If readLoop already popped it, the channel is buffered-1 so
		// we drain it to avoid a leaked goroutine.
		c.mu.Lock()
		for i, pq := range c.queryQueue {
			if pq.ch == ch {
				c.queryQueue = append(c.queryQueue[:i], c.queryQueue[i+1:]...)
				break
			}
		}
		c.mu.Unlock()
		select {
		case <-ch:
		default:
		}
		return "", ctx.Err()
	}
}
