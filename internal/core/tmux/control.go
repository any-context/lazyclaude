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
	done     chan struct{}
	closed   bool
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

// validateControlArg rejects strings that could inject tmux commands.
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
		c.mu.Unlock()
	}()
	for c.scanner.Scan() {
		line := c.scanner.Text()
		ev := ParseControlLine(line)
		if ev.Type == EventOutput {
			c.mu.Lock()
			fn := c.onOutput
			c.mu.Unlock()
			if fn != nil {
				fn(ev.PaneID)
			}
		}
	}
}
