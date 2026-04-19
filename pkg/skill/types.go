package skill

import "regexp"

// Scope defines where a skill is loaded from.
type Scope string

const (
	// ScopePersonal represents a skill from ~/.nano/skills/.
	ScopePersonal Scope = "personal"
	// ScopeProject represents a skill from .nano/skills/ in the project.
	ScopeProject Scope = "project"
)

// validSkillNameRegexp restricts skill names to safe characters,
// preventing path traversal (e.g., ".." or "/" in the name).
var validSkillNameRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// maxSkillNameLength is the maximum allowed length for a skill name.
const maxSkillNameLength = 64

// DefaultMaxSkillSize is the default max SKILL.md file size in bytes (64KB).
const DefaultMaxSkillSize int64 = 64 * 1024

// DefaultMaxSkills is the default maximum number of skills.
const DefaultMaxSkills = 50

// DefaultMaxActiveSkills is the default maximum number of simultaneously active skills.
const DefaultMaxActiveSkills = 5

// SkillMetadata holds the frontmatter metadata of a SKILL.md file.
// This is always loaded and injected into the system prompt summary.
type SkillMetadata struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Triggers    []string `yaml:"triggers,omitempty"`
	Globs       []string `yaml:"globs,omitempty"`
	AutoInvoke  *bool    `yaml:"auto_invoke,omitempty"`
	Priority    int      `yaml:"priority,omitempty"`
	Scope       Scope    `yaml:"-"`
	SourcePath  string   `yaml:"-"`
}

// IsAutoInvoke returns whether the skill should be auto-invoked.
// Default is true if not explicitly set.
func (m *SkillMetadata) IsAutoInvoke() bool {
	if m.AutoInvoke == nil {
		return true
	}
	return *m.AutoInvoke
}

// Skill represents a fully loaded skill with instructions.
type Skill struct {
	SkillMetadata
	Instructions string // Markdown body (full instructions)
}

// MatchResult represents a skill that matched user input.
type MatchResult struct {
	Skill  *Skill
	Score  float64 // Match score (higher = better match)
	Reason string  // Why this skill matched
}
