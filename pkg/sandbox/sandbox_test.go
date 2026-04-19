package sandbox

import (
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
	pc := NewPathChecker(cfg(false))
	if err := pc.Check("/etc/passwd", OpRead); err != nil {
		t.Fatalf("disabled checker should allow all: %v", err)
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
	if wrappedBin != "sandbox-exec" {
		t.Errorf("expected sandbox-exec, got %q", wrappedBin)
	}
	if len(args) < 2 || args[0] != "-p" {
		t.Fatalf("expected -p <profile> as first args, got: %v", args)
	}
	profile := args[1]
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
	_, args, _ := s.WrapCommand("/workspace", "sh", []string{"-c", "ls"})
	profile := args[1]
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
	_, args, _ := s.WrapCommand("/workspace", "sh", []string{"-c", "ls"})
	profile := args[1]
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
