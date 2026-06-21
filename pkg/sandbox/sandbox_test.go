package sandbox

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func cfg(enabled bool) *config.SandboxConfig {
	return &config.SandboxConfig{Enabled: enabled, NetworkAccess: true}
}

// ── PathChecker tests ─────────────────────────────────────────────────────────

func TestPathChecker_DisabledAllowsAll(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("default blocked paths use Unix-style paths; skipping on Windows")
	}
	pc := NewPathChecker(cfg(false))
	// With sandbox disabled, non-sensitive paths should be allowed
	if err := pc.Check("/tmp/test.txt", OpRead); err != nil {
		t.Fatalf("disabled checker should allow non-sensitive paths: %v", err)
	}
	// Default blacklist is ALWAYS enforced, even when sandbox is disabled
	if err := pc.Check("/etc/passwd", OpRead); err == nil {
		t.Fatal("default blacklist should block /etc/passwd even when sandbox is disabled")
	}
}

func TestPathChecker_NilAllowsNonSensitivePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("default blocked paths use Unix-style paths; skipping on Windows")
	}
	pc := NewPathChecker(nil)
	// /etc/hostname is not in the default blacklist, so nil config should allow it.
	if err := pc.Check("/etc/hostname", OpRead); err != nil {
		t.Fatalf("nil config should allow non-sensitive paths: %v", err)
	}
	// Default blacklist is always enforced: /etc/passwd should be blocked.
	if err := pc.Check("/etc/passwd", OpRead); err == nil {
		t.Fatal("expected /etc/passwd to be blocked by default blacklist")
	}
}

func TestPathChecker_BlockedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix-style absolute paths; skipping on Windows")
	}
	c := cfg(true)
	c.BlockedPaths = []string{"/etc", "/sys"}
	pc := NewPathChecker(c)

	if err := pc.Check("/etc/hosts", OpRead); err == nil {
		t.Fatal("expected blocked path error for /etc/hosts")
	}
	if err := pc.Check("/sys/kernel", OpRead); err == nil {
		t.Fatal("expected blocked path error for /sys/kernel")
	}
	// Non-blocked path should be allowed.
	if err := pc.Check("/tmp/foo.txt", OpRead); err != nil {
		t.Fatalf("unexpected error for allowed path: %v", err)
	}
}

func TestPathChecker_AllowedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix-style absolute paths; skipping on Windows")
	}
	c := cfg(true)
	c.AllowedPaths = []string{"/tmp", "/workspace"}
	pc := NewPathChecker(c)

	if err := pc.Check("/tmp/file", OpRead); err != nil {
		t.Fatalf("expected /tmp/file to be allowed: %v", err)
	}
	if err := pc.Check("/etc/hosts", OpRead); err == nil {
		t.Fatal("expected /etc/hosts to be denied (not in allowed list)")
	}
}

func TestPathChecker_ReadOnlyPaths(t *testing.T) {
	c := cfg(true)
	c.ReadOnlyPaths = []string{"/data/readonly"}
	pc := NewPathChecker(c)

	// Read should succeed.
	if err := pc.Check("/data/readonly/file", OpRead); err != nil {
		t.Fatalf("read on read-only path should be allowed: %v", err)
	}
	// Write should fail.
	if err := pc.Check("/data/readonly/file", OpWrite); err == nil {
		t.Fatal("write on read-only path should be denied")
	}
	// Delete should fail.
	if err := pc.Check("/data/readonly/file", OpDelete); err == nil {
		t.Fatal("delete on read-only path should be denied")
	}
}

func TestPathChecker_RelativePath(t *testing.T) {
	c := cfg(true)
	c.AllowedPaths = []string{"/tmp"}
	pc := NewPathChecker(c)

	// Relative paths should be resolved (most will be outside /tmp).
	// We just verify the call doesn't panic.
	_ = pc.Check("relative/path", OpRead)
}

// ── A8: PathChecker.CheckAndResolve ──────────────────────────────────────────
// CheckAndResolve narrows the TOCTOU window by returning the already-resolved
// (symlink-evaluated) canonical path that was used for the policy check so
// callers can open that path directly.

