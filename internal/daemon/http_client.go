package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxErrorBodySize limits how much of an error response body we read.
const maxErrorBodySize = 4096

// HTTPClient implements ClientAPI over HTTP against a lazyclaude daemon.
type HTTPClient struct {
	baseURL string
	token   string
	httpCli *http.Client
}

// NewHTTPClient creates a new daemon HTTP client.
func NewHTTPClient(baseURL, token string) *HTTPClient {
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpCli: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// sessionPath returns an escaped path for a session endpoint.
func sessionPath(id, suffix string) string {
	return "/session/" + url.PathEscape(id) + suffix
}

// --- Session CRUD ---

func (c *HTTPClient) CreateSession(ctx context.Context, req SessionCreateRequest) (*SessionCreateResponse, error) {
	var resp SessionCreateResponse
	if err := c.postJSON(ctx, "/session/create", req, &resp); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &resp, nil
}

func (c *HTTPClient) DeleteSession(ctx context.Context, id string) error {
	return c.delete(ctx, sessionPath(id, ""))
}

func (c *HTTPClient) RenameSession(ctx context.Context, id, newName string) error {
	req := SessionRenameRequest{ID: id, NewName: newName}
	return c.postJSON(ctx, sessionPath(id, "/rename"), req, nil)
}

func (c *HTTPClient) Sessions(ctx context.Context) ([]SessionInfo, error) {
	var resp SessionListResponse
	if err := c.getJSON(ctx, "/sessions", &resp); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return resp.Sessions, nil
}

func (c *HTTPClient) PurgeOrphans(ctx context.Context) (int, error) {
	var resp struct {
		Purged int `json:"purged"`
	}
	if err := c.postJSON(ctx, "/sessions/purge", nil, &resp); err != nil {
		return 0, fmt.Errorf("purge orphans: %w", err)
	}
	return resp.Purged, nil
}

// --- Worktree ---

func (c *HTTPClient) CreateWorktree(ctx context.Context, req WorktreeCreateRequest) (*WorktreeCreateResponse, error) {
	var resp WorktreeCreateResponse
	if err := c.postJSON(ctx, "/worktree/create", req, &resp); err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}
	return &resp, nil
}

func (c *HTTPClient) ResumeWorktree(ctx context.Context, req WorktreeResumeRequest) (*WorktreeResumeResponse, error) {
	var resp WorktreeResumeResponse
	if err := c.postJSON(ctx, "/worktree/resume", req, &resp); err != nil {
		return nil, fmt.Errorf("resume worktree: %w", err)
	}
	return &resp, nil
}

func (c *HTTPClient) ResumeSession(ctx context.Context, req SessionResumeRequest) (*SessionResumeResponse, error) {
	var resp SessionResumeResponse
	if err := c.postJSON(ctx, "/session/resume", req, &resp); err != nil {
		return nil, fmt.Errorf("resume session: %w", err)
	}
	return &resp, nil
}

func (c *HTTPClient) ListWorktrees(ctx context.Context, projectRoot string) ([]WorktreeInfo, error) {
	q := url.Values{"project_root": {projectRoot}}
	p := "/worktrees?" + q.Encode()
	var resp WorktreeListResponse
	if err := c.getJSON(ctx, p, &resp); err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	return resp.Worktrees, nil
}

// --- Messaging ---

func (c *HTTPClient) MsgSend(ctx context.Context, req MsgSendRequest) (*MsgSendResponse, error) {
	var resp MsgSendResponse
	if err := c.postJSON(ctx, "/msg/send", req, &resp); err != nil {
		return nil, fmt.Errorf("msg send: %w", err)
	}
	return &resp, nil
}

func (c *HTTPClient) MsgCreate(ctx context.Context, req MsgCreateRequest) (*MsgCreateResponse, error) {
	var resp MsgCreateResponse
	if err := c.postJSON(ctx, "/msg/create", req, &resp); err != nil {
		return nil, fmt.Errorf("msg create: %w", err)
	}
	return &resp, nil
}

func (c *HTTPClient) MsgSessions(ctx context.Context) (*MsgSessionsResponse, error) {
	var resp MsgSessionsResponse
	if err := c.getJSON(ctx, "/msg/sessions", &resp); err != nil {
		return nil, fmt.Errorf("msg sessions: %w", err)
	}
	return &resp, nil
}

