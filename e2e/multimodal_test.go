//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// MultimodalSuite 覆盖多模态输入（图像 + 文本）相关集成行为。
type MultimodalSuite struct {
	AgentTestSuite
}

func TestMultimodalSuite(t *testing.T) {
	suite.Run(t, new(MultimodalSuite))
}

func (s *MultimodalSuite) TestMultimodalExecution() {
	// Mock server 响应
	s.MockServer.AddResponse(MockResponse{
		Content: "I see a cat in the image.",
		ToolCalls: []MockToolCall{
			{
				ID:        "call_task_done",
				Name:      "task_done",
				Arguments: `{"status": "success"}`,
			},
		},
	})

	ctx := context.Background()

	// 构造一个虚拟图片（data URL）
	images := []llm.MultimodalImage{
		{
			URL:      "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEAAAAAAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/2wBDAQkJCQwLDBgNDRgyIRwhMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjL/wAARCAABAAEDASIAAhEBAxEB/8QAFQABAQAAAAAAAAAAAAAAAAAAAAf/xAAUAQEAAAAAAAAAAAAAAAAAAAAA/8QAFAEBAAAAAAAAAAAAAAAAAAAAAP/EABQRAQAAAAAAAAAAAAAAAAAAAAD/2gAMAwEAAhEDEQA/AL+AD//Z",
			MimeType: "image/jpeg",
		},
	}

	var events []event.StreamEvent
	err := s.Agent.ProcessStreamWithMultimodal(ctx, "What is in this image?", images, func(e event.StreamEvent) {
		events = append(events, e)
	})
	require.NoError(s.T(), err)

	// 验证 MockServer 收到的请求包含 image_url 内容
	require.NotEmpty(s.T(), s.MockServer.Requests, "mock server received no requests")

	req := s.MockServer.Requests[0]
	msgs, ok := req["messages"].([]interface{})
	require.True(s.T(), ok, "messages should be an array")
	lastMsg := msgs[len(msgs)-1].(map[string]interface{})

	// OpenAI 多模态格式：content 是数组
	contentArr, ok := lastMsg["content"].([]interface{})
	require.Truef(s.T(), ok, "expected content to be an array for multimodal, got %T", lastMsg["content"])

	foundImage := false
	for _, item := range contentArr {
		obj := item.(map[string]interface{})
		if obj["type"] == "image_url" {
			foundImage = true
			imgURL := obj["image_url"].(map[string]interface{})
			urlStr, _ := imgURL["url"].(string)
			if !strings.HasPrefix(urlStr, "data:image/jpeg;base64") {
				s.T().Errorf("unexpected image URL format: %s", urlStr)
			}
		}
	}
	require.True(s.T(), foundImage, "expected to find image in LLM request")

	// 校验输出事件中包含对图像内容的描述
	contentEventFound := false
	for _, e := range events {
		if e.Type == event.EventTypeContent && strings.Contains(e.Content, "cat") {
			contentEventFound = true
			break
		}
	}
	require.True(s.T(), contentEventFound, "expected to find response content mentioning cat in events")

	// 更细粒度：多模态流程应该通过 task_done 正常收尾
	AssertToolCalled(s.T(), events, "task_done")
}
