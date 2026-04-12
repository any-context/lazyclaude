package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const clientTimeout = 5 * time.Second

// maxErrBody is the maximum number of bytes read from an error response body.
const maxErrBody = 512

// Client is a thin HTTP client for the lazyclaude MCP server API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a client targeting the server at the given port with the given auth token.
func NewClient(port int, token string) *Client {
	return &Client{
		baseURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		token:      token,
		httpClient: &http.Client{Timeout: clientTimeout},
	}
}

// Sessions fetches the session list from GET /msg/sessions.
func (c *Client) Sessions(ctx context.Context) ([]SessionInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/msg/sessions", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Auth-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request /msg/sessions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	var sessions []SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("decode sessions: %w", err)
	}
	return sessions, nil
}

// SendMessage sends a message via POST /msg/send.
func (c *Client) SendMessage(ctx context.Context, from, to, msgType, body string) error {
	payload := msgSendRequest{
		From: from,
		To:   to,
		Type: msgType,
		Body: body,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal send request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/msg/send", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Auth-Token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request /msg/send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// CreateSession creates a new session via POST /msg/create.
func (c *Client) CreateSession(ctx context.Context, from, name, sessionType, prompt string) (*MsgCreateResponse, error) {
	payload := msgCreateRequest{
		From:   from,
		Name:   name,
		Type:   sessionType,
		Prompt: prompt,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal create request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/msg/create", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Auth-Token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request /msg/create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	var result MsgCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode create response: %w", err)
	}
	return &result, nil
}

// ResumeSession resumes a session via POST /msg/resume.
func (c *Client) ResumeSession(ctx context.Context, id, prompt, name string) (*MsgResumeResponse, error) {
	payload := msgResumeRequest{
		ID:     id,
		Prompt: prompt,
		Name:   name,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal resume request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/msg/resume", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Auth-Token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request /msg/resume: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	var result MsgResumeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode resume response: %w", err)
	}
	return &result, nil
}

// readErrorBody reads and truncates the response body for error reporting.
func readErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
	if err != nil {
		return fmt.Sprintf("(body unreadable: %v)", err)
	}
	return string(bytes.TrimSpace(body))
}
