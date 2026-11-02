//go:build e2e

package helpers

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	expect "github.com/Netflix/go-expect"
	"github.com/creack/pty"
)

// PTYSession wraps a real nano process connected to a pseudoterminal.
type PTYSession struct {
	Console *expect.Console
	Cmd     *exec.Cmd
	Output  *bytes.Buffer
}

// BuildNanoBinary builds cmd/nano into a temporary directory.
func BuildNanoBinary(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "nano")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/nano")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build nano binary: %v\n%s", err, output)
	}
	return out
}

// NewPTYSession starts a command attached to a PTY-backed expect console.
func NewPTYSession(t *testing.T, binary string, args ...string) *PTYSession {
	t.Helper()
	buf := &bytes.Buffer{}
	opts := []expect.ConsoleOpt{
		expect.WithDefaultTimeout(3 * time.Second),
		expect.WithStdout(buf),
	}
	if os.Getenv("EXPECT_DEBUG") == "1" {
		opts = append(opts, expect.WithLogger(log.New(os.Stderr, "expect: ", log.LstdFlags)))
	}
	console, err := expect.NewConsole(opts...)
	if err != nil {
		t.Fatalf("new expect console: %v", err)
	}
	if err := pty.Setsize(console.Tty(), &pty.Winsize{Rows: 30, Cols: 120}); err != nil {
		t.Fatalf("set pty size: %v", err)
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"NO_COLOR=0",
		"NANO_API_KEY=e2e-test-key",
		"NANO_MODEL=e2e-test-model",
		"NANO_ENABLE_MCP=false",
	)
	cmd.Stdin = console.Tty()
	cmd.Stdout = console.Tty()
	cmd.Stderr = console.Tty()
	if err := cmd.Start(); err != nil {
		_ = console.Close()
		t.Fatalf("start PTY command: %v", err)
	}

	s := &PTYSession{Console: console, Cmd: cmd, Output: buf}
	t.Cleanup(s.Close)
	return s
}

// ExpectString waits for text within timeout.
func (s *PTYSession) ExpectString(t *testing.T, text string, timeout time.Duration) {
	t.Helper()
	if _, err := s.Console.Expect(expect.String(text), expect.WithTimeout(timeout)); err != nil {
		t.Fatalf("expect %q: %v\noutput:\n%s", text, err, s.Output.String())
	}
}

// SendKeys sends raw key sequences.
func (s *PTYSession) SendKeys(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		seq := key
		switch key {
		case "enter":
			seq = "\r"
		case "ctrl+c":
			seq = "\x03"
		case "esc":
			seq = "\x1b"
		case "up":
			seq = "\x1b[A"
		case "down":
			seq = "\x1b[B"
		}
		if _, err := s.Console.Send(seq); err != nil {
			t.Fatalf("send key %q: %v", key, err)
		}
	}
}

// Close releases the console and waits for the process to exit.
func (s *PTYSession) Close() {
	if s.Console != nil {
		_ = s.Console.Close()
	}
	if s.Cmd != nil && s.Cmd.Process != nil {
		done := make(chan struct{})
		go func() {
			_ = s.Cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			_ = s.Cmd.Process.Kill()
			<-done
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = parent
	}
}

func (s *PTYSession) String() string {
	return strings.TrimSpace(fmt.Sprintf("%s", s.Output.String()))
}