// TestCheckAndResolve_AllowedPathReturnsResolved verifies that CheckAndResolve
// returns the symlink-resolved absolute path on a successful (allowed) check.
func TestCheckAndResolve_AllowedPathReturnsResolved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink resolution uses Unix paths; skipping on Windows")
	}
	c := cfg(true)
	tmp := t.TempDir()
	c.AllowedPaths = []string{tmp}
	pc := NewPathChecker(c)

	target := tmp + "/file.txt"
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatalf("create file: %v", err)
	}

	resolved, err := pc.CheckAndResolve(target, OpRead)
	if err != nil {
		t.Fatalf("CheckAndResolve: unexpected error: %v", err)
	}
	if resolved == "" {
		t.Fatal("CheckAndResolve returned empty resolved path")
	}
	// The resolved path must be absolute and contain the filename.
	if !strings.Contains(resolved, "file.txt") {
		t.Errorf("resolved path %q does not contain filename", resolved)
	}
}

// TestCheckAndResolve_SymlinkResolved verifies that CheckAndResolve resolves
// symlinks and uses the canonical target for the policy decision.
func TestCheckAndResolve_SymlinkResolved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privilege on Windows; skipping")
	}
	tmp := t.TempDir()
	c := cfg(true)
	c.AllowedPaths = []string{tmp}
	pc := NewPathChecker(c)

	// Create a real file and a symlink pointing to it.
	realFile := tmp + "/real.txt"
	if err := os.WriteFile(realFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("create real file: %v", err)
	}
	link := tmp + "/link.txt"
	if err := os.Symlink(realFile, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	resolved, err := pc.CheckAndResolve(link, OpRead)
	if err != nil {
		t.Fatalf("CheckAndResolve on symlink: unexpected error: %v", err)
	}
	// The resolved path should point to the real file, not the symlink.
	if resolved == link {
		t.Error("CheckAndResolve should resolve symlinks; got the symlink path unchanged")
	}
	if !strings.Contains(resolved, "real.txt") {
		t.Errorf("resolved path %q should reference real.txt", resolved)
	}
}

// TestCheckAndResolve_BlockedPathReturnsError verifies that CheckAndResolve
// propagates a policy denial as an error (same behaviour as Check).
func TestCheckAndResolve_BlockedPathReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("default blocked paths use Unix-style paths; skipping on Windows")
	}
	c := cfg(true)
	c.BlockedPaths = []string{"/etc"}
	pc := NewPathChecker(c)

	resolved, err := pc.CheckAndResolve("/etc/hosts", OpRead)
	if err == nil {
		t.Fatalf("CheckAndResolve on blocked path must return error; got resolved=%q", resolved)
	}
	if resolved != "" {
		t.Errorf("on error resolved path must be empty; got %q", resolved)
	}
}

// TestCheckAndResolve_DefaultBlocklistEnforced verifies that the hardcoded
// default blocklist (/etc/passwd, /etc/shadow, /etc/master.passwd) is enforced
// by CheckAndResolve even when the sandbox config has no explicit BlockedPaths.
func TestCheckAndResolve_DefaultBlocklistEnforced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("default blocked paths use Unix-style paths; skipping on Windows")
	}
	pc := NewPathChecker(cfg(false)) // sandbox disabled — default blocklist still applies

	for _, p := range []string{"/etc/passwd", "/etc/shadow"} {
		if resolved, err := pc.CheckAndResolve(p, OpRead); err == nil {
			t.Errorf("CheckAndResolve(%q) should be blocked by default blocklist; got resolved=%q", p, resolved)
		}
	}
}

// TestCheckAndResolve_ReadOnlyPathBlocksWrite verifies that a path in
// ReadOnlyPaths is allowed for reads but denied for writes.
func TestCheckAndResolve_ReadOnlyPathBlocksWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix paths; skipping on Windows")
	}
	tmp := t.TempDir()
	c := cfg(true)
	c.AllowedPaths = []string{tmp}
	c.ReadOnlyPaths = []string{tmp + "/ro"}
	pc := NewPathChecker(c)

	roDir := tmp + "/ro"
	if err := os.MkdirAll(roDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := roDir + "/file.txt"
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("create file: %v", err)
	}

	// Read must succeed and return the resolved path.
	resolved, err := pc.CheckAndResolve(target, OpRead)
	if err != nil {
		t.Fatalf("read on read-only path should be allowed: %v", err)
	}
	if resolved == "" {
		t.Fatal("resolved path must not be empty on success")
	}

	// Write must be denied.
	if _, err := pc.CheckAndResolve(target, OpWrite); err == nil {
		t.Error("write on read-only path must be denied")
	}
}

// ── NoopSandbox ───────────────────────────────────────────────────────────────

