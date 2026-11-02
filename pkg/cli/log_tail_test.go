package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// safeBuffer is a thread-safe bytes.Buffer for use across goroutines.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// withFastPolling overrides tailPollInterval for the duration of a test.
func withFastPolling(t *testing.T) {
	t.Helper()
	prev := tailPollInterval
	tailPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { tailPollInterval = prev })
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func waitForContains(t *testing.T, buf *safeBuffer, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), needle) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("did not observe %q in output within %s; got %q", needle, timeout, buf.String())
}

func TestTailFollow_InitialAndAppend(t *testing.T) {
	withFastPolling(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("first\nsecond\nthird\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf safeBuffer
	done := make(chan error, 1)
	go func() { done <- tailFollow(ctx, path, 2, &buf) }()

	// Should print last 2 lines initially.
	waitForContains(t, &buf, "second", 500*time.Millisecond)
	waitForContains(t, &buf, "third", 500*time.Millisecond)
	if strings.Contains(buf.String(), "first") {
		t.Errorf("expected only last 2 lines, got %q", buf.String())
	}

	// Append additional lines.
	appendLine(t, path, "fourth\n")
	appendLine(t, path, "fifth\n")
	waitForContains(t, &buf, "fourth", 1*time.Second)
	waitForContains(t, &buf, "fifth", 1*time.Second)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("tailFollow returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("tailFollow did not exit after cancel")
	}
}

func TestTailFollow_LogRotate(t *testing.T) {
	withFastPolling(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("old1\nold2\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf safeBuffer
	done := make(chan error, 1)
	go func() { done <- tailFollow(ctx, path, 1, &buf) }()
	waitForContains(t, &buf, "old2", 500*time.Millisecond)

	// Simulate logrotate: rename the file (creates new inode when re-created).
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	// Recreate file with new content (different inode).
	if err := os.WriteFile(path, []byte("rotated1\n"), 0o644); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	waitForContains(t, &buf, "rotated1", 2*time.Second)

	cancel()
	<-done
}

func TestTailFollow_ContextCancel(t *testing.T) {
	withFastPolling(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var buf safeBuffer
	done := make(chan error, 1)
	go func() { done <- tailFollow(ctx, path, 1, &buf) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on cancel, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("tailFollow did not exit after cancel")
	}
}

func TestPrintTailLines_LongFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("line")
		sb.WriteString(strings.Repeat("x", 0))
		sb.WriteString("\n")
	}
	// Make distinct content for the last 3 lines.
	content := sb.String() + "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out bytes.Buffer
	if err := printTailLines(path, 3, &out); err != nil {
		t.Fatalf("printTailLines: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") || !strings.Contains(got, "gamma") {
		t.Errorf("missing expected tail lines, got %q", got)
	}
	if strings.Count(got, "\n") != 3 {
		t.Errorf("expected 3 lines in output, got %q", got)
	}
}
