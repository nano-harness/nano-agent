package slash

import (
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
)

type fakeEventStore struct {
	events []event.StreamEvent
}

func (f fakeEventStore) Since(seq int64, filter func(event.StreamEvent) bool) []event.StreamEvent {
	var out []event.StreamEvent
	for _, ev := range f.events {
		if ev.Seq <= seq {
			continue
		}
		if filter(ev) {
			out = append(out, ev)
		}
	}
	return out
}

func TestDispatcherObserveCommands(t *testing.T) {
	store := fakeEventStore{events: []event.StreamEvent{
		{Seq: 1, Type: event.EventTypeContent, Content: "hello", Source: "agent", SessionID: "s1"},
		{Seq: 2, Type: event.EventTypeError, Error: "boom", Source: "agent", SessionID: "s1"},
	}}
	d := NewLocalDispatcher("", t.TempDir()).
		WithDoctorReporter(BuildDoctorReporter(config.DefaultConfig())).
		WithEventsQuerier(BuildEventsQuerier(store)).
		WithAuditQuerier(BuildAuditQuerier(store))

	for _, tc := range []struct {
		input string
		want  string
	}{
		{"/doctor", "config_loaded: true"},
		{"/events --limit 2", "hello"},
		{"/audit", "boom"},
	} {
		r := d.Dispatch(tc.input)
		if !r.Handled || !strings.Contains(r.Message, tc.want) {
			t.Fatalf("%q: expected %q in result, got %+v", tc.input, tc.want, r)
		}
	}
}
