package spinnerverbs

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestEffectiveVerbs_NilConfig(t *testing.T) {
	// nil config should return default verbs
	verbs := EffectiveVerbs(nil)
	if len(verbs) != len(DefaultVerbs) {
		t.Errorf("Expected %d verbs, got %d", len(DefaultVerbs), len(verbs))
	}
	// Verify it's actually the default verbs
	for i, v := range verbs {
		if v != DefaultVerbs[i] {
			t.Errorf("Expected verb %q at index %d, got %q", DefaultVerbs[i], i, v)
		}
	}
}

func TestEffectiveVerbs_Disabled(t *testing.T) {
	// enabled: false should return empty list
	disabled := false
	cfg := &config.SpinnerVerbsConfig{
		Enabled: &disabled,
	}
	verbs := EffectiveVerbs(cfg)
	if len(verbs) != 0 {
		t.Errorf("Expected 0 verbs when disabled, got %d", len(verbs))
	}
}

func TestEffectiveVerbs_Enabled(t *testing.T) {
	// enabled: true with no custom verbs should return default verbs
	enabled := true
	cfg := &config.SpinnerVerbsConfig{
		Enabled: &enabled,
	}
	verbs := EffectiveVerbs(cfg)
	if len(verbs) != len(DefaultVerbs) {
		t.Errorf("Expected %d verbs, got %d", len(DefaultVerbs), len(verbs))
	}
}

func TestEffectiveVerbs_AppendMode(t *testing.T) {
	// mode: append should combine default + custom verbs
	cfg := &config.SpinnerVerbsConfig{
		Mode:  "append",
		Verbs: []string{"Custom1", "Custom2"},
	}
	verbs := EffectiveVerbs(cfg)
	expectedLen := len(DefaultVerbs) + 2
	if len(verbs) != expectedLen {
		t.Errorf("Expected %d verbs in append mode, got %d", expectedLen, len(verbs))
	}
	// Check that default verbs come first
	for i, v := range DefaultVerbs {
		if verbs[i] != v {
			t.Errorf("Expected default verb %q at index %d, got %q", v, i, verbs[i])
		}
	}
	// Check custom verbs are appended
	if verbs[len(DefaultVerbs)] != "Custom1" || verbs[len(DefaultVerbs)+1] != "Custom2" {
		t.Errorf("Custom verbs not appended correctly")
	}
}

func TestEffectiveVerbs_EmptyModeDefaultsToAppend(t *testing.T) {
	// empty mode string should default to append behavior
	cfg := &config.SpinnerVerbsConfig{
		Mode:  "",
		Verbs: []string{"Extra"},
	}
	verbs := EffectiveVerbs(cfg)
	expectedLen := len(DefaultVerbs) + 1
	if len(verbs) != expectedLen {
		t.Errorf("Expected %d verbs with empty mode (append default), got %d", expectedLen, len(verbs))
	}
}

func TestEffectiveVerbs_ReplaceMode(t *testing.T) {
	// mode: replace with custom verbs should return only custom verbs
	cfg := &config.SpinnerVerbsConfig{
		Mode:  "replace",
		Verbs: []string{"Only1", "Only2", "Only3"},
	}
	verbs := EffectiveVerbs(cfg)
	if len(verbs) != 3 {
		t.Errorf("Expected 3 verbs in replace mode, got %d", len(verbs))
	}
	expected := []string{"Only1", "Only2", "Only3"}
	for i, v := range verbs {
		if v != expected[i] {
			t.Errorf("Expected verb %q at index %d, got %q", expected[i], i, v)
		}
	}
}

func TestEffectiveVerbs_ReplaceMode_EmptyFallback(t *testing.T) {
	// mode: replace with no custom verbs should fall back to defaults
	cfg := &config.SpinnerVerbsConfig{
		Mode:  "replace",
		Verbs: []string{},
	}
	verbs := EffectiveVerbs(cfg)
	if len(verbs) != len(DefaultVerbs) {
		t.Errorf("Expected %d verbs (fallback to defaults), got %d", len(DefaultVerbs), len(verbs))
	}
	// Verify it's the default verbs
	for i, v := range verbs {
		if v != DefaultVerbs[i] {
			t.Errorf("Expected default verb %q at index %d, got %q", DefaultVerbs[i], i, v)
		}
	}
}

func TestVerbForFrame_EmptyList(t *testing.T) {
	// empty verb list should return empty string
	verb := VerbForFrame(0, []string{})
	if verb != "" {
		t.Errorf("Expected empty string for empty verb list, got %q", verb)
	}
}

func TestVerbForFrame_SingleVerb(t *testing.T) {
	// single verb should always return that verb
	verbs := []string{"OnlyOne"}
	for i := 0; i < 10; i++ {
		verb := VerbForFrame(i, verbs)
		if verb != "OnlyOne" {
			t.Errorf("Expected 'OnlyOne' for frame %d, got %q", i, verb)
		}
	}
}

