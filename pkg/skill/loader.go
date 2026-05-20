package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// Manager discovers, loads, and manages skills from personal and project directories.
type Manager struct {
	personalDir     string // e.g. ~/.nano/skills
	projectDir      string // e.g. .nano/skills (relative to workingDir)
	workingDir      string
	maxSkillSize    int64
	maxSkills       int
	maxActiveSkills int
	autoInvoke      bool

	skills       []Skill        // all discovered skills
	skillsByName map[string]int // name → index in skills slice

	activeSkills map[string]bool // currently activated skill names

	stateStore        *config.StateStore // optional persistent state store
	builtinSkillNames map[string]bool
}

// NewManager creates a new skill Manager.
func NewManager(workingDir string, personalDir, projectDir string, maxSkillSize int64, maxSkills, maxActiveSkills int, autoInvoke bool) *Manager {
	if maxSkillSize <= 0 {
		maxSkillSize = DefaultMaxSkillSize
	}
	if maxSkills <= 0 {
		maxSkills = DefaultMaxSkills
	}
	if maxActiveSkills <= 0 {
		maxActiveSkills = DefaultMaxActiveSkills
	}

	// Resolve personal dir default
	if personalDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			personalDir = filepath.Join(home, ".nano", "skills")
		}
	}

	// Resolve project dir default
	if projectDir == "" {
		projectDir = ".nano/skills"
	}

	return &Manager{
		personalDir:       personalDir,
		projectDir:        projectDir,
		workingDir:        workingDir,
		maxSkillSize:      maxSkillSize,
		maxSkills:         maxSkills,
		maxActiveSkills:   maxActiveSkills,
		autoInvoke:        autoInvoke,
		skills:            make([]Skill, 0),
		skillsByName:      make(map[string]int),
		activeSkills:      make(map[string]bool),
		builtinSkillNames: make(map[string]bool),
	}
}

// SetStateStore attaches a StateStore for persisting active skill state.
func (m *Manager) SetStateStore(ss *config.StateStore) {
	m.stateStore = ss
}

// EnableBuiltinSkills makes selected embedded skills available during discovery.
func (m *Manager) EnableBuiltinSkills(names []string) {
	for _, name := range names {
		if strings.TrimSpace(name) != "" {
			m.builtinSkillNames[name] = true
		}
	}
}

// getActiveSkillNames returns a sorted slice of active skill names.
func (m *Manager) getActiveSkillNames() []string {
	names := make([]string, 0, len(m.activeSkills))
	for name := range m.activeSkills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Discover scans all skill directories and loads skill metadata and instructions.
// Project-level skills override personal-level skills with the same name.
func (m *Manager) Discover() error {
	m.skills = m.skills[:0]
	m.skillsByName = make(map[string]int)

	m.loadBuiltinSkills()

	// Load personal skills first
	if m.personalDir != "" {
		if err := m.scanDirectory(m.personalDir, ScopePersonal); err != nil {
			logger.Warnf("Failed to scan personal skills dir %q: %v", m.personalDir, err)
		}
	}

	// Load project skills (overrides personal)
	absProjectDir := m.projectDir
	if !filepath.IsAbs(absProjectDir) {
		absProjectDir = filepath.Join(m.workingDir, m.projectDir)
	}
	if err := m.scanDirectory(absProjectDir, ScopeProject); err != nil {
		logger.Warnf("Failed to scan project skills dir %q: %v", absProjectDir, err)
	}

	logger.Infof("Discovered %d skills", len(m.skills))
	return nil
}

func (m *Manager) loadBuiltinSkills() {
	if len(m.builtinSkillNames) == 0 {
		return
	}
	for _, skill := range builtinSkills() {
		if skill == nil {
			continue
		}
		if !m.builtinSkillNames[skill.Name] {
			continue
		}
		m.skillsByName[skill.Name] = len(m.skills)
		m.skills = append(m.skills, *skill)
	}
}

// scanDirectory scans a skills directory for SKILL.md files.
func (m *Manager) scanDirectory(dir string, scope Scope) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Directory doesn't exist, that's fine
		}
		return fmt.Errorf("read skills directory %q: %w", dir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
		info, err := os.Stat(skillPath)
		if err != nil {
			continue // No SKILL.md in this directory
		}

		// Enforce size limit
		if info.Size() > m.maxSkillSize {
			logger.Warnf("Skill file %q exceeds max size (%d > %d bytes), skipping",
				skillPath, info.Size(), m.maxSkillSize)
			continue
		}

		skill, err := ParseSkillFile(skillPath)
		if err != nil {
			logger.Warnf("Failed to parse skill %q: %v", skillPath, err)
			continue
		}

		skill.Scope = scope

		// Project-level overrides personal-level with same name
		if idx, exists := m.skillsByName[skill.Name]; exists {
			if scope == ScopeProject {
				logger.Infof("Project skill %q overrides personal skill", skill.Name)
				m.skills[idx] = *skill
			}
			continue
		}

		// Enforce max skills limit only for new (non-override) skills
		if len(m.skills) >= m.maxSkills {
			logger.Warnf("Maximum number of skills (%d) reached, skipping remaining", m.maxSkills)
			break
		}

		m.skillsByName[skill.Name] = len(m.skills)
		m.skills = append(m.skills, *skill)
	}

	return nil
}

