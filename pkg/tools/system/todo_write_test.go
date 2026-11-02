package system

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSchemaExamplesAndUsage(t *testing.T) {
	tool := NewTodoWriteTool()
	schema := tool.Schema()
	if schema == nil {
		t.Fatalf("Schema should not be nil")
	}
	todosProp := schema.Properties["todos"]
	if todosProp == nil {
		t.Fatalf("Schema must include 'todos' property")
	}
	if len(todosProp.Examples) == 0 {
		t.Fatalf("'todos' property should contain examples for LLM guidance")
	}
	if todosProp.Usage == "" {
		t.Fatalf("'todos' property should contain usage guidance")
	}
}

func TestParseTodosArrayOfObjects(t *testing.T) {
	tool := NewTodoWriteTool()
	// Construct []interface{} with objects
	items := []interface{}{
		map[string]interface{}{"id": "task_1", "content": "搭建API骨架", "status": "PENDING", "priority": "HIGH"},
		map[string]interface{}{"content": "补充接口文档", "status": "in_progress", "priority": "medium"},
		map[string]interface{}{"title": "完善单元测试", "priority": "low"},
	}
	list, err := tool.parseTodos(items)
	if err != nil {
		t.Fatalf("parseTodos failed: %v", err)
	}
	if len(list.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list.Items))
	}
	if list.Items[0].Status != "pending" || list.Items[0].Priority != "high" {
		t.Fatalf("normalization failed: status=%s priority=%s", list.Items[0].Status, list.Items[0].Priority)
	}
	if list.Items[2].Content != "完善单元测试" {
		t.Fatalf("alias handling failed for title -> content")
	}
}

func TestParseTodosJSONStringArray(t *testing.T) {
	tool := NewTodoWriteTool()
	// JSON array string
	jsonArr := `[{"content":"任务一"},{"content":"任务二"},{"content":"任务三"}]`
	list, err := tool.parseTodos(jsonArr)
	if err != nil {
		t.Fatalf("parseTodos failed for JSON array: %v", err)
	}
	if len(list.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list.Items))
	}
}

func TestParseTodosJSONStringObject(t *testing.T) {
	tool := NewTodoWriteTool()
	obj := map[string]interface{}{
		"explanation": "测试对象结构",
		"todos": []map[string]interface{}{
			{"content": "A"}, {"content": "B"}, {"content": "C"},
		},
	}
	b, _ := json.Marshal(obj)
	list, err := tool.parseTodos(string(b))
	if err != nil {
		t.Fatalf("parseTodos failed for JSON object: %v", err)
	}
	if len(list.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list.Items))
	}
}

func TestExecuteSuccess(t *testing.T) {
	tool := NewTodoWriteTool()
	params := map[string]interface{}{
		"explanation": "示例说明",
		"todos": []interface{}{
			map[string]interface{}{"content": "A"},
			map[string]interface{}{"content": "B"},
			map[string]interface{}{"content": "C"},
		},
	}
	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !res.Success {
		t.Fatalf("Execute should succeed, got: %+v", res)
	}
}
