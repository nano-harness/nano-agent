package permission

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
)

// stubTool implements the minimal subset of interfaces.Tool used by the manager.
type stubTool struct {
	name            string
	requiresConfirm bool
	category        interfaces.ToolCategory
}

func (s *stubTool) Name() string                   { return s.name }
func (s *stubTool) Description() string            { return "" }
func (s *stubTool) Schema() *interfaces.ToolSchema { return &interfaces.ToolSchema{} }
func (s *stubTool) Category() interfaces.ToolCategory {
	return s.category
}
func (s *stubTool) RequiresConfirmation() bool { return s.requiresConfirm }
func (s *stubTool) ConcurrencySafe() bool      { return true }
func (s *stubTool) Execute(context.Context, map[string]interface{}) (*interfaces.ToolResult, error) {
	return nil, errors.New("not implemented")
}

type fakeClassifier struct {
	block   bool
	err     error
	timeout time.Duration
	calls   int
}

func (f *fakeClassifier) Classify(_ context.Context, _ ClassifyRequest) (*ClassifyResult, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &ClassifyResult{ShouldBlock: f.block, Reason: "stub"}, nil
}

func (f *fakeClassifier) Timeout() time.Duration {
	if f.timeout == 0 {
		return time.Second
	}
	return f.timeout
}

func TestModeAutoConsultsClassifier_AllowAndBlock(t *testing.T) {
	tool := &stubTool{name: "write_file", requiresConfirm: true}

	for _, tc := range []struct {
		name        string
		classifier  *fakeClassifier
		wantConfirm bool
	}{
		{"classifier-allows", &fakeClassifier{block: false}, false},
		{"classifier-blocks", &fakeClassifier{block: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(ModeAuto, nil)
			m.SetClassifier(tc.classifier)
			got := m.ShouldConfirm(tool.name, map[string]interface{}{"path": "x"}, tool)
			if got != tc.wantConfirm {
				t.Fatalf("ShouldConfirm = %v, want %v", got, tc.wantConfirm)
			}
			if tc.classifier.calls != 1 {
				t.Fatalf("classifier called %d times, want 1", tc.classifier.calls)
			}
		})
	}
}

func TestModeAutoFallsBackOnClassifierError(t *testing.T) {
	tool := &stubTool{name: "write_file", requiresConfirm: true}
	m := NewManager(ModeAuto, nil)
	m.SetClassifier(&fakeClassifier{err: errors.New("boom")})

	if !m.ShouldConfirm(tool.name, map[string]interface{}{"path": "x"}, tool) {
		t.Fatal("expected confirm when classifier errors and tool requires confirmation")
	}
}

func TestNanoAutoAcceptEnvBypasses(t *testing.T) {
	// NANO_AUTO_ACCEPT is deprecated: it no longer bypasses the permission
	// system. Operators should use --permission-mode=yolo instead.
	tool := &stubTool{name: "write_file", requiresConfirm: true}
	m := NewManager(ModeDefault, nil)
	t.Setenv("NANO_AUTO_ACCEPT", "1")
	// In ModeDefault, write_file.RequiresConfirmation() == true → ShouldConfirm
	// must still return true regardless of NANO_AUTO_ACCEPT.
	if !m.ShouldConfirm(tool.name, nil, tool) {
		t.Fatal("NANO_AUTO_ACCEPT=1 must no longer skip confirmation; use --permission-mode=yolo")
	}
	t.Setenv("NANO_AUTO_ACCEPT", "true")
	if !m.ShouldConfirm(tool.name, nil, tool) {
		t.Fatal("NANO_AUTO_ACCEPT=true must no longer skip confirmation; use --permission-mode=yolo")
	}
	t.Setenv("NANO_AUTO_ACCEPT", "false")
	if !m.ShouldConfirm(tool.name, nil, tool) {
		t.Fatal("expected NANO_AUTO_ACCEPT=false to honour normal flow")
	}
}

func TestCachingClassifierServesRepeatedRequests(t *testing.T) {
	delegate := &fakeClassifier{block: true}
	c := &CachingClassifier{Delegate: delegate, TTL: time.Minute, MaxSize: 16}
	req := ClassifyRequest{ToolName: "write_file", Params: map[string]interface{}{"path": "x"}}

	first, err := c.Classify(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.CachedHit {
		t.Fatal("first call should not be a cache hit")
	}
	second, err := c.Classify(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !second.CachedHit {
		t.Fatal("second call should be a cache hit")
	}
	if delegate.calls != 1 {
		t.Fatalf("delegate called %d times, want 1", delegate.calls)
	}
}

func TestFailClosedClassifierBlocks(t *testing.T) {
	c := &FailClosedClassifier{}
	res, err := c.Classify(context.Background(), ClassifyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.ShouldBlock {
		t.Fatal("fail-closed classifier should block")
	}
}

func TestCyclePermissionModeOrder(t *testing.T) {
	// Sanity check that the Auto mode is part of IsValidMode and the
	// canonical sequence of modes the rest of the codebase advertises.
	for _, m := range []PermissionMode{ModeDefault, ModeAcceptEdits, ModePlan, ModeAuto, ModeYOLO} {
		if !IsValidMode(m) {
			t.Fatalf("%s should be a valid mode", m)
		}
	}
}