// ListMetadata returns metadata for all discovered skills.
func (m *Manager) ListMetadata() []SkillMetadata {
	result := make([]SkillMetadata, len(m.skills))
	for i, s := range m.skills {
		result[i] = s.SkillMetadata
	}
	return result
}

// GetByName returns a skill by name, or nil if not found.
func (m *Manager) GetByName(name string) *Skill {
	if idx, ok := m.skillsByName[name]; ok {
		return &m.skills[idx]
	}
	return nil
}

// ActivateSkill marks a skill as active. Returns error if max active skills reached.
func (m *Manager) ActivateSkill(name string) error {
	if _, ok := m.skillsByName[name]; !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	if len(m.activeSkills) >= m.maxActiveSkills && !m.activeSkills[name] {
		return fmt.Errorf("maximum active skills (%d) reached", m.maxActiveSkills)
	}
	m.activeSkills[name] = true
	if m.stateStore != nil {
		m.stateStore.SetActiveSkills(m.getActiveSkillNames())
		if err := m.stateStore.Save(); err != nil {
			return fmt.Errorf("save active skills state: %w", err)
		}
	}
	return nil
}

// DeactivateSkill removes a skill from the active set.
func (m *Manager) DeactivateSkill(name string) error {
	delete(m.activeSkills, name)
	if m.stateStore != nil {
		m.stateStore.SetActiveSkills(m.getActiveSkillNames())
		if err := m.stateStore.Save(); err != nil {
			return fmt.Errorf("save active skills state: %w", err)
		}
	}
	return nil
}

// GetActiveSkills returns all currently active skills sorted by priority (high first).
func (m *Manager) GetActiveSkills() []*Skill {
	var result []*Skill
	for name := range m.activeSkills {
		if s := m.GetByName(name); s != nil {
			result = append(result, s)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority > result[j].Priority
	})
	return result
}

// IsActive returns whether a skill is currently active.
func (m *Manager) IsActive(name string) bool {
	return m.activeSkills[name]
}

// ClearActiveSkills resets all active skills.
func (m *Manager) ClearActiveSkills() {
	m.activeSkills = make(map[string]bool)
}

// Count returns the number of discovered skills.
func (m *Manager) Count() int {
	return len(m.skills)
}

// IsAutoInvokeEnabled returns whether global auto-invoke is enabled.
func (m *Manager) IsAutoInvokeEnabled() bool {
	return m.autoInvoke
}

// ListSkillNames returns a formatted list of all skill names and descriptions.
func (m *Manager) ListSkillNames() string {
	if len(m.skills) == 0 {
		return "No skills available."
	}

	var sb strings.Builder
	for _, s := range m.skills {
		status := ""
		if m.activeSkills[s.Name] {
			status = " [ACTIVE]"
		}
		fmt.Fprintf(&sb, "- %s: %s (%s)%s\n", s.Name, s.Description, s.Scope, status)
	}
	return sb.String()
}

