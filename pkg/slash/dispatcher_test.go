package slash

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/team"
)

type fakeCheckpointer struct {
	creates  []string
	restores []string
	infos    []CheckpointInfo
	err      error
}

func (f *fakeCheckpointer) Create(reason string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.creates = append(f.creates, reason)
	return "ckpt-1", nil
}
func (f *fakeCheckpointer) List() ([]CheckpointInfo, error) { return f.infos, f.err }
func (f *fakeCheckpointer) Restore(id string) error {
	if f.err != nil {
		return f.err
	}
	f.restores = append(f.restores, id)
	return nil
}

func TestLocalDispatcher_NotHandled(t *testing.T) {
	d := NewLocalDispatcher("", t.TempDir())
	for _, in := range []string{"hello", "/yolo", "/permission default", "/think on", "/clear", "/new"} {
		if r := d.Dispatch(in); r.Handled {
			t.Errorf("expected %q not handled, got %+v", in, r)
		}
	}
}

func TestLocalDispatcher_CheckpointNotEnabled(t *testing.T) {
	d := NewLocalDispatcher("", t.TempDir())
	for _, in := range []string{"/checkpoint", "/checkpoints", "/restore abc"} {
		r := d.Dispatch(in)
		if !r.Handled {
			t.Fatalf("expected %q handled", in)
		}
		if r.Level != "warning" || !strings.Contains(r.Message, "Checkpointing") {
			t.Errorf("expected friendly degradation for %q, got %+v", in, r)
		}
	}
}

func TestLocalDispatcher_CheckpointEnabled(t *testing.T) {
	fc := &fakeCheckpointer{
		infos: []CheckpointInfo{{ID: "id1", CreatedAt: "now", Reason: "before refactor", FileCount: 3, TotalBytes: 42}},
	}
	d := NewLocalDispatcher("", t.TempDir()).WithCheckpointer(fc)

	if r := d.Dispatch("/checkpoint pre-refactor"); !r.Handled || r.Level != "success" {
		t.Errorf("expected success, got %+v", r)
	}
	if len(fc.creates) != 1 || fc.creates[0] != "pre-refactor" {
		t.Errorf("expected create('pre-refactor'), got %v", fc.creates)
	}

	r := d.Dispatch("/checkpoints")
	if !strings.Contains(r.Message, "id1") || !strings.Contains(r.Message, "before refactor") {
		t.Errorf("expected list to include checkpoint info, got %q", r.Message)
	}

	r = d.Dispatch("/restore id1")
	if r.Level != "success" || len(fc.restores) != 1 {
		t.Errorf("expected restore, got %+v", r)
	}

	r = d.Dispatch("/restore")
	if r.Level != "error" {
		t.Errorf("expected error for missing id, got %+v", r)
	}
}

func TestLocalDispatcher_CheckpointError(t *testing.T) {
	fc := &fakeCheckpointer{err: errors.New("disk full")}
	d := NewLocalDispatcher("", t.TempDir()).WithCheckpointer(fc)
	r := d.Dispatch("/checkpoint")
	if r.Level != "error" || !strings.Contains(r.Message, "disk full") {
		t.Errorf("expected error with disk full, got %+v", r)
	}
}

func TestLocalDispatcher_Models(t *testing.T) {
	d := NewLocalDispatcher("", t.TempDir())
	r := d.Dispatch("/models")
	if !r.Handled || r.Message == "" {
		t.Errorf("expected default models hint, got %+v", r)
	}

	d2 := NewLocalDispatcher("", t.TempDir()).WithModelLister(func() string { return "model-A\nmodel-B" })
	r2 := d2.Dispatch("/models")
	if !strings.Contains(r2.Message, "model-A") {
		t.Errorf("expected lister output, got %+v", r2)
	}

	r = d.Dispatch("/model use gpt-4")
	if !r.Handled || r.Level != "warning" || !strings.Contains(r.Message, "未连接模型切换器") {
		t.Errorf("expected /model use to degrade without switcher, got %+v", r)
	}
}