func TestVerbForFrame_MultipleVerbs(t *testing.T) {
	// verbs rotate every VerbRotationFrames frames
	verbs := []string{"First", "Second", "Third"}
	tests := []struct {
		frame    int
		expected string
	}{
		{0, "First"},
		{29, "First"},
		{30, "Second"},
		{59, "Second"},
		{60, "Third"},
		{89, "Third"},
		{90, "First"}, // wraps
	}
	for _, tt := range tests {
		verb := VerbForFrame(tt.frame, verbs)
		if verb != tt.expected {
			t.Errorf("VerbForFrame(%d) = %q, expected %q", tt.frame, verb, tt.expected)
		}
	}
}

func TestVerbForFrame_LargeFrame(t *testing.T) {
	// test with large frame numbers to ensure rotation / modulo works correctly
	verbs := []string{"A", "B", "C"}
	verb := VerbForFrame(1000, verbs)
	expected := verbs[(1000/VerbRotationFrames)%len(verbs)]
	if verb != expected {
		t.Errorf("VerbForFrame(1000) = %q, expected %q", verb, expected)
	}
}

func TestVerbForFrame_RotatesEveryThirtyFrames(t *testing.T) {
	verbs := []string{"A", "B", "C"}
	tests := []struct {
		frame    int
		expected string
	}{
		{0, "A"},
		{29, "A"},
		{30, "B"},
		{59, "B"},
		{60, "C"},
		{90, "A"},
	}
	for _, tt := range tests {
		if got := VerbForFrame(tt.frame, verbs); got != tt.expected {
			t.Errorf("VerbForFrame(%d) = %q, expected %q", tt.frame, got, tt.expected)
		}
	}
}

func TestVerbForFrame_EmptyVerbs(t *testing.T) {
	if got := VerbForFrame(0, nil); got != "" {
		t.Errorf("VerbForFrame(0, nil) = %q, expected empty", got)
	}
	if got := VerbForFrame(5, []string{}); got != "" {
		t.Errorf("VerbForFrame(5, []) = %q, expected empty", got)
	}
}

func TestVerbForFrame_NegativeFrameUsesFirstVerb(t *testing.T) {
	verbs := []string{"X", "Y"}
	if got := VerbForFrame(-1, verbs); got != "X" {
		t.Errorf("VerbForFrame(-1) = %q, expected %q", got, "X")
	}
	if got := VerbForFrame(-100, verbs); got != "X" {
		t.Errorf("VerbForFrame(-100) = %q, expected %q", got, "X")
	}
}

func TestEffectiveVerbs_DisabledWithCustomVerbs(t *testing.T) {
	// even with custom verbs, disabled should return empty
	disabled := false
	cfg := &config.SpinnerVerbsConfig{
		Enabled: &disabled,
		Mode:    "append",
		Verbs:   []string{"Custom1", "Custom2"},
	}
	verbs := EffectiveVerbs(cfg)
	if len(verbs) != 0 {
		t.Errorf("Expected 0 verbs when disabled (even with custom verbs), got %d", len(verbs))
	}
}

func TestRandomVerb_EmptyList(t *testing.T) {
	if got := RandomVerb([]string{}); got != "" {
		t.Errorf("RandomVerb([]) = %q, expected empty", got)
	}
	if got := RandomVerb(nil); got != "" {
		t.Errorf("RandomVerb(nil) = %q, expected empty", got)
	}
}

func TestRandomVerb_SingleVerb(t *testing.T) {
	verbs := []string{"OnlyOne"}
	for i := 0; i < 10; i++ {
		if got := RandomVerb(verbs); got != "OnlyOne" {
			t.Errorf("RandomVerb(single) = %q, expected %q", got, "OnlyOne")
		}
	}
}

func TestRandomVerb_MultipleVerbs(t *testing.T) {
	verbs := []string{"A", "B", "C", "D", "E"}
	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		v := RandomVerb(verbs)
		if v == "" {
			t.Fatalf("RandomVerb returned empty string for non-empty list")
		}
		seen[v] = true
	}
	// With 200 samples from 5 verbs the probability of missing any verb is
	// astronomically small; this guards against always returning the same verb.
	if len(seen) < 2 {
		t.Errorf("RandomVerb appears non-random: only saw %d distinct verbs in 200 calls", len(seen))
	}
	// All returned verbs must be from the input list.
	verbSet := map[string]bool{"A": true, "B": true, "C": true, "D": true, "E": true}
	for v := range seen {
		if !verbSet[v] {
			t.Errorf("RandomVerb returned unexpected verb %q", v)
		}
	}
}