// InstallHTTPTimeout is the timeout for HTTP requests when installing skills.
const InstallHTTPTimeout = 30 * time.Second

// maxInstallResponseBytes is the maximum bytes to read when installing a skill (256KB).
const maxInstallResponseBytes = 256 * 1024

// InstallSkill installs a skill from a URL, local path, or archive.
// It supports HTTP/HTTPS URLs (plain SKILL.md or .zip/.tar.gz archive),
// local SKILL.md files, local directories, and local archives.
// After installation, the manager re-discovers all skills.
func (m *Manager) InstallSkill(ctx context.Context, source string) (*Skill, error) {
	if m.personalDir == "" {
		return nil, fmt.Errorf("personal skills directory is not configured")
	}

	if source == "" {
		return nil, fmt.Errorf("source cannot be empty")
	}

	inst := NewInstaller(m.personalDir, m.maxSkillSize)

	// Re-discover to ensure in-memory state reflects the filesystem.
	if err := m.Discover(); err != nil {
		return nil, fmt.Errorf("discover skills before install: %w", err)
	}

	sk, _, err := inst.Install(ctx, source)
	if err != nil {
		return nil, err
	}

	// Check max skills limit (allow overwriting existing skills)
	if len(m.skills) >= m.maxSkills {
		if _, exists := m.skillsByName[sk.Name]; !exists {
			// Clean up the installed files since we can't accept this skill
			if cleanupErr := os.RemoveAll(filepath.Join(m.personalDir, sk.Name)); cleanupErr != nil {
				logger.Warnf("Failed to clean up skill directory for %q: %v", sk.Name, cleanupErr)
			}
			return nil, fmt.Errorf("maximum number of skills (%d) reached", m.maxSkills)
		}
	}

	// Re-discover to load the newly installed skill
	if err := m.Discover(); err != nil {
		return nil, fmt.Errorf("re-discover after install: %w", err)
	}

	installed := m.GetByName(sk.Name)
	if installed == nil {
		return nil, fmt.Errorf("skill %q was installed but not found after re-discovery", sk.Name)
	}

	return installed, nil
}

// RemoveSkill removes a personal skill installation and refreshes discovery.
// Project skills are source-controlled declarations and are not removed by the
// runtime extension manager.
func (m *Manager) RemoveSkill(name string) error {
	if name == "" {
		return fmt.Errorf("skill name cannot be empty")
	}
	if m.personalDir == "" {
		return fmt.Errorf("personal skills directory is not configured")
	}
	if err := m.Discover(); err != nil {
		return fmt.Errorf("discover skills before remove: %w", err)
	}
	sk := m.GetByName(name)
	if sk == nil {
		return fmt.Errorf("skill %q not found", name)
	}
	if sk.Scope != ScopePersonal {
		return fmt.Errorf("skill %q is %s-scoped and cannot be removed by the runtime extension manager", name, sk.Scope)
	}
	if sk.SourcePath == "" {
		return fmt.Errorf("skill %q has no source path", name)
	}
	skillDir := filepath.Dir(sk.SourcePath)
	personalAbs, err := filepath.Abs(m.personalDir)
	if err != nil {
		return fmt.Errorf("resolve personal skills directory: %w", err)
	}
	skillAbs, err := filepath.Abs(skillDir)
	if err != nil {
		return fmt.Errorf("resolve skill directory: %w", err)
	}
	rel, err := filepath.Rel(personalAbs, skillAbs)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return fmt.Errorf("refusing to remove skill outside personal skills directory: %s", skillDir)
	}
	if err := os.RemoveAll(skillAbs); err != nil {
		return fmt.Errorf("remove skill %q: %w", name, err)
	}
	delete(m.activeSkills, name)
	if m.stateStore != nil {
		m.stateStore.SetActiveSkills(m.getActiveSkillNames())
		if err := m.stateStore.Save(); err != nil {
			return fmt.Errorf("save active skills state: %w", err)
		}
	}
	if err := m.Discover(); err != nil {
		return fmt.Errorf("re-discover after remove: %w", err)
	}
	return nil
}
