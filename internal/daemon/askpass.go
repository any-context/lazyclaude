package daemon

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// askpassReadTimeout is the maximum time to wait for a client to send
// the prompt string after connecting.
const askpassReadTimeout = 10 * time.Second

// AskpassServer listens on a Unix socket for SSH_ASKPASS requests.
// When an askpass client connects, it reads a prompt string, calls
// the handler to get the user's response (typically via a GUI popup),
// and sends the response back.
//
// Only one handler invocation is active at a time. Concurrent
// connections are serialized by handlerMu to prevent channel
// clobbering in the GUI layer.
type AskpassServer struct {
	sockPath   string
	scriptPath string // wrapper script for SSH_ASKPASS
	listener   net.Listener
	handler    func(prompt string) (string, error)

	handlerMu sync.Mutex // serializes handler invocations
	mu        sync.Mutex
	closed    bool
	conns     map[net.Conn]struct{} // active connections for cleanup
}

// NewAskpassServer creates a new AskpassServer. The handler is called
// for each incoming prompt and should return the user's password or
// an empty string for cancellation.
//
// Socket and script paths include the current PID to avoid collisions
// when multiple lazyclaude instances run concurrently.
func NewAskpassServer(runtimeDir string, handler func(prompt string) (string, error)) *AskpassServer {
	pid := os.Getpid()
	return &AskpassServer{
		sockPath:   filepath.Join(runtimeDir, fmt.Sprintf("askpass-%d.sock", pid)),
		scriptPath: filepath.Join(runtimeDir, fmt.Sprintf("askpass-%d.sh", pid)),
		handler:    handler,
		conns:      make(map[net.Conn]struct{}),
	}
}

// Start begins listening on the Unix socket and accepting connections.
// It also writes the wrapper script that SSH_ASKPASS should point to.
// Each connection is handled in a separate goroutine, but handler
// invocations are serialized.
func (s *AskpassServer) Start() error {
	// Remove stale files.
	os.Remove(s.sockPath)
	os.Remove(s.scriptPath)

	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("listen on askpass socket: %w", err)
	}

	// Set socket permissions to owner-only (0600).
	if err := os.Chmod(s.sockPath, 0o600); err != nil {
		ln.Close()
		os.Remove(s.sockPath)
		return fmt.Errorf("chmod askpass socket: %w", err)
	}

	s.mu.Lock()
	s.listener = ln
	s.closed = false
	s.mu.Unlock()

	go s.acceptLoop()
	return nil
}

// WriteScript writes the SSH_ASKPASS wrapper script that invokes
// "lazyclaude askpass" with the prompt argument. The script path
// is returned by ScriptPath(). binPath must be the absolute path
// to the lazyclaude binary.
func (s *AskpassServer) WriteScript(binPath string) error {
	content := fmt.Sprintf("#!/bin/sh\nexec %s askpass \"$@\"\n", binPath)
	if err := os.WriteFile(s.scriptPath, []byte(content), 0o700); err != nil {
		return fmt.Errorf("write askpass script: %w", err)
	}
	return nil
}

// Stop closes the listener, all active connections, and removes
// the socket and script files.
func (s *AskpassServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true

	if s.listener != nil {
		s.listener.Close()
	}

	// Close all active connections to unblock any blocked handlers.
	for conn := range s.conns {
		conn.Close()
	}
	s.conns = make(map[net.Conn]struct{})

	os.Remove(s.sockPath)
	os.Remove(s.scriptPath)
}

// SockPath returns the Unix socket file path.
func (s *AskpassServer) SockPath() string {
	return s.sockPath
}

// ScriptPath returns the wrapper script file path.
// This is the value to set as SSH_ASKPASS.
func (s *AskpassServer) ScriptPath() string {
	return s.scriptPath
}

func (s *AskpassServer) trackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.conns[conn] = struct{}{}
	}
}

func (s *AskpassServer) untrackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

func (s *AskpassServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			continue
		}
		s.trackConn(conn)
		go s.handleConn(conn)
	}
}

func (s *AskpassServer) handleConn(conn net.Conn) {
	defer func() {
		s.untrackConn(conn)
		conn.Close()
	}()

	// Set a read deadline to prevent goroutine leaks from clients
	// that connect but never send a prompt.
	conn.SetReadDeadline(time.Now().Add(askpassReadTimeout))

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}
	prompt := scanner.Text()

	// Clear the deadline for the handler phase (user interaction
	// may take longer than the read timeout).
	conn.SetDeadline(time.Time{})

	// Serialize handler calls so only one GUI popup is active at a time.
	s.handlerMu.Lock()
	response, err := s.handler(prompt)
	s.handlerMu.Unlock()

	if err != nil {
		// Send empty line to signal cancellation.
		fmt.Fprintln(conn, "")
		return
	}

	fmt.Fprintln(conn, response)
}
