package swarm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/middleware"
	"github.com/stretchr/testify/require"
)

// helper to write a tiny shell hook that records its event into a file
func writeRecordingHook(t *testing.T, dir, name string) (string, string) {
	t.Helper()
	scriptPath := filepath.Join(dir, name+".sh")
	logPath := filepath.Join(dir, name+".log")
	script := "#!/bin/sh\necho \"" + name + "\" >> " + logPath + "\nexit 0\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write hook script: %v", err)
	}
	return scriptPath, logPath
}

func TestSwarmHookDispatcherFiresLifecycleEvents(t *testing.T) {
	dir := t.TempDir()
	startScript, startLog := writeRecordingHook(t, dir, "start")
	stopScript, stopLog := writeRecordingHook(t, dir, "stop")
	idleScript, idleLog := writeRecordingHook(t, dir, "idle")

	hooks := []middleware.Hook{
		{Name: "sub-start", Event: middleware.HookSubagentStart, Pattern: "*", Command: startScript, Enabled: true},
		{Name: "sub-stop", Event: middleware.HookSubagentStop, Pattern: "*", Command: stopScript, Enabled: true},
		{Name: "tm-idle", Event: middleware.HookNotification, Pattern: "*", Command: idleScript, Enabled: true},
	}
	// Hook execution is synchronous, but spawning the shell script can take
	// seconds on a heavily loaded machine (e.g. full `make test` runs). The
	// hookservice default timeout (5s) would kill the script and fail open
	// with a warning, so give this test ample headroom.
	engine := middleware.NewHookEngineWithOptions(hooks, middleware.HookOptions{Timeout: time.Minute})
	d := NewSwarmHookDispatcher(engine)
	if d == nil {
		t.Fatal("dispatcher should not be nil")
	}

	identity := &TeammateIdentity{
		AgentID:        "researcher@test",
		AgentName:      "researcher",
		TeamName:       "test",
		PermissionMode: "default",
	}
	ctx := context.Background()
	if err := d.DispatchSubagentStart(ctx, identity); err != nil {
		t.Fatalf("DispatchSubagentStart: %v", err)
	}
	if err := d.DispatchTeammateIdle(ctx, identity); err != nil {
		t.Fatalf("DispatchTeammateIdle: %v", err)
	}
	if err := d.DispatchSubagentStop(ctx, identity, "success"); err != nil {
		t.Fatalf("DispatchSubagentStop: %v", err)
	}

	// Dispatch is synchronous: when each call returns, its hook script has
	// already run to completion, so the recording logs must exist.
	for _, p := range []string{startLog, idleLog, stopLog} {
		require.FileExists(t, p, "expected hook log %s", p)
	}
}

func TestSwarmHookDispatcherNilEngine(t *testing.T) {
	if d := NewSwarmHookDispatcher(nil); d != nil {
		t.Fatalf("expected nil dispatcher when engine is nil")
	}

	// nil dispatcher methods must be safe.
	var d *SwarmHookDispatcher
	if err := d.DispatchSubagentStart(context.Background(), &TeammateIdentity{}); err != nil {
		t.Fatalf("nil dispatcher should be a no-op: %v", err)
	}
	if err := d.DispatchSubagentStop(context.Background(), &TeammateIdentity{}, "ok"); err != nil {
		t.Fatalf("nil dispatcher should be a no-op: %v", err)
	}
	if err := d.DispatchTeammateIdle(context.Background(), &TeammateIdentity{}); err != nil {
		t.Fatalf("nil dispatcher should be a no-op: %v", err)
	}
}
