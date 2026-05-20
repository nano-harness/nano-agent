package spinnerverbs

import (
	"math/rand"

	"github.com/nano-harness/nano-agent/pkg/config"
)

// DefaultVerbs is the built-in list of spinner verbs displayed during agent operations.
var DefaultVerbs = []string{
	"Thinking",
	"Processing",
	"Analyzing",
	"Generating",
	"Computing",
	"Executing",
	"Working",
	"Brewing",
	"Crafting",
	"Pondering",
	"Weaving",
	"Building",
}

// EffectiveVerbs returns the final list of verbs based on the configuration.
// Behavior:
// - cfg == nil: returns DefaultVerbs (enabled by default)
// - enabled == false: returns empty list
// - mode == "replace" && len(verbs)>0: returns only custom verbs
// - mode == "replace" && len(verbs)==0: falls back to DefaultVerbs
// - otherwise (mode == "append" or empty): returns DefaultVerbs + custom verbs
func EffectiveVerbs(cfg *config.SpinnerVerbsConfig) []string {
	// nil config means use defaults with spinner enabled
	if cfg == nil {
		return DefaultVerbs
	}

	// Check if explicitly disabled
	if cfg.Enabled != nil && !*cfg.Enabled {
		return []string{}
	}

	// Handle replace mode
	if cfg.Mode == "replace" {
		if len(cfg.Verbs) > 0 {
			return cfg.Verbs
		}
		// Empty replace falls back to defaults
		return DefaultVerbs
	}

	// Default: append mode (or empty mode string defaults to append)
	result := make([]string, len(DefaultVerbs))
	copy(result, DefaultVerbs)
	result = append(result, cfg.Verbs...)
	return result
}

// RandomVerb returns a randomly selected verb from the given list.
// Returns empty string when the verb list is empty.
func RandomVerb(verbs []string) string {
	if len(verbs) == 0 {
		return ""
	}
	return verbs[rand.Intn(len(verbs))]
}

// VerbRotationFrames controls how many spinner frames pass before
// VerbForFrame rotates to the next verb.
// Deprecated: Use RandomVerb instead. Verb is now selected once per
// thinking cycle rather than rotating on every N frames.
const VerbRotationFrames = 30

// VerbForFrame returns the verb string for the given animation frame.
// Verbs rotate every VerbRotationFrames frames. Returns the empty
// string when the verb list is empty. Negative frames are clamped to
// zero so callers never index out of range.
// Deprecated: Use RandomVerb instead.
func VerbForFrame(frame int, verbs []string) string {
	if len(verbs) == 0 {
		return ""
	}
	if frame < 0 {
		frame = 0
	}
	return verbs[(frame/VerbRotationFrames)%len(verbs)]
}
