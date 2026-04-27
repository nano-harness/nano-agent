package eventsource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/daemon"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/tools"
	"github.com/gorilla/websocket"
)

const wsWriteTimeout = 10 * time.Second

type DaemonWS struct {
	client   *daemon.Client
	session  string
	teamName string

	mu       sync.Mutex
	state    ConnectionState
	sinceSeq int64
	inbound  chan Inbound
	conn     *websocket.Conn
	cancel   context.CancelFunc
	closed   bool
}

func NewDaemonWS(client *daemon.Client, sessionID, teamName string, sinceSeq int64) *DaemonWS {
	return &DaemonWS{
		client:   client,
		session:  strings.TrimSpace(sessionID),
		teamName: strings.TrimSpace(teamName),
		sinceSeq: sinceSeq,
		state:    StateDisconnected,
		inbound:  make(chan Inbound, 256),
	}
}

func (s *DaemonWS) Start(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("eventsource: daemon client is required")
	}
	if s.session == "" {
		return fmt.Errorf("eventsource: session_id is required")
	}
	childCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.closed = false
	s.mu.Unlock()
	go s.loop(childCtx)
	return nil
}

func (s *DaemonWS) Inbound() <-chan Inbound { return s.inbound }

func (s *DaemonWS) Send(o Outbound) error {
	switch strings.TrimSpace(o.Kind) {
	case "submit":
		return s.writeSubmit(o.Text)
	case "cancel":
		_, err := s.client.CancelSession(s.session)
		if s.teamName != "" {
			_, err = s.client.CancelTeamLeadSession(s.session)
		}
		return err
	case "approval":
		if o.Approval == nil {
			return fmt.Errorf("eventsource: approval decision is required")
		}
		return s.writeJSON(map[string]interface{}{
			"type":         "tool_approval",
			"call_id":      o.Approval.CallID,
			"approved":     o.Approval.Allow,
			"always_allow": o.Approval.Always,
		})
	case "control":
		return s.control(o.Control)
	default:
		return fmt.Errorf("eventsource: unknown outbound kind %q", o.Kind)
	}
}

func (s *DaemonWS) State() ConnectionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *DaemonWS) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.setStateLocked(StateClosed)
	close(s.inbound)
	s.mu.Unlock()
	return nil
}

func (s *DaemonWS) Describe() string {
	if s.teamName != "" {
		return fmt.Sprintf("daemon team:%s session:%s", s.teamName, s.session)
	}
	return "daemon session:" + s.session
}

func (s *DaemonWS) loop(ctx context.Context) {
	delays := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}
	attempt := 0
	for {
		if ctx.Err() != nil || s.isClosed() {
			return
		}
		if attempt == 0 {
			s.setState(StateConnecting)
		} else {
			s.setState(StateReconnecting)
		}
		url, err := s.streamURL()
		if err != nil {
			s.emit(Inbound{Notice: err.Error()})
			return
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
		if err != nil {
			if !s.waitReconnect(ctx, delays, attempt) {
				return
			}
			attempt++
			continue
		}
		s.mu.Lock()
		s.conn = conn
		s.mu.Unlock()
		s.setState(StateConnected)
		since := s.currentSeq()
		if err := s.writeSubscribe(since); err != nil {
			_ = conn.Close()
			attempt++
			continue
		}
		if attempt > 0 {
			s.emit(Inbound{ResumedFrom: since, Notice: fmt.Sprintf("已重连，自 seq=%d 续传事件", since)})
		}
		attempt = 0
		if s.readLoop(ctx, conn) {
			return
		}
		attempt++
	}
}

func (s *DaemonWS) readLoop(ctx context.Context, conn *websocket.Conn) bool {
	for {
		if ctx.Err() != nil || s.isClosed() {
			_ = conn.Close()
			return true
		}
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			_ = conn.Close()
			return false
		}
		if seq := seqFromMessage(msg); seq > 0 {
			s.mu.Lock()
			if seq > s.sinceSeq {
				s.sinceSeq = seq
			}
			s.mu.Unlock()
		}
		ev := streamEventFromMessage(msg)
		s.emit(Inbound{Event: &ev})
	}
}

func (s *DaemonWS) streamURL() (string, error) {
	if s.teamName != "" {
		return s.client.TeamLeadStreamURL(s.session)
	}
	return s.client.StreamURL()
}

func (s *DaemonWS) writeSubscribe(since int64) error {
	payload := map[string]interface{}{"type": "subscribe", "since_seq": since}
	if s.teamName == "" {
		payload["session_id"] = s.session
	}
	return s.writeJSON(payload)
}

func (s *DaemonWS) writeSubmit(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if s.teamName != "" {
		return s.writeJSON(map[string]interface{}{"type": "lead_input", "command": text, "since_seq": s.currentSeq()})
	}
	return s.writeJSON(map[string]interface{}{"type": "command", "session_id": s.session, "command": text})
}

func (s *DaemonWS) writeJSON(v interface{}) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("eventsource: websocket is not connected")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	err := conn.WriteJSON(v)
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func (s *DaemonWS) control(cmd string) error {
	switch strings.TrimSpace(cmd) {
	case "/cancel":
		return s.Send(Outbound{Kind: "cancel"})
	case "/clear", "/reset":
		_, err := s.client.ResetSession(s.session)
		return err
	case "/sessions":
		resp, err := s.client.ListSessions(50)
		if err != nil {
			return err
		}
		var b strings.Builder
		b.WriteString("会话列表:")
		for _, sess := range resp.Sessions {
			b.WriteString("\n- ")
			b.WriteString(sess.ID)
			if sess.Type != "" {
				b.WriteString(" (")
				b.WriteString(sess.Type)
				b.WriteString(")")
			}
		}
		s.emit(Inbound{Notice: b.String()})
		return nil
	default:
		return fmt.Errorf("eventsource: unknown control %q", cmd)
	}
}

func (s *DaemonWS) waitReconnect(ctx context.Context, delays []time.Duration, attempt int) bool {
	if attempt >= len(delays) {
		attempt = len(delays) - 1
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delays[attempt]):
		return true
	}
}

func (s *DaemonWS) currentSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sinceSeq
}

func (s *DaemonWS) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *DaemonWS) setState(st ConnectionState) {
	s.mu.Lock()
	s.setStateLocked(st)
	s.mu.Unlock()
}

func (s *DaemonWS) setStateLocked(st ConnectionState) {
	s.state = st
	state := st
	select {
	case s.inbound <- Inbound{State: &state}:
	default:
	}
}

func (s *DaemonWS) emit(in Inbound) {
	if s.isClosed() {
		return
	}
	select {
	case s.inbound <- in:
	default:
	}
}

func seqFromMessage(msg map[string]interface{}) int64 {
	for _, key := range []string{"seq", "last_seq"} {
		switch v := msg[key].(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case int:
			return int64(v)
		}
	}
	return 0
}

func streamEventFromMessage(msg map[string]interface{}) event.StreamEvent {
	b, _ := json.Marshal(msg)
	var ev event.StreamEvent
	_ = json.Unmarshal(b, &ev)
	if ev.Type == "" {
		if typ, _ := msg["type"].(string); typ != "" {
			ev.Type = event.EventType(typ)
		}
	}
	if calls, ok := msg["tool_calls"].([]interface{}); ok && len(calls) > 0 {
		for _, raw := range calls {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			call := &tools.ToolCall{}
			call.ID, _ = m["id"].(string)
			call.Name, _ = m["name"].(string)
			call.Arguments, _ = m["arguments"].(map[string]interface{})
			ev.ToolCalls = append(ev.ToolCalls, call)
		}
	}
	return ev
}
