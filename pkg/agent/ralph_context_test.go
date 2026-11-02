package agent

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestRalphContextMaxIteration(t *testing.T) {
	ctx := NewRalphContext(&config.Config{Hooks: &config.HooksConfig{Ralph: config.RalphLoopConfig{
		Enabled:           true,
		MaxIterations:     2,
		HardMaxIterations: 50,
	}}})
	if !ctx.IsEnabled() {
		t.Fatal("expected ralph enabled")
	}
	if iter, exceeded := ctx.NextIteration(); iter != 1 || exceeded {
		t.Fatalf("iteration 1 = %d/%v", iter, exceeded)
	}
	if iter, exceeded := ctx.NextIteration(); iter != 2 || exceeded {
		t.Fatalf("iteration 2 = %d/%v", iter, exceeded)
	}
	if iter, exceeded := ctx.NextIteration(); iter != 3 || !exceeded {
		t.Fatalf("iteration 3 = %d/%v", iter, exceeded)
	}
}

func TestRalphContextHardMaxClamp(t *testing.T) {
	ctx := NewRalphContext(&config.Config{Hooks: &config.HooksConfig{Ralph: config.RalphLoopConfig{
		Enabled:           true,
		MaxIterations:     100,
		HardMaxIterations: 100,
	}}})
	if ctx.Max() != defaultRalphHardMax {
		t.Fatalf("expected hard max clamp to %d, got %d", defaultRalphHardMax, ctx.Max())
	}
}
