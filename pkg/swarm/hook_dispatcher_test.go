package swarm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/middleware"
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
	engine := middleware.NewHookEngine(hooks)
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

	for _, p := range []string{startLog, idleLog, stopLog} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected hook log %s: %v", p, err)
		}
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