// --- Capture ---

// CaptureScrollback retrieves a range of scrollback lines for a session via
// POST /session/{id}/scrollback. Used by the fullscreen copy-mode path for
// remote sessions.
func (c *HTTPClient) CaptureScrollback(ctx context.Context, req ScrollbackRequest) (*ScrollbackResponse, error) {
	var resp ScrollbackResponse
	if err := c.postJSON(ctx, sessionPath(req.ID, "/scrollback"), req, &resp); err != nil {
		return nil, fmt.Errorf("capture scrollback: %w", err)
	}
	return &resp, nil
}

// HistorySize fetches the pane scrollback history size via
// GET /session/{id}/history-size. Used together with CaptureScrollback by
// the fullscreen copy mode.
func (c *HTTPClient) HistorySize(ctx context.Context, id string) (int, error) {
	var resp HistorySizeResponse
	if err := c.getJSON(ctx, sessionPath(id, "/history-size"), &resp); err != nil {
		return 0, fmt.Errorf("history size: %w", err)
	}
	return resp.Lines, nil
}

// --- System Info ---

func (c *HTTPClient) CWD(ctx context.Context) (string, error) {
	var resp CWDResponse
	if err := c.getJSON(ctx, "/cwd", &resp); err != nil {
		return "", fmt.Errorf("cwd: %w", err)
	}
	return resp.CWD, nil
}

// --- Health / Lifecycle ---

func (c *HTTPClient) Health(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.getJSON(ctx, "/health", &resp); err != nil {
		return nil, fmt.Errorf("health: %w", err)
	}
	return &resp, nil
}

func (c *HTTPClient) Shutdown(ctx context.Context, req ShutdownRequest) error {
	return c.postJSON(ctx, "/shutdown", req, nil)
}

// --- Notifications ---

func (c *HTTPClient) PendingNotifications(ctx context.Context) ([]*ToolNotificationInfo, error) {
	var resp struct {
		Notifications []*ToolNotificationInfo `json:"notifications"`
	}
	if err := c.getJSON(ctx, "/notifications/pending", &resp); err != nil {
		return nil, fmt.Errorf("pending notifications: %w", err)
	}
	return resp.Notifications, nil
}

// SubscribeNotifications opens an SSE stream for real-time events.
// The returned channel emits events until the context is canceled or
// the connection drops. The caller must drain the channel.
func (c *HTTPClient) SubscribeNotifications(ctx context.Context) (<-chan NotificationEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/notifications", nil)
	if err != nil {
		return nil, fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set(AuthHeader, c.token)
	}

	// Use a separate client without timeout for long-lived SSE connections.
	sseClient := &http.Client{}
	resp, err := sseClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SSE connect: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("SSE connect: HTTP %d", resp.StatusCode)
	}

	ch := make(chan NotificationEvent, 32)
	go parseSSEStream(ctx, resp.Body, ch)
	return ch, nil
}

// parseSSEStream reads SSE events from r and sends them to ch.
// Closes ch and r when the context is canceled or the stream ends.
func parseSSEStream(ctx context.Context, r io.ReadCloser, ch chan<- NotificationEvent) {
	defer close(ch)
	defer r.Close()

	scanner := bufio.NewScanner(r)
	var eventType string
	var dataBuf bytes.Buffer

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := scanner.Text()

		if line == "" {
			// Empty line = event boundary. Dispatch if we have data.
			if dataBuf.Len() > 0 {
				var ev NotificationEvent
				if err := json.Unmarshal(dataBuf.Bytes(), &ev); err == nil {
					// Override type from SSE event field if present.
					if eventType != "" {
						ev.Type = NotificationEventType(eventType)
					}
					select {
					case ch <- ev:
					case <-ctx.Done():
						return
					}
				}
				dataBuf.Reset()
				eventType = ""
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(data)
		}
		// Ignore "id:", "retry:", and comment lines starting with ":"
	}
}

// --- HTTP helpers ---

func (c *HTTPClient) getJSON(ctx context.Context, path string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, dest)
}

func (c *HTTPClient) postJSON(ctx context.Context, path string, body, dest interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.doJSON(req, dest)
}

func (c *HTTPClient) delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

func (c *HTTPClient) doJSON(req *http.Request, dest interface{}) error {
	if c.token != "" {
		req.Header.Set(AuthHeader, c.token)
	}
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