func TestLocalDispatcher_Skill(t *testing.T) {
	d := NewLocalDispatcher("", t.TempDir()).WithSkillLister(func() string { return "skill-a\nskill-b" })
	r := d.Dispatch("/skill:list")
	if !strings.Contains(r.Message, "skill-a") {
		t.Errorf("expected skill listing, got %+v", r)
	}
	r = d.Dispatch("/skill:info")
	if r.Level != "error" {
		t.Errorf("expected error for missing arg, got %+v", r)
	}
	r = d.Dispatch("/skill:use foo")
	if r.Handled {
		t.Errorf("expected skill:use with arg to fall through to agent path, got %+v", r)
	}
}

func TestLocalDispatcher_Routines(t *testing.T) {
	d := NewLocalDispatcher("", t.TempDir()).WithRoutinesLister(func() string { return "(no routines)" })
	r := d.Dispatch("/routines list")
	if !strings.Contains(r.Message, "no routines") {
		t.Errorf("expected lister output, got %+v", r)
	}

	r = NewLocalDispatcher("", t.TempDir()).Dispatch("/routines list")
	if !r.Handled || strings.Contains(r.Message, "daemon") || strings.Contains(r.Message, "需 daemon") {
		t.Errorf("expected daemon-free list fallback, got %+v", r)
	}

	d = NewLocalDispatcher("", t.TempDir()).
		WithRoutinesLister(func() string { return "all routines" }).
		WithRunningStatusLister(func() string { return "running routines" })
	r = d.Dispatch("/routines status")
	if !strings.Contains(r.Message, "running routines") {
		t.Errorf("expected running status lister output, got %+v", r)
	}

	d = NewLocalDispatcher("", t.TempDir()).WithRoutinesLister(func() string { return "all routines" })
	r = d.Dispatch("/routines status")
	if !strings.Contains(r.Message, "all routines") {
		t.Errorf("expected status fallback to routines lister, got %+v", r)
	}

	r = NewLocalDispatcher("", t.TempDir()).Dispatch("/routines status")
	if !r.Handled || !strings.Contains(r.Message, "nano routines list") || strings.Contains(r.Message, "daemon") {
		t.Errorf("expected status fallback hint, got %+v", r)
	}

	var added, removed, paused, resumed, run string
	d = NewLocalDispatcher("", t.TempDir()).
		WithRoutinesAdder(func(s string) string { added = s; return "added " + s }).
		WithRoutinesRemover(func(s string) string { removed = s; return "removed " + s }).
		WithRoutinesPauser(func(s string) string { paused = s; return "paused " + s }).
		WithRoutinesResumer(func(s string) string { resumed = s; return "resumed " + s }).
		WithRoutinesRunner(func(s string) string { run = s; return "run " + s })
	for _, tc := range []struct {
		input string
		want  *string
		arg   string
	}{
		{"/routines add every 5 minutes run echo hi", &added, "every 5 minutes run echo hi"},
		{"/routines remove task-1", &removed, "task-1"},
		{"/routines pause task-1", &paused, "task-1"},
		{"/routines resume task-1", &resumed, "task-1"},
		{"/routines run task-1", &run, "task-1"},
	} {
		r = d.Dispatch(tc.input)
		if !r.Handled || r.Level != "success" || *tc.want != tc.arg {
			t.Errorf("expected %q callback success, got result=%+v callback=%q", tc.input, r, *tc.want)
		}
	}

	// Test /routines run without argument
	r = d.Dispatch("/routines run")
	if !r.Handled || r.Level != "error" {
		t.Errorf("expected /routines run without arg to return error, got %+v", r)
	}

	// Test /routines run without handler
	d2 := NewLocalDispatcher("", t.TempDir())
	r = d2.Dispatch("/routines run task-1")
	if !r.Handled || r.Level != "warning" || !strings.Contains(r.Message, "未连接") {
		t.Errorf("expected /routines run without handler to return warning, got %+v", r)
	}
}

func TestLocalDispatcher_Opsx(t *testing.T) {
	d := NewLocalDispatcher("", t.TempDir())
	r := d.Dispatch("/opsx:propose my-change")
	if r.Handled {
		t.Errorf("expected valid opsx command to fall through to agent path, got %+v", r)
	}
	r = d.Dispatch("/opsx:propose")
	if !r.Handled || !strings.Contains(r.Message, "change-name") {
		t.Errorf("expected usage prompt, got %+v", r)
	}
}

