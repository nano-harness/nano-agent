package openspec

import (
	"testing"
)

func TestParseTasks(t *testing.T) {
	content := `# Tasks

## 1. Theme Infrastructure
- [x] 1.1 Create ThemeContext with light/dark state
- [ ] 1.2 Add CSS custom properties for colors
- [x] 1.3 Implement localStorage persistence

## 2. UI Components
- [ ] 2.1 Create ThemeToggle component
- [ ] 2.2 Add toggle to settings page
`

	tasks := ParseTasks(content)
	if len(tasks) != 5 {
		t.Fatalf("expected 5 tasks, got %d", len(tasks))
	}

	// Check first task
	if tasks[0].ID != "1.1" {
		t.Errorf("task[0].ID = %q, want '1.1'", tasks[0].ID)
	}
	if tasks[0].Status != TaskStatusComplete {
		t.Error("task[0] should be complete")
	}
	if tasks[0].GroupName != "Theme Infrastructure" {
		t.Errorf("task[0].GroupName = %q, want 'Theme Infrastructure'", tasks[0].GroupName)
	}

	// Check incomplete task
	if tasks[1].ID != "1.2" {
		t.Errorf("task[1].ID = %q, want '1.2'", tasks[1].ID)
	}
	if tasks[1].Status != TaskStatusPending {
		t.Error("task[1] should be pending")
	}

	// Check group change
	if tasks[3].GroupName != "UI Components" {
		t.Errorf("task[3].GroupName = %q, want 'UI Components'", tasks[3].GroupName)
	}
}

func TestParseTasksEmpty(t *testing.T) {
	tasks := ParseTasks("")
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks from empty content, got %d", len(tasks))
	}
}

func TestParseTasksNoCheckboxes(t *testing.T) {
	content := "# Tasks\n\nSome description without checkboxes.\n"
	tasks := ParseTasks(content)
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	content := `- [ ] 1.1 First task
- [ ] 1.2 Second task
- [x] 1.3 Third task`

	// Mark 1.1 complete
	updated := UpdateTaskStatus(content, "1.1", TaskStatusComplete)
	tasks := ParseTasks(updated)
	if tasks[0].Status != TaskStatusComplete {
		t.Error("task 1.1 should now be complete")
	}

	// Mark 1.3 incomplete
	updated = UpdateTaskStatus(updated, "1.3", TaskStatusPending)
	tasks = ParseTasks(updated)
	if tasks[2].Status != TaskStatusPending {
		t.Error("task 1.3 should now be pending")
	}
}

func TestUpdateTaskStatusNonexistent(t *testing.T) {
	content := "- [ ] 1.1 A task\n"
	updated := UpdateTaskStatus(content, "9.9", TaskStatusComplete)
	if updated != content {
		t.Error("content should be unchanged for nonexistent task")
	}
}

func TestFormatTasks(t *testing.T) {
	tasks := []Task{
		{ID: "1.1", Description: "First task", Status: TaskStatusComplete, GroupName: "Setup"},
		{ID: "1.2", Description: "Second task", Status: TaskStatusPending, GroupName: "Setup"},
		{ID: "2.1", Description: "Third task", Status: TaskStatusPending, GroupName: "Build"},
	}

	result := FormatTasks(tasks)

	// Parse back to verify round-trip
	parsed := ParseTasks(result)
	if len(parsed) != 3 {
		t.Fatalf("round-trip produced %d tasks, expected 3", len(parsed))
	}
	if parsed[0].Status != TaskStatusComplete {
		t.Error("first task should be complete after round-trip")
	}
	if parsed[1].Status != TaskStatusPending {
		t.Error("second task should be pending after round-trip")
	}
}
