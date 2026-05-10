//go:build e2e

package tui

import (
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/e2e/helpers"
)

func TestE2E_TUI_StartupBanner(t *testing.T) {
	bin := helpers.BuildNanoBinary(t)
	s := helpers.NewPTYSession(t, bin, "--tui", "--tea")
	s.ExpectString(t, "terminal AI", 5*time.Second)
	s.SendKeys(t, "ctrl+c")
}

func TestE2E_TUI_PromptReady(t *testing.T) {
	s := startBubbleTeaE2E(t)
	s.ExpectString(t, "Enter 发送", 5*time.Second)
	s.SendKeys(t, "ctrl+c")
}

func TestE2E_TUI_HashFilePickerFlow(t *testing.T) {
	s := startBubbleTeaE2E(t)
	s.ExpectString(t, "Enter 发送", 5*time.Second)
	s.SendKeys(t, "#go.mod")
	s.ExpectString(t, "go.mod", 3*time.Second)
	s.SendKeys(t, "down", "up", "enter")
	s.ExpectString(t, "#go.mod", 3*time.Second)
	s.SendKeys(t, "ctrl+c")
}

func TestE2E_TUI_ClearSessionCommand(t *testing.T) {
	s := startBubbleTeaE2E(t)
	s.ExpectString(t, "Enter 发送", 5*time.Second)
	s.SendKeys(t, "/clear", "enter")
	s.ExpectString(t, "已开启新会话", 3*time.Second)
	s.SendKeys(t, "ctrl+c")
}

func TestE2E_TUI_GracefulShutdown(t *testing.T) {
	s := startBubbleTeaE2E(t)
	s.ExpectString(t, "Enter 发送", 5*time.Second)
	s.SendKeys(t, "ctrl+c")
	done := make(chan error, 1)
	go func() { done <- s.Cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("TUI exited with error: %v\noutput:\n%s", err, s.Output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("TUI did not exit after ctrl+c\noutput:\n%s", s.Output.String())
	}
	s.Cmd = nil
}

func startBubbleTeaE2E(t *testing.T) *helpers.PTYSession {
	t.Helper()
	bin := helpers.BuildNanoBinary(t)
	return helpers.NewPTYSession(t, bin, "--tui", "--tea", "--no-banner")
}