func TestLocalDispatcher_AgentsListsTeams(t *testing.T) {
	prevList := teamLister
	prevLoad := teamLoader
	t.Cleanup(func() { teamLister = prevList; teamLoader = prevLoad })

	teamLister = func() ([]*team.Team, error) {
		return []*team.Team{{
			Name: "alpha",
			Members: []team.TeamMember{
				{Name: "researcher", Kind: "subprocess", Mode: "default", IsActive: true, AgentID: "researcher@alpha"},
				{Name: "builder", Kind: "in_process", IsActive: false, AgentID: "builder@alpha"},
			},
		}}, nil
	}

	d := NewLocalDispatcher("", t.TempDir())
	r := d.Dispatch("/agents")
	if !r.Handled {
		t.Fatalf("expected handled, got %+v", r)
	}
	if !strings.Contains(r.Message, "researcher") || !strings.Contains(r.Message, "builder") {
		t.Errorf("expected both teammates, got %q", r.Message)
	}
	if !strings.Contains(r.Message, "active") || !strings.Contains(r.Message, "idle") {
		t.Errorf("expected status indicators, got %q", r.Message)
	}
}

func TestLocalDispatcher_AgentProfileSlash(t *testing.T) {
	cwd := t.TempDir()
	dir := filepath.Join(cwd, ".nano", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reviewer.yaml"), []byte(`description: Review code
initial_prompt: Review the requested changes.
permission_mode: acceptEdits
model: gpt-5-mini
fallbacks: [openai/gpt-4.1]
context_providers: [memory, skills]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	d := NewLocalDispatcher("", cwd)

	r := d.Dispatch("/reviewer check pkg/agent")
	if r.Handled {
		t.Fatalf("agent profile dispatch should not set Handled, got %+v", r)
	}
	if !r.ShouldSubmit {
		t.Fatalf("expected ShouldSubmit, got %+v", r)
	}
	for _, want := range []string{
		"spawn_teammate",
		`name="reviewer"`,
		`permission_mode="acceptEdits"`,
		`model="gpt-5-mini"`,
		`fallbacks=["openai/gpt-4.1"]`,
		`context_providers="memory,skills"`,
		"check pkg/agent",
	} {
		if !strings.Contains(r.SubmitInput, want) {
			t.Errorf("SubmitInput missing %q: %s", want, r.SubmitInput)
		}
	}

	// No prompt -> profile InitialPrompt is used.
	r = d.Dispatch("/reviewer")
	if !r.ShouldSubmit {
		t.Fatalf("expected ShouldSubmit for /reviewer, got %+v", r)
	}
	if !strings.Contains(r.SubmitInput, "Review the requested changes.") {
		t.Errorf("SubmitInput missing default initial_prompt: %s", r.SubmitInput)
	}
}

func TestLocalDispatcher_UnknownSlashUnchanged(t *testing.T) {
	d := NewLocalDispatcher("", t.TempDir())
	r := d.Dispatch("/totally-unknown-command arg1 arg2")
	if r.Handled || r.ShouldSubmit {
		t.Fatalf("unknown slash should be unhandled, got %+v", r)
	}
}

func TestLocalDispatcher_BuiltinAgentSlash(t *testing.T) {
	// Use an empty cwd so no filesystem agents shadow built-ins.
	cwd := t.TempDir()
	d := NewLocalDispatcher("", cwd)

	r := d.Dispatch("/explore find all tests")
	if r.Handled {
		t.Fatalf("built-in agent dispatch should not set Handled, got %+v", r)
	}
	if !r.ShouldSubmit {
		t.Fatalf("expected ShouldSubmit for /explore, got %+v", r)
	}
	for _, want := range []string{
		"spawn_teammate",
		`name="explore"`,
		"find all tests",
	} {
		if !strings.Contains(r.SubmitInput, want) {
			t.Errorf("SubmitInput missing %q: %s", want, r.SubmitInput)
		}
	}

	// No prompt -> uses built-in InitialPrompt.
	r = d.Dispatch("/explore")
	if !r.ShouldSubmit {
		t.Fatalf("expected ShouldSubmit for /explore without prompt, got %+v", r)
	}
	if !strings.Contains(r.SubmitInput, "exploration subagent") {
		t.Errorf("SubmitInput missing built-in initial_prompt: %s", r.SubmitInput)
	}
}
