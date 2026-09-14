package agent

import (
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/tools"
)

func toolCallMsg(calls ...tools.ToolCall) llm.Message {
	return llm.Message{Role: "assistant", Content: "working", ToolCalls: calls}
}

func TestExtractArtifactRecords(t *testing.T) {
	tests := []struct {
		name     string
		messages []llm.Message
		want     []ArtifactRecord
	}{
		{
			name:     "empty history",
			messages: nil,
			want:     nil,
		},
		{
			name: "no mutating tool calls",
			messages: []llm.Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "hello"},
				toolCallMsg(tools.ToolCall{ID: "c1", Name: "read_file", Arguments: map[string]interface{}{"path": "/a.go"}}),
			},
			want: nil,
		},
		{
			name: "write edit delete across turns, sorted by path",
			messages: []llm.Message{
				{Role: "user", Content: "do work"},
				toolCallMsg(tools.ToolCall{ID: "c1", Name: "write_file", Arguments: map[string]interface{}{"file_path": "/b.go"}}),
				{Role: "tool", Content: "ok", ToolCallID: "c1"},
				toolCallMsg(tools.ToolCall{ID: "c2", Name: "edit_file", Arguments: map[string]interface{}{"path": "/a.go"}}),
				{Role: "tool", Content: "ok", ToolCallID: "c2"},
				toolCallMsg(tools.ToolCall{ID: "c3", Name: "delete_file", Arguments: map[string]interface{}{"path": "/c.go"}}),
				{Role: "tool", Content: "ok", ToolCallID: "c3"},
			},
			want: []ArtifactRecord{
				{Path: "/a.go", Action: ArtifactModified, Tool: "edit_file"},
				{Path: "/b.go", Action: ArtifactWritten, Tool: "write_file"},
				{Path: "/c.go", Action: ArtifactDeleted, Tool: "delete_file"},
			},
		},
		{
			name: "last action on same path wins",
			messages: []llm.Message{
				{Role: "user", Content: "iterate"},
				toolCallMsg(tools.ToolCall{ID: "c1", Name: "write_file", Arguments: map[string]interface{}{"file_path": "/a.go"}}),
				toolCallMsg(tools.ToolCall{ID: "c2", Name: "edit_file", Arguments: map[string]interface{}{"path": "/a.go"}}),
				toolCallMsg(tools.ToolCall{ID: "c3", Name: "delete_file", Arguments: map[string]interface{}{"path": "/a.go"}}),
			},
			want: []ArtifactRecord{
				{Path: "/a.go", Action: ArtifactDeleted, Tool: "delete_file"},
			},
		},
		{
			name: "missing path argument is ignored",
			messages: []llm.Message{
				{Role: "user", Content: "go"},
				toolCallMsg(tools.ToolCall{ID: "c1", Name: "write_file", Arguments: map[string]interface{}{"content": "x"}}),
				toolCallMsg(tools.ToolCall{ID: "c2", Name: "edit_file", Arguments: nil}),
			},
			want: nil,
		},
		{
			name: "write_file also accepts path key",
			messages: []llm.Message{
				{Role: "user", Content: "go"},
				toolCallMsg(tools.ToolCall{ID: "c1", Name: "write_file", Arguments: map[string]interface{}{"path": "/p.go"}}),
			},
			want: []ArtifactRecord{
				{Path: "/p.go", Action: ArtifactWritten, Tool: "write_file"},
			},
		},
		{
			name: "existing manifest from earlier compaction is merged",
			messages: []llm.Message{
				{Role: "system", Content: "sys"},
				{Role: "user", Content: "<!-- COMPRESSED CONTEXT -->\nsummary\n<!-- END COMPRESSED CONTEXT -->\n\n" +
					"<artifact_manifest>\nwritten | /old.go | write_file\n</artifact_manifest>"},
				{Role: "user", Content: "continue"},
				toolCallMsg(tools.ToolCall{ID: "c1", Name: "edit_file", Arguments: map[string]interface{}{"path": "/new.go"}}),
			},
			want: []ArtifactRecord{
				{Path: "/new.go", Action: ArtifactModified, Tool: "edit_file"},
				{Path: "/old.go", Action: ArtifactWritten, Tool: "write_file"},
			},
		},
		{
			name: "later tool call overrides manifest record for same path",
			messages: []llm.Message{
				{Role: "user", Content: "<artifact_manifest>\nwritten | /a.go | write_file\n</artifact_manifest>"},
				{Role: "user", Content: "continue"},
				toolCallMsg(tools.ToolCall{ID: "c1", Name: "delete_file", Arguments: map[string]interface{}{"path": "/a.go"}}),
			},
			want: []ArtifactRecord{
				{Path: "/a.go", Action: ArtifactDeleted, Tool: "delete_file"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractArtifactRecords(tt.messages)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d records (%v), want %d (%v)", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("record %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFormatArtifactManifest(t *testing.T) {
	tests := []struct {
		name         string
		records      []ArtifactRecord
		wantEmpty    bool
		wantContains []string
	}{
		{
			name:      "no records produces empty string",
			records:   nil,
			wantEmpty: true,
		},
		{
			name: "records render deterministic block",
			records: []ArtifactRecord{
				{Path: "/a.go", Action: ArtifactModified, Tool: "edit_file"},
				{Path: "/b.go", Action: ArtifactWritten, Tool: "write_file"},
			},
			wantContains: []string{
				"<artifact_manifest>",
				"</artifact_manifest>",
				"modified | /a.go | edit_file",
				"written | /b.go | write_file",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatArtifactManifest(tt.records)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty manifest, got %q", got)
				}
				return
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("manifest missing %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestArtifactManifest_RoundTrip(t *testing.T) {
	records := []ArtifactRecord{
		{Path: "/src/a.go", Action: ArtifactWritten, Tool: "write_file"},
		{Path: "/src/b.go", Action: ArtifactModified, Tool: "edit_file"},
		{Path: "/src/c.go", Action: ArtifactDeleted, Tool: "delete_file"},
	}
	manifest := FormatArtifactManifest(records)

	parsed := ParseArtifactManifest("prefix text\n" + manifest + "\nsuffix text")
	if len(parsed) != len(records) {
		t.Fatalf("round trip parsed %d records, want %d", len(parsed), len(records))
	}
	for i := range records {
		if parsed[i] != records[i] {
			t.Errorf("record %d = %+v, want %+v", i, parsed[i], records[i])
		}
	}
}

func TestParseArtifactManifest_Malformed(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantLen int
	}{
		{name: "no manifest", content: "plain text", wantLen: 0},
		{name: "unclosed manifest", content: "<artifact_manifest>\nwritten | /a.go | write_file", wantLen: 0},
		{name: "malformed lines skipped", content: "<artifact_manifest>\ngarbage\nunknown | /x | y\nmodified | /a.go | edit_file\n</artifact_manifest>", wantLen: 1},
		{name: "empty manifest", content: "<artifact_manifest>\n</artifact_manifest>", wantLen: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseArtifactManifest(tt.content); len(got) != tt.wantLen {
				t.Fatalf("parsed %d records, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestCompressionStrategy_AppendArtifactManifest(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "work"},
		toolCallMsg(tools.ToolCall{ID: "c1", Name: "write_file", Arguments: map[string]interface{}{"file_path": "/a.go"}}),
		{Role: "tool", Content: "ok", ToolCallID: "c1"},
	}

	tests := []struct {
		name         string
		cfg          *config.Config
		wantManifest bool
	}{
		{name: "nil config defaults to enabled", cfg: nil, wantManifest: true},
		{name: "explicitly enabled", cfg: &config.Config{ContextConfig: config.ContextConfig{EnableArtifactTracking: true}}, wantManifest: true},
		{name: "disabled leaves content untouched", cfg: &config.Config{ContextConfig: config.ContextConfig{EnableArtifactTracking: false}}, wantManifest: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := NewCompressionStrategyWithConfig(1000, 0.3, 3)
			cs.cfg = tt.cfg
			content := cs.appendArtifactManifest("SUMMARY", messages)
			hasManifest := strings.Contains(content, artifactManifestOpen)
			if hasManifest != tt.wantManifest {
				t.Fatalf("manifest present = %v, want %v (content: %q)", hasManifest, tt.wantManifest, content)
			}
			if tt.wantManifest {
				if !strings.Contains(content, "written | /a.go | write_file") {
					t.Fatalf("manifest missing record: %q", content)
				}
				if !strings.HasPrefix(content, "SUMMARY") {
					t.Fatalf("manifest must be appended after the summary, got %q", content)
				}
			} else if content != "SUMMARY" {
				t.Fatalf("content modified while tracking disabled: %q", content)
			}
		})
	}
}

func TestCompressionStrategy_AppendArtifactManifest_NoArtifacts(t *testing.T) {
	cs := NewCompressionStrategyWithConfig(1000, 0.3, 3)
	messages := []llm.Message{{Role: "user", Content: "just chatting"}}
	if got := cs.appendArtifactManifest("SUMMARY", messages); got != "SUMMARY" {
		t.Fatalf("expected content unchanged when no artifacts, got %q", got)
	}
}
