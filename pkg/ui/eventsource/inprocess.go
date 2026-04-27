package eventsource

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

type InProcess struct {
	eng       *engine.Engine
	sessionID string
	perm      *permission.Manager

	mu         sync.Mutex
	state      ConnectionState
	inbound    chan Inbound
	ctx        context.Context
	cancel     context.CancelFunc
	turnCancel context.CancelFunc
	closed     bool
}

func NewInProcess(eng *engine.Engine, sessionID string, perm *permission.Manager) *InProcess {
	return &InProcess{
		eng:       eng,
		sessionID: strings.TrimSpace(sessionID),
		perm:      perm,
		state:     StateDisconnected,
		inbound:   make(chan Inbound, 256),
	}
}

func (s *InProcess) Start(ctx context.Context) error {
	if s.eng == nil || s.eng.Agent == nil {
		return fmt.Errorf("eventsource: engine is required")
	}
	if err := s.eng.Start(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("eventsource: source is closed")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.setStateLocked(StateConnected)
	s.mu.Unlock()
	return nil
}

func (s *InProcess) Inbound() <-chan Inbound { return s.inbound }

func (s *InProcess) Send(o Outbound) error {
	switch strings.TrimSpace(o.Kind) {
	case "submit":
		return s.submit(o.Text)
	case "cancel":
		s.cancelTurn()
		return nil
	case "approval":
		if o.Approval == nil {
			return fmt.Errorf("eventsource: approval decision is required")
		}
		return s.eng.Agent.GetToolScheduler().HandleConfirmationResponse(o.Approval.CallID, o.Approval.Allow)
	case "control":
		return s.control(o.Control)
	default:
		return fmt.Errorf("eventsource: unknown outbound kind %q", o.Kind)
	}
}

func (s *InProcess) State() ConnectionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *InProcess) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.turnCancel != nil {
		s.turnCancel()
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.setStateLocked(StateClosed)
	close(s.inbound)
	s.mu.Unlock()
	return nil
}

func (s *InProcess) Describe() string {
	if s.sessionID == "" {
		return "local in-process"
	}
	return "local in-process session:" + s.sessionID
}

func (s *InProcess) submit(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("eventsource: source is closed")
	}
	base := s.ctx
	if base == nil {
		base = context.Background()
	}
	turnCtx, cancel := context.WithCancel(base)
	s.turnCancel = cancel
	sessionID := s.sessionID
	s.mu.Unlock()

	go func() {
		defer cancel()
		err := s.eng.Agent.ProcessStreamWithMultimodalAndSession(turnCtx, sessionID, text, nil, func(e event.StreamEvent) {
			s.emit(Inbound{Event: &e})
		})
		if err != nil && turnCtx.Err() == nil {
			ev := event.StreamEvent{Type: event.EventTypeError, Error: err.Error()}
			s.emit(Inbound{Event: &ev})
		}
	}()
	return nil
}

func (s *InProcess) cancelTurn() {
	s.mu.Lock()
	cancel := s.turnCancel
	s.turnCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *InProcess) control(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	switch cmd {
	case "/cancel":
		s.cancelTurn()
	case "/clear", "/reset":
		newID := s.eng.Agent.StartNewSession()
		s.mu.Lock()
		s.sessionID = newID
		s.mu.Unlock()
		s.emit(Inbound{Notice: "已开启新会话: " + newID})
	case "/sessions":
		infos, err := s.eng.Agent.GetSessionManager().ListStoredSessionInfos()
		if err != nil {
			return err
		}
		var b strings.Builder
		if len(infos) == 0 {
			b.WriteString("暂无已存储会话")
		} else {
			b.WriteString("会话列表:")
			for _, info := range infos {
				b.WriteString("\n- ")
				b.WriteString(info.ID)
			}
		}
		s.emit(Inbound{Notice: b.String()})
	default:
		return fmt.Errorf("eventsource: unknown control %q", cmd)
	}
	return nil
}

func (s *InProcess) emit(in Inbound) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	select {
	case s.inbound <- in:
	default:
		logger.Warn("eventsource: dropping inbound event because UI channel is full")
	}
}

func (s *InProcess) setStateLocked(st ConnectionState) {
	s.state = st
	state := st
	select {
	case s.inbound <- Inbound{State: &state}:
	default:
	}
}
