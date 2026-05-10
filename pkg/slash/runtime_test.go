package slash

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/sandbox"
)

func TestCommandRuntimeExecutesPreludeThroughSandbox(t *testing.T) {
	rt := &recordingSandboxRuntime{}
	runtime := NewCommandRuntime(t.TempDir(), rt)

	results, err := runtime.ExecutePrelude(context.Background(), Command{
		Name:                  "preflight",
		Prelude:               []string{"echo ok"},
		PreludeTimeoutSeconds: 3,
		PreludeOnError:        "abort",
		PreludeOutput:         "full",
	})
	if err != nil {
		t.Fatalf("ExecutePrelude returned error: %v", err)
	}
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("unexpected results: %#v", results)
	}
	if rt.prepareCount != 1 || rt.cleanupCount != 1 {
		t.Fatalf("sandbox prepare/cleanup = %d/%d, want 1/1", rt.prepareCount, rt.cleanupCount)
	}
	if rt.lastReq.Metadata["tool"] != "slash_command_prelude" || rt.lastReq.Metadata["slash_command"] != "preflight" {
		t.Fatalf("unexpected sandbox metadata: %#v", rt.lastReq.Metadata)
	}
}

type recordingSandboxRuntime struct {
	prepareCount int
	cleanupCount int
	lastReq      sandbox.SandboxRequest
}

func (r *recordingSandboxRuntime) PrepareCommand(_ context.Context, req sandbox.SandboxRequest) (*sandbox.SandboxEnvironment, error) {
	r.prepareCount++
	r.lastReq = req
	return &sandbox.SandboxEnvironment{
		Backend:    sandbox.BackendNone,
		Command:    req.Command,
		Args:       req.Args,
		WorkingDir: req.WorkingDir,
		Metadata:   req.Metadata,
	}, nil
}

func (r *recordingSandboxRuntime) Cleanup(_ context.Context, _ *sandbox.SandboxEnvironment) error {
	r.cleanupCount++
	return nil
}
