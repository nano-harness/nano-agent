package bubbletea_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ui "github.com/nano-harness/nano-agent/pkg/ui/bubbletea"
	btuitest "github.com/nano-harness/nano-agent/pkg/ui/bubbletea/testing"
)

func TestFilePicker_HashTriggersActive(t *testing.T) {
	m := newFilePickerModel(t)
	typeText(m, "#mod")

	active, query, _, results := m.FilePickerState()
	if !active || query != "mod" || len(results) == 0 {
		t.Fatalf("FilePickerState() active=%v query=%q results=%#v", active, query, results)
	}
}

func TestFilePicker_DetectContext_NoHash(t *testing.T) {
	m := newFilePickerModel(t)
	typeText(m, "mod")

	active, _, _, _ := m.FilePickerState()
	if active {
		t.Fatal("file picker should be inactive without a hash trigger")
	}
}

func TestFilePicker_NavigationArrows(t *testing.T) {
	m := newFilePickerModel(t)
	typeText(m, "#")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, _, cursor, _ := m.FilePickerState()
	if cursor != 2 {
		t.Fatalf("cursor after down/down = %d, want 2", cursor)
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	_, _, cursor, _ = m.FilePickerState()
	if cursor != 1 {
		t.Fatalf("cursor after up = %d, want 1", cursor)
	}
}

func TestFilePicker_EnterInsertsPath(t *testing.T) {
	m := newFilePickerModel(t)
	typeText(m, "#go.mod")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := m.InputValue(); got != "@go.mod " {
		t.Fatalf("InputValue() = %q, want selected path", got)
	}
}

func TestFilePicker_EscCloses(t *testing.T) {
	m := newFilePickerModel(t)
	typeText(m, "#go")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	active, _, _, _ := m.FilePickerState()
	if active {
		t.Fatal("file picker should close on esc")
	}
	if got := m.InputValue(); got != "#go" {
		t.Fatalf("InputValue() = %q, want preserved input", got)
	}
}

func TestFilePicker_MultipleHashes(t *testing.T) {
	m := newFilePickerModel(t)
	typeText(m, "#go.mod ")
	typeText(m, "#README")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := m.InputValue(); got != "#go.mod @README.md " {
		t.Fatalf("InputValue() = %q, want both file mentions", got)
	}
}

func TestFilePicker_TeatestShowsPicker(t *testing.T) {
	m := newFilePickerModel(t)
	tm := btuitest.NewTeatestModel(t, m)
	t.Cleanup(func() { _ = tm.Quit() })
	tm.Type("#mod")
	btuitest.WaitForText(t, tm, "go\\.mod", time.Second)
}

func newFilePickerModel(t *testing.T) *ui.Model {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"go.mod", "README.md", "pkg/model.go"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ui.New(nil, nil, "", dir)
}

func typeText(m *ui.Model, s string) {
	for _, r := range s {
		_, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}
