package slash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestDispatcherModelCommands(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Model = "gpt-4.1"
	cfg.BaseURL = "https://api.openai.com/v1"
	cfg.Fallbacks = []string{"deepseek/deepseek-chat"}
	cfgPath := filepath.Join(t.TempDir(), ".nano.yaml")
	d := NewLocalDispatcher("", t.TempDir()).
		WithModelLister(BuildModelLister(cfg)).
		WithModelStatusGetter(BuildModelStatusGetter(cfg)).
		WithModelSwitcher(BuildModelSwitcher(cfgPath)).
		WithModelFallbackHandler(BuildModelFallbackHandler(cfg)).
		WithModelDoctor(BuildModelDoctor(cfg))

	for _, tc := range []struct {
		input string
		want  string
	}{
		{"/models", "OpenAI"},
		{"/model status", "provider: openai"},
		{"/model fallback list", "deepseek"},
		{"/model doctor", "capabilities:"},
		{"/model use deepseek/deepseek-chat", "重启 TUI 后生效"},
	} {
		r := d.Dispatch(tc.input)
		if !r.Handled || !strings.Contains(r.Message, tc.want) {
			t.Fatalf("%q: expected %q in handled result, got %+v", tc.input, tc.want, r)
		}
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	if !strings.Contains(string(data), "model: deepseek-chat") {
		t.Fatalf("expected model switch to write config, got:\n%s", string(data))
	}
}

func TestDispatcherContextStatusCommand(t *testing.T) {
	d := NewLocalDispatcher("", t.TempDir()).
		WithContextStatusGetter(func() string { return "context ok" })
	r := d.Dispatch("/context status")
	if !r.Handled || !strings.Contains(r.Message, "context ok") {
		t.Fatalf("expected context callback, got %+v", r)
	}
}
