package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/any-context/lazyclaude/internal/core/model"
)

// handleSSE streams real-time notifications via Server-Sent Events.
// On connect it sends a full_sync event with all session state, then
// streams activity and tool_info events as they arrive from the broker.
func (s *DaemonServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send initial full_sync
	sessions := s.mgr.Sessions()
	infos := make([]SessionInfo, len(sessions))
	for i, sess := range sessions {
		infos[i] = sessionToInfo(sess)
	}
	syncEvent := NotificationEvent{
		ID:       s.nextEventID(),
		Type:     EventFullSync,
		Time:     time.Now(),
		Sessions: infos,
	}
	writeSSEEvent(w, s.log, syncEvent)
	flusher.Flush()

	// Subscribe to broker events
	sub := s.broker.Subscribe(64)
	defer sub.Cancel()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownCh:
			return
		case evt, ok := <-sub.Ch():
			if !ok {
				return
			}
			notif := s.brokerEventToNotification(evt)
			if notif == nil {
				continue
			}
			writeSSEEvent(w, s.log, *notif)
			flusher.Flush()
		}
	}
}

// brokerEventToNotification converts a model.Event into a NotificationEvent.
// Returns nil for events that should not be sent to SSE clients.
func (s *DaemonServer) brokerEventToNotification(evt model.Event) *NotificationEvent {
	switch {
	case evt.ActivityNotification != nil:
		an := evt.ActivityNotification
		return &NotificationEvent{
			ID:        s.nextEventID(),
			Type:      EventActivity,
			Time:      an.Timestamp,
			SessionID: s.sessionIDForWindow(an.Window),
			Activity:  an.State,
			ToolName:  an.ToolName,
		}
	case evt.Notification != nil:
		n := evt.Notification
		return &NotificationEvent{
			ID:        s.nextEventID(),
			Type:      EventToolInfo,
			Time:      n.Timestamp,
			SessionID: s.sessionIDForWindow(n.Window),
			ToolNotification: &model.ToolNotification{
				ToolName:    n.ToolName,
				Input:       n.Input,
				CWD:         n.CWD,
				Window:      n.Window,
				Timestamp:   n.Timestamp,
				MaxOption:   n.MaxOption,
				OldFilePath: n.OldFilePath,
				NewContents: n.NewContents,
			},
		}
	case evt.StopNotification != nil:
		sn := evt.StopNotification
		state := model.ActivityIdle
		if sn.StopReason == "error" || sn.StopReason == "interrupt" {
			state = model.ActivityError
		}
		return &NotificationEvent{
			ID:        s.nextEventID(),
			Type:      EventActivity,
			Time:      sn.Timestamp,
			SessionID: s.sessionIDForWindow(sn.Window),
			Activity:  state,
		}
	case evt.SessionStartNotification != nil:
		ssn := evt.SessionStartNotification
		return &NotificationEvent{
			ID:        s.nextEventID(),
			Type:      EventActivity,
			Time:      ssn.Timestamp,
			SessionID: s.sessionIDForWindow(ssn.Window),
			Activity:  model.ActivityRunning,
		}
	case evt.PromptSubmitNotification != nil:
		psn := evt.PromptSubmitNotification
		return &NotificationEvent{
			ID:        s.nextEventID(),
			Type:      EventActivity,
			Time:      psn.Timestamp,
			SessionID: s.sessionIDForWindow(psn.Window),
			Activity:  model.ActivityRunning,
		}
	default:
		return nil
	}
}

// sessionIDForWindow resolves a tmux window identifier to the
// canonical session UUID from the daemon's session store. The input
// may be a tmux window ID (e.g. "@22"), a canonical window name
// (e.g. "lc-abcd1234"), or empty.
//
// Background: ActivityNotification.Window is populated by the MCP
// server from resolveNotifyWindow, which returns whatever tmux reports
// for the PID — on this code path that is the raw window ID "@22".
// Local GUI subscribers to the SSE stream key by the session UUID
// (they look up the local mirror session to find its TmuxWindow for
// the sidebar activity map), so we must translate the window back to
// the UUID here rather than passing the raw window ID through. See
// Bug 4 for the full trace.
//
// On miss (no matching session) the function returns an empty string
// so downstream matching fails cleanly rather than accidentally
// matching a different session by the stray window ID.
func (s *DaemonServer) sessionIDForWindow(window string) string {
	if window == "" {
		return ""
	}
	sessions := s.mgr.Sessions()
	for _, sess := range sessions {
		if sess.TmuxWindow == window || sess.WindowName() == window {
			return sess.ID
		}
	}
	// Fallback: accept a short "lc-<8>" hint by matching against the
	// prefix of the session UUID. Historically some callers emit the
	// window as "lc-xxxx" where xxxx is already the first 8 chars of
	// the UUID.
	if after, ok := strings.CutPrefix(window, "lc-"); ok && after != "" {
		for _, sess := range sessions {
			if strings.HasPrefix(sess.ID, after) {
				return sess.ID
			}
		}
	}
	return ""
}

func (s *DaemonServer) nextEventID() string {
	return fmt.Sprintf("%d", s.sseEventID.Add(1))
}

func writeSSEEvent(w http.ResponseWriter, logger *log.Logger, evt NotificationEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		logger.Printf("sse: marshal event: %v", err)
		return
	}
	fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", evt.ID, evt.Type, data)
}
