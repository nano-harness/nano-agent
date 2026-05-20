package slash

import "testing"

func TestLocalDispatcherGoal(t *testing.T) {
	var last string
	d := NewLocalDispatcher("", t.TempDir()).WithGoalHandler(func(args string) string {
		last = args
		return "ok:" + args
	})
	if r := d.Dispatch("/goal all tests pass"); !r.Handled || r.Message != "ok:all tests pass" || last != "all tests pass" {
		t.Fatalf("set goal result = %+v, last=%q", r, last)
	}
	if r := d.Dispatch("/goal"); !r.Handled || r.Message != "ok:" || last != "" {
		t.Fatalf("status goal result = %+v, last=%q", r, last)
	}
	if r := d.Dispatch("/goal clear"); !r.Handled || r.Message != "ok:clear" || last != "clear" {
		t.Fatalf("clear goal result = %+v, last=%q", r, last)
	}
}
