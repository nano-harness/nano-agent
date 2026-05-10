package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

func TestModelUseWritesProjectConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	config.SetGlobalConfig(nil)

	root := newTestModelRoot()
	root.SetArgs([]string{"model", "use", "deepseek-r1", "--provider", "deepseek"})
	if err := root.Execute(); err != nil {
		t.Fatalf("model use failed: %v", err)
	}

	cfg := readNanoYAML(t)
	if got := cfg["model"]; got != "deepseek-r1" {
		t.Fatalf("model = %v, want deepseek-r1", got)
	}
	if got := cfg["base_url"]; got != "https://api.deepseek.com/v1" {
		t.Fatalf("base_url = %v, want deepseek preset URL", got)
	}
}

func TestModelFallbackAddReplacesByName(t *testing.T) {
	t.Chdir(t.TempDir())
	config.SetGlobalConfig(nil)

	root := newTestModelRoot()
	root.SetArgs([]string{"model", "fallback", "add", "gpt-4.1", "--name", "fast", "--provider", "openai"})
	if err := root.Execute(); err != nil {
		t.Fatalf("fallback add failed: %v", err)
	}
	root = newTestModelRoot()
	root.SetArgs([]string{"model", "fallback", "add", "gpt-5", "--name", "fast", "--provider", "openai"})
	if err := root.Execute(); err != nil {
		t.Fatalf("fallback replace failed: %v", err)
	}

	cfg := readNanoYAML(t)
	routing, ok := cfg["model_routing"].(map[interface{}]interface{})
	if !ok {
		t.Fatalf("model_routing missing: %#v", cfg["model_routing"])
	}
	rawRoutes, ok := routing["fallbacks"].([]interface{})
	if !ok || len(rawRoutes) != 1 {
		t.Fatalf("fallbacks = %#v, want one route", routing["fallbacks"])
	}
	route, ok := rawRoutes[0].(map[interface{}]interface{})
	if !ok {
		t.Fatalf("route has unexpected type: %#v", rawRoutes[0])
	}
	if route["name"] != "fast" {
		t.Fatalf("route name = %v, want fast", route["name"])
	}
	if route["model"] != "gpt-5" {
		t.Fatalf("route model = %v, want gpt-5", route["model"])
	}
}

func TestThinkOnWritesReasoningConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	config.SetGlobalConfig(nil)

	root := newTestModelRoot()
	root.SetArgs([]string{"think", "on", "--effort", "high", "--exclude"})
	if err := root.Execute(); err != nil {
		t.Fatalf("think on failed: %v", err)
	}

	cfg := readNanoYAML(t)
	reasoning, ok := cfg["reasoning"].(map[interface{}]interface{})
	if !ok {
		t.Fatalf("reasoning missing: %#v", cfg["reasoning"])
	}
	if reasoning["enabled"] != true {
		t.Fatalf("reasoning.enabled = %v, want true", reasoning["enabled"])
	}
	if reasoning["effort"] != "high" {
		t.Fatalf("reasoning.effort = %v, want high", reasoning["effort"])
	}
	if reasoning["exclude"] != true {
		t.Fatalf("reasoning.exclude = %v, want true", reasoning["exclude"])
	}
}

func TestModelCommandIncludesExpectedSubcommands(t *testing.T) {
	cmd := NewModelCommand()
	var names []string
	for _, child := range cmd.Commands() {
		names = append(names, child.Name())
	}
	got := strings.Join(names, ",")
	for _, want := range []string{"list", "use", "status", "fallback", "doctor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("model subcommands %q missing %q", got, want)
		}
	}
}

func newTestModelRoot() *cobra.Command {
	root := &cobra.Command{Use: "nano"}
	root.AddCommand(NewModelCommand())
	root.AddCommand(NewModelsCommand())
	root.AddCommand(NewThinkCommand())
	return root
}

func readNanoYAML(t *testing.T) map[interface{}]interface{} {
	t.Helper()
	data, err := os.ReadFile(".nano.yaml")
	if err != nil {
		t.Fatalf("read .nano.yaml: %v", err)
	}
	var cfg map[interface{}]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse .nano.yaml: %v", err)
	}
	return cfg
}
