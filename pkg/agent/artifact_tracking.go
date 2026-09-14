package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

// Artifact tracking addresses a known industry blind spot: in third-party
// cross-evaluations, every compaction method scored only 2.19–2.45/5 on
// "remembering which files were modified" after compression. Rather than
// trusting the LLM summary to recall file mutations, nano-agent extracts a
// deterministic artifact manifest directly from the tool call records and
// keeps it in the context, outside the LLM-summarized region. See
// docs/features/CONTEXT_ENGINEERING.md for the design rationale.

// ArtifactAction describes how a file artifact was changed in this session.
type ArtifactAction string

const (
	ArtifactWritten  ArtifactAction = "written"  // write_file (create or full overwrite)
	ArtifactModified ArtifactAction = "modified" // edit_file (str_replace / insert)
	ArtifactDeleted  ArtifactAction = "deleted"  // delete_file
)

// ArtifactRecord captures one file mutation observed in tool call records.
type ArtifactRecord struct {
	Path   string
	Action ArtifactAction
	Tool   string
}

const (
	artifactManifestOpen  = "<artifact_manifest>"
	artifactManifestClose = "</artifact_manifest>"
	artifactFieldSep      = " | "
)

// artifactToolActions maps filesystem-mutating tool names to the artifact
// action they perform. Read-only and shell tools are intentionally excluded:
// shell commands are free-form and cannot be attributed deterministically.
var artifactToolActions = map[string]ArtifactAction{
	"write_file":  ArtifactWritten,
	"edit_file":   ArtifactModified,
	"delete_file": ArtifactDeleted,
}

// ExtractArtifactRecords scans conversation messages and returns the
// deterministic list of file artifacts written, modified, or deleted so far.
// Data source: assistant tool call records only — never LLM memory.
//
// Records are merged by path with last-write-wins semantics (a later action
// on the same path supersedes an earlier one), then sorted by path so the
// output is byte-for-byte deterministic. Previously emitted manifests found
// in the messages (e.g. from an earlier compaction) are parsed and merged in,
// so artifact knowledge survives repeated compactions.
func ExtractArtifactRecords(messages []llm.Message) []ArtifactRecord {
	merged := make(map[string]ArtifactRecord)

	merge := func(rec ArtifactRecord) {
		if rec.Path == "" {
			return
		}
		merged[rec.Path] = rec
	}

	for _, msg := range messages {
		// Merge manifests emitted by earlier compactions so they survive.
		if msg.Role == "user" && strings.Contains(msg.Content, artifactManifestOpen) {
			for _, rec := range ParseArtifactManifest(msg.Content) {
				merge(rec)
			}
			continue
		}
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			action, ok := artifactToolActions[tc.Name]
			if !ok {
				continue
			}
			merge(ArtifactRecord{
				Path:   artifactPathFromArguments(tc.Arguments),
				Action: action,
				Tool:   tc.Name,
			})
		}
	}

	records := make([]ArtifactRecord, 0, len(merged))
	for _, rec := range merged {
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	return records
}

// artifactPathFromArguments extracts the target file path from tool call
// arguments. write_file uses "file_path"; edit_file and delete_file use
// "path". Both keys are accepted for robustness.
func artifactPathFromArguments(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	for _, key := range []string{"file_path", "path"} {
		if v, ok := args[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// FormatArtifactManifest renders records as a deterministic manifest block.
// Returns an empty string when there are no records, so callers can skip
// appending anything. The manifest is plain data: it is generated without any
// LLM call and must be preserved verbatim in the compressed context.
func FormatArtifactManifest(records []ArtifactRecord) string {
	if len(records) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(artifactManifestOpen)
	sb.WriteString("\n")
	sb.WriteString("# Files changed this session (deterministically extracted from tool call records; not part of the LLM summary).\n")
	for _, rec := range records {
		fmt.Fprintf(&sb, "%s%s%s%s%s\n", rec.Action, artifactFieldSep, rec.Path, artifactFieldSep, rec.Tool)
	}
	sb.WriteString(artifactManifestClose)
	return sb.String()
}

// ParseArtifactManifest extracts artifact records from a message content that
// contains a manifest block. Lines that do not match the
// "<action> | <path> | <tool>" shape are ignored.
func ParseArtifactManifest(content string) []ArtifactRecord {
	start := strings.Index(content, artifactManifestOpen)
	if start < 0 {
		return nil
	}
	rest := content[start+len(artifactManifestOpen):]
	end := strings.Index(rest, artifactManifestClose)
	if end < 0 {
		return nil
	}
	body := rest[:end]

	var records []ArtifactRecord
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, artifactFieldSep)
		if len(parts) != 3 {
			continue
		}
		action := ArtifactAction(parts[0])
		switch action {
		case ArtifactWritten, ArtifactModified, ArtifactDeleted:
		default:
			continue
		}
		records = append(records, ArtifactRecord{
			Path:   parts[1],
			Action: action,
			Tool:   parts[2],
		})
	}
	return records
}
