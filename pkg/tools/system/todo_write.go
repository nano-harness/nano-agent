package system

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
)

// TodoItem represents a single todo item
type TodoItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`   // pending, in_progress, completed
	Priority string `json:"priority"` // high, medium, low
}

// TodoList represents a collection of todo items
type TodoList struct {
	Items       []TodoItem `json:"todos"`
	Explanation string     `json:"explanation,omitempty"`
}

// TodoWriteTool implements a tool for creating and managing todo lists
type TodoWriteTool struct{}

// NewTodoWriteTool creates a new TodoWriteTool instance
func NewTodoWriteTool() *TodoWriteTool {
	return &TodoWriteTool{}
}

// Name returns the tool name
func (t *TodoWriteTool) Name() string {
	return "todo_write"
}

// Description returns the tool description
func (t *TodoWriteTool) Description() string {
	return "创建和管理结构化任务列表。参数：explanation为字符串；todos为数组，数组元素为对象{id?, content, status?, priority?}。status可选值：pending|in_progress|completed（支持别名：todo/doing/done、待处理/进行中/已完成）；priority可选值：high|medium|low（支持别名：高/中/低，p0/p1/p2）。todos也可为JSON字符串（数组或含todos字段的对象）。"
}

// Category returns the tool category
func (t *TodoWriteTool) Category() interfaces.ToolCategory {
	return "system"
}

// RequiresConfirmation returns whether the tool requires confirmation
func (t *TodoWriteTool) RequiresConfirmation() bool {
	return false
}

// ConcurrencySafe returns false: todo list writes mutate shared in-memory state.
func (t *TodoWriteTool) ConcurrencySafe() bool { return false }

// Schema returns the tool schema
func (t *TodoWriteTool) Schema() *interfaces.ToolSchema {
	schema := interfaces.CreateSchema(
		"创建和管理结构化的任务列表。请严格遵循参数类型。",
		map[string]*interfaces.PropertySchema{
			"explanation": interfaces.NewStringProperty("对任务列表的简要说明（必须为非空字符串）"),
			"todos":       interfaces.NewArrayProperty("任务项数组。每个元素应为对象：{id?, content, status?, priority?}。status支持pending|in_progress|completed及常见别名（todo/doing/done、待处理/进行中/已完成）；priority支持high|medium|low及p0/p1/p2、高/中/低。也支持传入JSON字符串（数组或包含todos字段的对象）", "object"),
		},
		[]string{"explanation", "todos"},
	)

	// 为LLM提供更明确的示例与用法提示
	if p := schema.Properties["explanation"]; p != nil {
		p.Examples = []string{"实现XX功能的阶段性任务规划"}
		min := 3   //nolint:revive
		max := 200 //nolint:revive
		p.MinLength = &min
		p.MaxLength = &max
		p.Usage = "简洁说明该任务列表的目的与范围"
	}
	if p := schema.Properties["todos"]; p != nil {
		p.Examples = []string{
			// JSON字符串示例（数组）
			"[{\"id\":\"task_1\",\"content\":\"搭建API骨架\",\"status\":\"pending\",\"priority\":\"high\"},{\"content\":\"补充接口文档\",\"status\":\"in_progress\",\"priority\":\"medium\"},{\"content\":\"完善单元测试\",\"status\":\"completed\",\"priority\":\"low\"}]",
			// JSON字符串示例（对象，含todos字段）
			"{\"todos\":[{\"content\":\"初始化项目\"},{\"content\":\"配置CI\"},{\"content\":\"提交README\"}],\"explanation\":\"项目初始化任务\"}",
		}
		p.Usage = "todos应为数组，元素为对象：{id?, content, status?, priority?}。content必填；status支持pending|in_progress|completed及常见别名（todo/doing/done、待处理/进行中/已完成，默认pending）；priority支持high|medium|low及p0/p1/p2、高/中/低（默认medium）。可直接传数组或其JSON字符串；也支持传入包含todos字段的JSON对象。"
	}

	return schema
}

// Execute executes the tool
func (t *TodoWriteTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	logger.Infof("TodoWriteTool executing with params: %+v", params)

	// Extract explanation
	explanation, ok := params["explanation"].(string)
	if !ok || explanation == "" {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "explanation parameter is required and must be a non-empty string",
			LLMContent:  "Error: Missing or invalid explanation parameter",
			UserContent: "错误：缺少或无效的说明参数",
		}, nil
	}

	// Extract todos
	todosParam, ok := params["todos"]
	if !ok {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       "todos parameter is required",
			LLMContent:  "Error: Missing todos parameter",
			UserContent: "错误：缺少todos参数",
		}, nil
	}

	// Parse todos
	todoList, err := t.parseTodos(todosParam)
	if err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to parse todos: %v", err),
			LLMContent:  fmt.Sprintf("Error parsing todos: %v", err),
			UserContent: fmt.Sprintf("解析任务列表失败：%v", err),
		}, nil
	}

	todoList.Explanation = explanation

	// 规范化与唯一性处理
	ensureUniqueIDs(todoList)

	// Validate todo list
	if err := t.validateTodoList(todoList); err != nil {
		return &interfaces.ToolResult{
			Success:     false,
			Error:       fmt.Sprintf("validation failed: %v", err),
			LLMContent:  fmt.Sprintf("Validation error: %v", err),
			UserContent: fmt.Sprintf("验证失败：%v", err),
		}, nil
	}

	// Format output
	llmContent := t.formatTodoListForLLM(todoList, explanation)
	userContent := t.formatTodoListForUser(todoList, explanation)

	logger.Infof("TodoWriteTool completed successfully with %d items", len(todoList.Items))

	// 统计信息
	pendingCount := 0
	inProgressCount := 0
	completedCount := 0
	for _, item := range todoList.Items {
		switch item.Status {
		case "pending":
			pendingCount++
		case "in_progress":
			inProgressCount++
		case "completed":
			completedCount++
		}
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        todoList,
		LLMContent:  llmContent,
		UserContent: userContent,
		Metadata: map[string]interface{}{
			"todo_count":        len(todoList.Items),
			"explanation":       explanation,
			"tool_name":         "todo_write",
			"pending_count":     pendingCount,
			"in_progress_count": inProgressCount,
			"completed_count":   completedCount,
		},
	}, nil
}

// parseTodos parses the todos parameter into a TodoList
func (t *TodoWriteTool) parseTodos(todosParam interface{}) (*TodoList, error) {
	todoList := &TodoList{Items: []TodoItem{}}

	switch v := todosParam.(type) {
	case []interface{}:
		for i, item := range v {
			todoItem, err := t.parseToDoItem(item, fmt.Sprintf("task_%d", i+1))
			if err != nil {
				return nil, fmt.Errorf("第%d项解析失败：%v", i+1, err)
			}
			todoList.Items = append(todoList.Items, *todoItem)
		}
	case string:
		// 尝试按JSON解析：先按数组解析，失败后再尝试对象结构
		var arr []interface{}
		if err := json.Unmarshal([]byte(v), &arr); err == nil {
			for i, item := range arr {
				todoItem, err := t.parseToDoItem(item, fmt.Sprintf("task_%d", i+1))
				if err != nil {
					return nil, fmt.Errorf("第%d项解析失败：%v", i+1, err)
				}
				todoList.Items = append(todoList.Items, *todoItem)
			}
			break
		}

		// 尝试对象结构：{ todos: [...], explanation?: "..." }
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(v), &obj); err == nil {
			rawTodos, ok := obj["todos"]
			if !ok {
				return nil, fmt.Errorf("JSON对象不包含'todos'字段")
			}
			// 递归解析内部todos
			inner, err := t.parseTodos(rawTodos)
			if err != nil {
				return nil, err
			}
			todoList.Items = append(todoList.Items, inner.Items...)
		} else {
			return nil, fmt.Errorf("todos必须是数组或其JSON字符串（可为数组或对象）")
		}
	default:
		return nil, fmt.Errorf("todos参数类型不支持：应为数组或JSON字符串")
	}

	return todoList, nil
}

// parseToDoItem parses a single todo item
func (t *TodoWriteTool) parseToDoItem(item interface{}, defaultID string) (*TodoItem, error) {
	switch v := item.(type) {
	case map[string]interface{}:
		todoItem := &TodoItem{
			ID:       defaultID,
			Status:   "pending",
			Priority: "medium",
		}

		if id, ok := v["id"].(string); ok && id != "" {
			todoItem.ID = id
		}
		// content字段必填，支持别名：title、description
		if content, ok := v["content"].(string); ok {
			todoItem.Content = strings.TrimSpace(content)
		} else if title, ok := v["title"].(string); ok {
			todoItem.Content = strings.TrimSpace(title)
		} else if desc, ok := v["description"].(string); ok {
			todoItem.Content = strings.TrimSpace(desc)
		}

		if status, ok := v["status"].(string); ok {
			todoItem.Status = normalizeStatus(status)
		}
		if priority, ok := v["priority"].(string); ok {
			todoItem.Priority = normalizePriority(priority)
		}

		if todoItem.Content == "" {
			return nil, fmt.Errorf("缺少必填字段'content'（或别名title/description）")
		}

		return todoItem, nil
	case string:
		return &TodoItem{
			ID:       defaultID,
			Content:  strings.TrimSpace(v),
			Status:   "pending",
			Priority: "medium",
		}, nil
	default:
		return nil, fmt.Errorf("无效的任务项格式：需要对象或字符串")
	}
}

// validateTodoList validates the todo list
func (t *TodoWriteTool) validateTodoList(todoList *TodoList) error {
	if len(todoList.Items) < 3 {
		return fmt.Errorf("todo list must contain at least 3 items, got %d", len(todoList.Items))
	}
	if len(todoList.Items) > 10 {
		return fmt.Errorf("todo list must contain at most 10 items, got %d", len(todoList.Items))
	}

	validStatuses := map[string]bool{"pending": true, "in_progress": true, "completed": true}
	validPriorities := map[string]bool{"high": true, "medium": true, "low": true}

	inProgressSeen := 0
	ids := make(map[string]bool)
	for i, item := range todoList.Items {
		if item.Content == "" {
			return fmt.Errorf("todo item %d: content cannot be empty", i+1)
		}
		if !validStatuses[item.Status] {
			return fmt.Errorf("todo item %d: invalid status '%s'", i+1, item.Status)
		}
		if !validPriorities[item.Priority] {
			return fmt.Errorf("todo item %d: invalid priority '%s'", i+1, item.Priority)
		}
		if item.Status == "in_progress" {
			inProgressSeen++
		}
		if item.ID == "" {
			return fmt.Errorf("todo item %d: id cannot be empty", i+1)
		}
		if ids[item.ID] {
			return fmt.Errorf("duplicate todo id '%s' detected", item.ID)
		}
		ids[item.ID] = true
	}

	if inProgressSeen > 1 {
		return fmt.Errorf("at most one item can be in_progress, got %d", inProgressSeen)
	}

	return nil
}

// formatTodoListForLLM formats the todo list for LLM consumption
func (t *TodoWriteTool) formatTodoListForLLM(todoList *TodoList, explanation string) string {
	content := fmt.Sprintf("Todo list created successfully with %d items.\n\nExplanation: %s\n\nTasks:\n", len(todoList.Items), explanation)

	for _, item := range todoList.Items {
		status := strings.ToUpper(item.Status)
		priority := strings.ToUpper(item.Priority)
		content += fmt.Sprintf("- [%s] %s (ID: %s, Priority: %s)\n", status, item.Content, item.ID, priority)
	}

	content += "\nGuidelines: Keep ONE item in IN_PROGRESS; update statuses promptly; keep tasks原子且不超过14个词；始终包含已完成项；保持ID唯一稳定。"
	return content
}

// formatTodoListForUser formats the todo list for user display
func (t *TodoWriteTool) formatTodoListForUser(todoList *TodoList, explanation string) string {
	content := fmt.Sprintf("📋 **任务列表已创建**\n\n💡 **说明**: %s\n\n", explanation)

	// Group by status
	pending := []TodoItem{}
	inProgress := []TodoItem{}
	completed := []TodoItem{}

	for _, item := range todoList.Items {
		switch item.Status {
		case "pending":
			pending = append(pending, item)
		case "in_progress":
			inProgress = append(inProgress, item)
		case "completed":
			completed = append(completed, item)
		}
	}

	// Show in progress first
	if len(inProgress) > 0 {
		content += "🔄 **进行中**:\n"
		for _, item := range inProgress {
			priority := t.getPriorityEmoji(item.Priority)
			content += fmt.Sprintf("  %s %s. %s\n", priority, item.ID, item.Content)
		}
		content += "\n"
	}

	// Then pending
	if len(pending) > 0 {
		content += "⏳ **待处理**:\n"
		for _, item := range pending {
			priority := t.getPriorityEmoji(item.Priority)
			content += fmt.Sprintf("  %s %s. %s\n", priority, item.ID, item.Content)
		}
		content += "\n"
	}

	// Finally completed
	if len(completed) > 0 {
		content += "✅ **已完成**:\n"
		for _, item := range completed {
			priority := t.getPriorityEmoji(item.Priority)
			content += fmt.Sprintf("  %s %s. %s\n", priority, item.ID, item.Content)
		}
		content += "\n"
	}

	content += fmt.Sprintf("📊 **统计**: 总计 %d 项任务\n\n", len(todoList.Items))

	// 强提示规范
	content += "📌 **规范提示**:\n"
	content += "  - 保持仅一个进行中项\n"
	content += "  - 及时更新状态\n"
	content += "  - 任务原子且不超过14词\n"
	content += "  - 始终包含已完成项\n"
	content += "  - 保持ID唯一稳定\n"

	return content
}

// getPriorityEmoji returns emoji for priority level
func (t *TodoWriteTool) getPriorityEmoji(priority string) string {
	switch priority {
	case "high":
		return "🔴"
	case "medium":
		return "🟡"
	case "low":
		return "🟢"
	default:
		return "⚪"
	}
}

// normalizeStatus 统一大小写与空白
func normalizeStatus(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "pending", "in_progress", "completed":
		return v
	case "todo", "to_do", "to-do", "待办", "未开始", "待处理":
		return "pending"
	case "doing", "inprogress", "in progress", "处理中", "进行中", "正在进行":
		return "in_progress"
	case "done", "已完成", "完成":
		return "completed"
	default:
		return "pending"
	}
}

// normalizePriority 统一大小写与空白
func normalizePriority(p string) string {
	v := strings.ToLower(strings.TrimSpace(p))
	switch v {
	case "high", "medium", "low":
		return v
	case "p0", "high_priority", "高":
		return "high"
	case "p1", "中":
		return "medium"
	case "p2", "低":
		return "low"
	default:
		return "medium"
	}
}

func ensureUniqueIDs(todoList *TodoList) {
	used := make(map[string]int)
	for i := range todoList.Items {
		id := strings.TrimSpace(todoList.Items[i].ID)
		if id == "" {
			todoList.Items[i].ID = fmt.Sprintf("task_%d", i+1)
			id = todoList.Items[i].ID
		}
		if cnt, ok := used[id]; ok {
			cnt++
			newID := fmt.Sprintf("%s-%d", id, cnt)
			for {
				if _, exists := used[newID]; !exists {
					break
				}
				cnt++
				newID = fmt.Sprintf("%s-%d", id, cnt)
			}
			used[id] = cnt
			todoList.Items[i].ID = newID
			used[newID] = 1
		} else {
			used[id] = 1
		}
	}
}
