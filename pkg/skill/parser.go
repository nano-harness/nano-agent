package skill

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
)

// ParseSkillFile parses a SKILL.md file and returns a fully loaded Skill.
func ParseSkillFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill file: %w", err)
	}
	return parseSkillContent(string(data), path)
}

// ParseMetadataOnly parses only the YAML frontmatter of a SKILL.md file.
func ParseMetadataOnly(path string) (*SkillMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open skill file: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	var inFrontmatter bool
	var frontmatter strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if !inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
				continue
			}
			// Skip any content before frontmatter
			continue
		}
		// Inside frontmatter
		if strings.TrimSpace(line) == "---" {
			break // end of frontmatter
		}
		frontmatter.WriteString(line)
		frontmatter.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan skill file: %w", err)
	}

	if frontmatter.Len() == 0 {
		return nil, fmt.Errorf("skill file %q has no YAML frontmatter", path)
	}

	var meta SkillMetadata
	if err := yaml.Unmarshal([]byte(frontmatter.String()), &meta); err != nil {
		return nil, fmt.Errorf("parse skill frontmatter: %w", err)
	}

	if err := validateSkillName(meta.Name); err != nil {
		return nil, err
	}

	meta.SourcePath = path
	return &meta, nil
}

// parseSkillContent parses skill content from a string.
func parseSkillContent(content, sourcePath string) (*Skill, error) {
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("split frontmatter in %q: %w", sourcePath, err)
	}

	var meta SkillMetadata
	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return nil, fmt.Errorf("parse skill frontmatter in %q: %w", sourcePath, err)
	}

	if err := validateSkillName(meta.Name); err != nil {
		return nil, err
	}

	meta.SourcePath = sourcePath

	return &Skill{
		SkillMetadata: meta,
		Instructions:  strings.TrimSpace(body),
	}, nil
}

// splitFrontmatter splits a document into YAML frontmatter and Markdown body.
// Expects the format:
//
//	---
//	yaml content
//	---
//	markdown body
func splitFrontmatter(content string) (frontmatter, body string, err error) {
	lines := strings.SplitAfter(content, "\n")

	var (
		inFrontmatter bool
		fmStart       int
		fmEnd         int
	)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = true
				fmStart = i + 1
				continue
			}
			continue
		}
		if trimmed == "---" {
			fmEnd = i
			break
		}
	}

	if !inFrontmatter || fmEnd == 0 {
		return "", "", fmt.Errorf("no valid YAML frontmatter found (missing --- delimiters)")
	}

	var fmBuilder strings.Builder
	for i := fmStart; i < fmEnd; i++ {
		fmBuilder.WriteString(lines[i])
	}

	var bodyBuilder strings.Builder
	for i := fmEnd + 1; i < len(lines); i++ {
		bodyBuilder.WriteString(lines[i])
	}

	return fmBuilder.String(), bodyBuilder.String(), nil
}

// validateSkillName checks that a skill name is safe and well-formed.
func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name cannot be empty")
	}
	if len(name) > maxSkillNameLength {
		return fmt.Errorf("skill name too long (max %d characters)", maxSkillNameLength)
	}
	if !validSkillNameRegexp.MatchString(name) {
		return fmt.Errorf("invalid skill name %q: must contain only lowercase letters, digits, or hyphens, and must start/end with a letter or digit", name)
	}
	return nil
}
