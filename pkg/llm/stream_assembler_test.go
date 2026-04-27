package llm

import "testing"

func TestStreamAssemblerAssemblesToolCallChunks(t *testing.T) {
	assembler := NewStreamAssembler()
	assembler.AddContent("hello")
	if !assembler.AddToolCallDelta(0, "call-1", "read_file", `{"path":`) {
		t.Fatal("expected first name delta to be counted")
	}
	if assembler.AddToolCallDelta(0, "", "", `"README.md"}`) {
		t.Fatal("did not expect second argument delta to count name")
	}

	calls := assembler.FinalizeToolCalls(nil)
	if assembler.Content() != "hello" {
		t.Fatalf("content = %q, want hello", assembler.Content())
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
	if calls[0].Arguments["path"] != "README.md" {
		t.Fatalf("path = %v, want README.md", calls[0].Arguments["path"])
	}
}
