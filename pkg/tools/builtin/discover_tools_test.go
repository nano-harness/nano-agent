package builtin

import (
	"context"
	"testing"
)

func TestDiscoverToolsTool_InvokesOnExpand(t *testing.T) {
	tool := NewDiscoverToolsTool(func(name string) (string, bool) {
		return `{"type":"object"}`, true
	})
	var captured string
	tool.SetOnExpand(func(name string) { captured = name })
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"name": "read_file"}); err != nil {
		t.Fatal(err)
	}
	if captured != "read_file" {
		t.Fatalf("onExpand captured %q, want read_file", captured)
	}
}

func TestDiscoverToolsTool_NilOnExpandIsSafe(t *testing.T) {
	tool := NewDiscoverToolsTool(func(name string) (string, bool) {
		return `{"type":"object"}`, true
	})
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"name": "read_file"}); err != nil {
		t.Fatal(err)
	}
}
