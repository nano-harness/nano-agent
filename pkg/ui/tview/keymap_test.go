package tview

import (
	"strings"
	"testing"
)

func TestFormatKeyHints(t *testing.T) {
	got := FormatKeyHints([]KeyHint{
		{Key: "Ctrl+P", Desc: "命令"},
		{Key: "Ctrl+L", Desc: "新会话"},
	})
	want := "Ctrl+P 命令 | Ctrl+L 新会话"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestDefaultKeyHints_Coverage(t *testing.T) {
	out := formatStatusHints()
	for _, needle := range []string{"Ctrl+P", "Ctrl+L", "Ctrl+Z", "Tab", "q"} {
		if !strings.Contains(out, needle) {
			t.Errorf("default hints missing %q: %q", needle, out)
		}
	}
}