func TestNoopSandbox_PassesThrough(t *testing.T) {
	s := &NoopSandbox{}
	gotCmd, gotArgs, err := s.WrapCommand("/work", "sh", []string{"-c", "echo hi"})
	if err != nil {
		t.Fatal(err)
	}
	if gotCmd != "sh" {
		t.Errorf("expected 'sh', got %q", gotCmd)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "-c" || gotArgs[1] != "echo hi" {
		t.Errorf("unexpected args: %v", gotArgs)
	}
	if s.IsEnabled() {
		t.Error("NoopSandbox.IsEnabled() should return false")
	}
}

// ── New() factory ─────────────────────────────────────────────────────────────

func TestNew_DisabledReturnsNoop(t *testing.T) {
	s := New(cfg(false), "/tmp")
	if s.IsEnabled() {
		t.Error("disabled sandbox should return NoopSandbox")
	}
}

func TestNew_NilReturnsNoop(t *testing.T) {
	s := New(nil, "/tmp")
	if s.IsEnabled() {
		t.Error("nil config should return NoopSandbox")
	}
}

func TestNew_EnabledReturnsPlatformSandbox(t *testing.T) {
	s := New(cfg(true), "/tmp")
	switch runtime.GOOS {
	case "linux", "darwin":
		// On supported platforms the sandbox should be enabled.
		// (bwrap might not be installed in CI; NoopSandbox is acceptable fallback.)
		_ = s.IsEnabled()
	default:
		// Other platforms always noop.
		if s.IsEnabled() {
			t.Errorf("unsupported platform %q should return NoopSandbox", runtime.GOOS)
		}
	}
}

// ── SandboxRuntime adapter ───────────────────────────────────────────────────

func TestRuntime_DisabledPreparesNoopEnvironment(t *testing.T) {
	rt := NewRuntime(cfg(false), "/workspace")
	env, err := rt.PrepareCommand(context.Background(), SandboxRequest{
		Command:    "sh",
		Args:       []string{"-c", "echo hi"},
		WorkingDir: "/workspace",
		Env:        []string{"NANO_TOOL_INPUT={}", "SECRET_TOKEN=hidden"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if env.Enabled {
		t.Fatal("disabled runtime should not enable process isolation")
	}
	if env.Backend != BackendNone || env.BackendDetail != "none" {
		t.Fatalf("unexpected backend: %s/%s", env.Backend, env.BackendDetail)
	}
	if env.Command != "sh" || len(env.Args) != 2 || env.Args[1] != "echo hi" {
		t.Fatalf("unexpected prepared command: %s %v", env.Command, env.Args)
	}
	if env.Network != NetworkInherited {
		t.Fatalf("disabled runtime network = %s, want %s", env.Network, NetworkInherited)
	}
}

func TestRuntime_RecordsNetworkMountsAndFallback(t *testing.T) {
	c := cfg(true)
	c.NetworkAccess = false
	c.ExtraReadOnlyPaths = []string{"/readonly"}
	c.ExtraWritablePaths = []string{"/writable"}
	rt := NewRuntime(c, "/workspace")
	env, err := rt.PrepareCommand(context.Background(), SandboxRequest{
		Command:    "sh",
		Args:       []string{"-c", "echo hi"},
		WorkingDir: "/workspace",
		Env:        []string{"NANO_TOOL_INPUT={}", "SECRET_TOKEN=hidden"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if env.Network != NetworkDenied {
		t.Fatalf("network = %s, want %s", env.Network, NetworkDenied)
	}
	if len(env.Mounts) != 3 {
		t.Fatalf("mount count = %d, want 3: %#v", len(env.Mounts), env.Mounts)
	}
	if env.Mounts[0].HostPath != "/workspace" || env.Mounts[0].Mode != MountReadWrite {
		t.Fatalf("workspace mount not recorded as rw: %#v", env.Mounts[0])
	}
	if !env.Enabled {
		if fallback, _ := env.Metadata["fallback"].(bool); !fallback {
			t.Fatalf("expected fallback metadata when enabled config falls back to noop: %#v", env.Metadata)
		}
	}
}

func TestRuntime_DockerBackendPreparesOneShotContainer(t *testing.T) {
	c := cfg(true)
	c.Backend = string(BackendDocker)
	c.NetworkAccess = false
	c.DockerImage = "nano-test:latest"
	c.ExtraReadOnlyPaths = []string{"/readonly"}

	rt := NewRuntime(c, "/workspace")
	env, err := rt.PrepareCommand(context.Background(), SandboxRequest{
		Command:    "sh",
		Args:       []string{"-c", "echo hi"},
		WorkingDir: "/workspace",
		Env:        []string{"NANO_TOOL_INPUT={}", "SECRET_TOKEN=hidden"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if env.Backend != BackendDocker || !env.Enabled {
		t.Fatalf("backend = %s enabled=%v, want docker enabled", env.Backend, env.Enabled)
	}
	if env.Command != "docker" {
		t.Fatalf("command = %q, want docker", env.Command)
	}
	if !containsStr(env.Args, "--network") || !containsStr(env.Args, "none") {
		t.Fatalf("docker args missing network none: %#v", env.Args)
	}
	if !containsStr(env.Args, "/workspace:/workspace:rw") {
		t.Fatalf("docker args missing workspace mount: %#v", env.Args)
	}
	if !containsStr(env.Args, "/readonly:/readonly:ro") {
		t.Fatalf("docker args missing readonly mount: %#v", env.Args)
	}
	if !containsStr(env.Args, "nano-test:latest") {
		t.Fatalf("docker args missing image: %#v", env.Args)
	}
	if !containsStr(env.Args, "NANO_TOOL_INPUT={}") {
		t.Fatalf("docker args missing nano env: %#v", env.Args)
	}
	if containsStr(env.Args, "SECRET_TOKEN=hidden") {
		t.Fatalf("docker args leaked non-NANO env: %#v", env.Args)
	}
}

func TestRuntime_DockerBackendPreparesSessionContainer(t *testing.T) {
	c := cfg(true)
	c.Backend = string(BackendDocker)
	c.NetworkAccess = false
	c.DockerLifecycle = "session"
	c.DockerImage = "nano-test:latest"

	rt := NewRuntime(c, "/workspace")
	env, err := rt.PrepareCommand(context.Background(), SandboxRequest{
		Command:    "sh",
		Args:       []string{"-c", "echo hi"},
		WorkingDir: "/workspace",
		Env:        []string{"NANO_TOOL_INPUT={}", "SECRET_TOKEN=hidden"},
		Metadata: map[string]interface{}{
			"sandbox_session_id": "sess-123",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if env.Command != "sh" || len(env.Args) != 2 || env.Args[0] != "-c" {
		t.Fatalf("persistent command = %q %#v, want sh -c", env.Command, env.Args)
	}
	script := env.Args[1]
	for _, want := range []string{"docker inspect", "'docker' 'run'", "--name", "'docker' 'exec'", "nano-session-sess-123"} {
		if !strings.Contains(script, want) {
			t.Fatalf("persistent script missing %q: %s", want, script)
		}
	}
	if strings.Contains(script, "--rm") {
		t.Fatalf("persistent script should not use --rm: %s", script)
	}
	if strings.Contains(script, "SECRET_TOKEN=hidden") {
		t.Fatalf("persistent script leaked non-NANO env: %s", script)
	}
	if env.Metadata["lifecycle"] != "session" || env.Metadata["container_name"] == "" {
		t.Fatalf("missing lifecycle metadata: %#v", env.Metadata)
	}
}

func TestRuntime_PublishesSandboxEvents(t *testing.T) {
	c := cfg(true)
	c.Backend = string(BackendDocker)
	pub := &recordingPublisher{}
	rt := NewRuntimeWithEventPublisher(c, "/workspace", pub)

	env, err := rt.PrepareCommand(context.Background(), SandboxRequest{
		Command:    "sh",
		Args:       []string{"-c", "echo hi"},
		WorkingDir: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Cleanup(context.Background(), env); err != nil {
		t.Fatal(err)
	}

	want := []string{
		EventTypeSandboxDecisionCreated,
		EventTypeSandboxEnvironmentCreated,
		EventTypeSandboxEnvironmentCleaned,
	}
	if len(pub.events) != len(want) {
		t.Fatalf("published %d events, want %d: %#v", len(pub.events), len(want), pub.events)
	}
	for i, wantType := range want {
		if pub.events[i].Type != wantType {
			t.Fatalf("event %d type = %s, want %s", i, pub.events[i].Type, wantType)
		}
		if pub.events[i].Metadata["sandbox"] == nil {
			t.Fatalf("event %d missing sandbox metadata: %#v", i, pub.events[i])
		}
	}
}

type recordingPublisher struct {
	events []Event
}

func (p *recordingPublisher) PublishSandboxEvent(ev Event) {
	p.events = append(p.events, ev)
}

// ── Linux BwrapSandbox (args only, no real execution) ────────────────────────

func TestBwrapArgs_ContainsUnshareAll(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bwrap args test only relevant on Linux")
	}
	s := New(cfg(true), "/workspace")
	if !s.IsEnabled() {
		t.Skip("bwrap not available in this environment")
	}
	_, args, err := s.WrapCommand("/workspace", "sh", []string{"-c", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(args, "--unshare-all") {
		t.Errorf("expected --unshare-all in bwrap args, got: %v", args)
	}
}

func TestBwrapArgs_NetworkAccessShareNet(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bwrap args test only relevant on Linux")
	}
	c := cfg(true)
	c.NetworkAccess = true
	s := New(c, "/workspace")
	if !s.IsEnabled() {
		t.Skip("bwrap not available in this environment")
	}
	_, args, err := s.WrapCommand("/workspace", "sh", []string{"-c", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(args, "--share-net") {
		t.Errorf("expected --share-net when NetworkAccess=true, got: %v", args)
	}
}

func TestBwrapArgs_NoNetworkNoShareNet(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bwrap args test only relevant on Linux")
	}
	c := cfg(true)
	c.NetworkAccess = false
	s := New(c, "/workspace")
	if !s.IsEnabled() {
		t.Skip("bwrap not available in this environment")
	}
	_, args, err := s.WrapCommand("/workspace", "sh", []string{"-c", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(args, "--share-net") {
		t.Errorf("expected no --share-net when NetworkAccess=false, got: %v", args)
	}
}

// ── macOS SandboxExecSandbox (profile contents) ───────────────────────────────

// extractProfileFromWrappedArgs walks the wrapped argv produced by WrapCommand
// (env -i KEY=val ... sandbox-exec -p <profile> cmd ...) and returns the SBPL
// profile string. PR #183 changed WrapCommand to prepend /usr/bin/env -i ...
// for environment sanitization, so the old "args[0] == -p" assumption is gone.
func extractProfileFromWrappedArgs(t *testing.T, wrappedBin string, args []string) string {
	t.Helper()
	if wrappedBin != "/usr/bin/env" {
		t.Fatalf("expected wrapper /usr/bin/env, got %q", wrappedBin)
	}
	for i, a := range args {
		if a == "sandbox-exec" {
			if i+2 >= len(args) || args[i+1] != "-p" {
				t.Fatalf("expected 'sandbox-exec -p <profile>' substring, got tail: %v", args[i:])
			}
			return args[i+2]
		}
	}
	t.Fatalf("'sandbox-exec' not found in wrapped args: %v", args)
	return ""
}

func TestSandboxExec_ProfileDenyDefault(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec test only relevant on macOS")
	}
	s := New(cfg(true), "/workspace")
	if !s.IsEnabled() {
		t.Skip("sandbox-exec not available")
	}
	wrappedBin, args, err := s.WrapCommand("/workspace", "sh", []string{"-c", "ls"})
	if err != nil {
		t.Fatal(err)
	}
	profile := extractProfileFromWrappedArgs(t, wrappedBin, args)
	if !strings.Contains(profile, "(deny default)") {
		t.Errorf("profile missing '(deny default)', got:\n%s", profile)
	}
}

func TestSandboxExec_ProfileNetworkAllow(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec test only relevant on macOS")
	}
	c := cfg(true)
	c.NetworkAccess = true
	s := New(c, "/workspace")
	wrappedBin, args, _ := s.WrapCommand("/workspace", "sh", []string{"-c", "ls"})
	profile := extractProfileFromWrappedArgs(t, wrappedBin, args)
	if !strings.Contains(profile, "(allow network*)") {
		t.Errorf("expected '(allow network*)' in profile, got:\n%s", profile)
	}
}

func TestSandboxExec_ProfileNetworkDeny(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec test only relevant on macOS")
	}
	c := cfg(true)
	c.NetworkAccess = false
	s := New(c, "/workspace")
	wrappedBin, args, _ := s.WrapCommand("/workspace", "sh", []string{"-c", "ls"})
	profile := extractProfileFromWrappedArgs(t, wrappedBin, args)
	if !strings.Contains(profile, "(deny network*)") {
		t.Errorf("expected '(deny network*)' in profile, got:\n%s", profile)
	}
}

// ── FileOperation helpers ─────────────────────────────────────────────────────

func TestFileOperation_IsWrite(t *testing.T) {
	cases := []struct {
		op      FileOperation
		isWrite bool
	}{
		{OpRead, false},
		{OpList, false},
		{OpWrite, true},
		{OpDelete, true},
	}
	for _, tc := range cases {
		if got := tc.op.IsWrite(); got != tc.isWrite {
			t.Errorf("%v.IsWrite() = %v, want %v", tc.op, got, tc.isWrite)
		}
	}
}

func TestFileOperation_String(t *testing.T) {
	cases := map[FileOperation]string{
		OpRead:   "read",
		OpWrite:  "write",
		OpDelete: "delete",
		OpList:   "list",
	}
	for op, want := range cases {
		if got := op.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", op, got, want)
		}
	}
}

// ── utility ───────────────────────────────────────────────────────────────────

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
