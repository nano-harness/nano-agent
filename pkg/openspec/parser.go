package openspec

import (
	"regexp"
	"strings"
)

// taskLineRegexp matches a markdown checkbox line like "- [ ] 1.1 Some task" or "- [x] 2.3 Another task".
var taskLineRegexp = regexp.MustCompile(`^[-*]\s+\[([ xX])\]\s+(\d+(?:\.\d+)?)\s+(.+)$`)

// groupHeadingRegexp matches a markdown heading at level 2 or deeper (##, ###, etc.)
// like "## 1. Group Name" or "## Group Name". Level-1 headings (# Title) are
// intentionally excluded to avoid treating the document title as a task group.
var groupHeadingRegexp = regexp.MustCompile(`^#{2,}\s+(?:\d+\.\s*)?(.+)$`)

// ParseTasks parses a tasks.md file content into a list of Task structs.
// It recognizes markdown checklist items and optional group headings.
func ParseTasks(content string) []Task {
	lines := strings.Split(content, "\n")
	var tasks []Task
	currentGroup := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for group heading
		if match := groupHeadingRegexp.FindStringSubmatch(trimmed); match != nil {
			currentGroup = strings.TrimSpace(match[1])
			continue
		}

		// Check for task checkbox
		if match := taskLineRegexp.FindStringSubmatch(trimmed); match != nil {
			status := TaskStatusPending
			if strings.EqualFold(match[1], "x") {
				status = TaskStatusComplete
			}
			tasks = append(tasks, Task{
				ID:          match[2],
				Description: strings.TrimSpace(match[3]),
				Status:      status,
				GroupName:   currentGroup,
			})
		}
	}

	return tasks
}

// FormatTasks converts a list of tasks back into markdown checklist format.
func FormatTasks(tasks []Task) string {
	var sb strings.Builder
	currentGroup := ""

	for _, t := range tasks {
		if t.GroupName != currentGroup {
			currentGroup = t.GroupName
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("## ")
			sb.WriteString(currentGroup)
			sb.WriteString("\n")
		}

		check := " "
		if t.Status == TaskStatusComplete {
			check = "x"
		}
		sb.WriteString("- [")
		sb.WriteString(check)
		sb.WriteString("] ")
		sb.WriteString(t.ID)
		sb.WriteString(" ")
		sb.WriteString(t.Description)
		sb.WriteString("\n")
	}

	return sb.String()
}

// UpdateTaskStatus finds a task by ID and updates its status, returning the
// modified content. If the task is not found, the original content is returned.
func UpdateTaskStatus(content string, taskID string, newStatus TaskStatus) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if match := taskLineRegexp.FindStringSubmatch(trimmed); match != nil {
			if match[2] == taskID {
				oldCheck := match[1]
				newCheck := " "
				if newStatus == TaskStatusComplete {
					newCheck = "x"
				}
				lines[i] = strings.Replace(line, "["+oldCheck+"]", "["+newCheck+"]", 1)
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}
