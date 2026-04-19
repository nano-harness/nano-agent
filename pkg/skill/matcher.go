package skill

import (
	"path/filepath"
	"sort"
	"strings"
)

// MatchContext provides context for skill matching.
type MatchContext struct {
	UserInput      string   // The user's input text
	MentionedFiles []string // File paths mentioned in the input
}

// Match returns skills that match the user input based on triggers and file globs.
// Only skills with auto_invoke enabled are considered unless forceAll is true.
func (m *Manager) Match(ctx *MatchContext, forceAll bool) []MatchResult {
	if ctx == nil || ctx.UserInput == "" {
		return nil
	}

	inputLower := strings.ToLower(ctx.UserInput)
	var results []MatchResult

	for i := range m.skills {
		s := &m.skills[i]

		// Skip already active skills
		if m.activeSkills[s.Name] {
			continue
		}

		// Skip skills with auto_invoke disabled unless forced
		if !forceAll && !s.IsAutoInvoke() {
			continue
		}

		// Check trigger keywords
		if score, reason := matchTriggers(inputLower, s.Triggers); score > 0 {
			results = append(results, MatchResult{
				Skill:  s,
				Score:  score,
				Reason: reason,
			})
			continue
		}

		// Check file glob patterns
		if len(ctx.MentionedFiles) > 0 && len(s.Globs) > 0 {
			if score, reason := matchGlobs(ctx.MentionedFiles, s.Globs); score > 0 {
				results = append(results, MatchResult{
					Skill:  s,
					Score:  score,
					Reason: reason,
				})
				continue
			}
		}

		// Check description keyword matching (lower priority)
		if score, reason := matchDescription(inputLower, s.Description); score > 0 {
			results = append(results, MatchResult{
				Skill:  s,
				Score:  score * 0.5, // Lower weight for description matches
				Reason: reason,
			})
		}
	}

	// Sort by score (highest first), then by priority
	sortResults(results)
	return results
}

// matchTriggers checks if user input contains any trigger keywords.
func matchTriggers(inputLower string, triggers []string) (float64, string) {
	matchCount := 0
	var matched []string

	for _, trigger := range triggers {
		triggerLower := strings.ToLower(trigger)
		if strings.Contains(inputLower, triggerLower) {
			matchCount++
			matched = append(matched, trigger)
		}
	}

	if matchCount == 0 {
		return 0, ""
	}

	// Score based on how many triggers matched
	score := float64(matchCount) / float64(len(triggers))
	return score, "trigger match: " + strings.Join(matched, ", ")
}

// matchGlobs checks if any mentioned files match the skill's glob patterns.
func matchGlobs(files, globs []string) (float64, string) {
	matchCount := 0
	var matched []string

	for _, file := range files {
		for _, glob := range globs {
			if ok, _ := filepath.Match(glob, file); ok {
				matchCount++
				matched = append(matched, file)
				break
			}
			// Also try matching just the filename
			if ok, _ := filepath.Match(glob, filepath.Base(file)); ok {
				matchCount++
				matched = append(matched, file)
				break
			}
		}
	}

	if matchCount == 0 {
		return 0, ""
	}

	score := float64(matchCount) / float64(len(files))
	return score, "file pattern match: " + strings.Join(matched, ", ")
}

// matchDescription checks if user input contains significant words from the description.
func matchDescription(inputLower, description string) (float64, string) {
	if description == "" {
		return 0, ""
	}

	descWords := strings.Fields(strings.ToLower(description))
	// Only consider words with 4+ characters (skip common short words)
	var significantWords []string
	for _, w := range descWords {
		if len(w) >= 4 {
			significantWords = append(significantWords, w)
		}
	}

	if len(significantWords) == 0 {
		return 0, ""
	}

	matchCount := 0
	var matched []string
	for _, word := range significantWords {
		if strings.Contains(inputLower, word) {
			matchCount++
			matched = append(matched, word)
		}
	}

	if matchCount == 0 {
		return 0, ""
	}

	score := float64(matchCount) / float64(len(significantWords))
	return score, "description match: " + strings.Join(matched, ", ")
}

// sortResults sorts match results by score (descending) then by priority (descending).
func sortResults(results []MatchResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Skill.Priority > results[j].Skill.Priority
	})
}
