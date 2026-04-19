// Package openspec provides OpenSpec (spec-driven development) integration
// for the nano-agent. It supports the /opsx: slash command workflow for
// structured proposal → specs → design → tasks → implementation.
package openspec

import (
	"time"
)

// ArtifactStatus represents the current state of an artifact.
type ArtifactStatus string

const (
	ArtifactStatusPending  ArtifactStatus = "pending"  // Dependencies not met
	ArtifactStatusReady    ArtifactStatus = "ready"    // Dependencies met, can be created
	ArtifactStatusCreated  ArtifactStatus = "created"  // File exists on disk
	ArtifactStatusOutdated ArtifactStatus = "outdated" // Dependencies changed after creation
)

// CommandType represents an /opsx: slash command.
type CommandType string

const (
	CommandPropose     CommandType = "propose"
	CommandExplore     CommandType = "explore"
	CommandNew         CommandType = "new"
	CommandContinue    CommandType = "continue"
	CommandFastForward CommandType = "ff"
	CommandApply       CommandType = "apply"
	CommandVerify      CommandType = "verify"
	CommandSync        CommandType = "sync"
	CommandArchive     CommandType = "archive"
	CommandBulkArchive CommandType = "bulk-archive"
	CommandStatus      CommandType = "status"
)

// TaskStatus represents whether a task is complete.
type TaskStatus string

const (
	TaskStatusPending  TaskStatus = "pending"
	TaskStatusComplete TaskStatus = "complete"
)

// Change represents an OpenSpec change — a proposed modification packaged
// in a folder with everything needed to understand and implement it.
type Change struct {
	Name      string               `json:"name" yaml:"name"`
	Schema    string               `json:"schema" yaml:"schema"`
	Path      string               `json:"path" yaml:"path"`           // Absolute path to change directory
	Artifacts map[string]*Artifact `json:"artifacts" yaml:"artifacts"` // Keyed by artifact ID
	Metadata  ChangeMetadata       `json:"metadata" yaml:"metadata"`
	CreatedAt time.Time            `json:"created_at" yaml:"created_at"`
}

// ChangeMetadata holds optional metadata from .openspec.yaml inside a change folder.
type ChangeMetadata struct {
	Schema    string `json:"schema" yaml:"schema"`
	CreatedAt string `json:"created_at" yaml:"created_at"`
}

// Artifact represents a single artifact (proposal, specs, design, tasks)
// within a change.
type Artifact struct {
	ID           string         `json:"id" yaml:"id"`
	FilePath     string         `json:"file_path" yaml:"file_path"`       // Relative to change dir
	FilePattern  string         `json:"file_pattern" yaml:"file_pattern"` // Glob pattern for multi-file artifacts (e.g., "specs/**/*.md")
	Status       ArtifactStatus `json:"status" yaml:"status"`
	Dependencies []string       `json:"dependencies" yaml:"dependencies"` // IDs of required artifacts
	Content      string         `json:"-" yaml:"-"`                       // Lazy-loaded file content
}

// SchemaDefinition defines the artifact types and their dependency graph
// for a particular workflow schema (e.g. "spec-driven").
type SchemaDefinition struct {
	Name      string           `json:"name" yaml:"name"`
	Artifacts []SchemaArtifact `json:"artifacts" yaml:"artifacts"`
}

// SchemaArtifact describes one artifact type within a schema.
type SchemaArtifact struct {
	ID        string   `json:"id" yaml:"id"`               // e.g. "proposal", "specs", "design", "tasks"
	Generates string   `json:"generates" yaml:"generates"` // File pattern: "proposal.md", "specs/**/*.md"
	Requires  []string `json:"requires" yaml:"requires"`   // Dependency artifact IDs
}

// Task represents a single task parsed from tasks.md.
type Task struct {
	ID          string     `json:"id"`          // e.g. "1.1"
	Description string     `json:"description"` // Task text
	Status      TaskStatus `json:"status"`
	GroupName   string     `json:"group_name"` // Heading group (e.g. "Theme Infrastructure")
}

// Command represents a parsed /opsx: slash command.
type Command struct {
	Type       CommandType `json:"type"`
	ChangeName string      `json:"change_name"` // Optional change name argument
	Args       []string    `json:"args"`        // Additional arguments
	RawInput   string      `json:"raw_input"`   // Original user input
}

// ChangeStatus provides a summary view of a change's current state.
type ChangeStatus struct {
	Name             string                    `json:"name"`
	ArtifactStatuses map[string]ArtifactStatus `json:"artifact_statuses"` // artifact ID → status
	TasksTotal       int                       `json:"tasks_total"`
	TasksCompleted   int                       `json:"tasks_completed"`
	ReadyArtifacts   []string                  `json:"ready_artifacts"`
}

// ProjectConfig represents openspec/config.yaml project-level configuration.
type ProjectConfig struct {
	Schema  string              `json:"schema" yaml:"schema"`
	Context string              `json:"context" yaml:"context"` // Project context injected into artifact instructions
	Rules   map[string][]string `json:"rules" yaml:"rules"`     // Per-artifact rules, keyed by artifact ID
}
