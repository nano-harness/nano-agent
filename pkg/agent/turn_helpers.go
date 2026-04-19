package agent

import (
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/llm"
)

// buildUserContextPrefix returns a one-line context note (time, timezone, language)
// to prepend to each user message so the AI always has this basic info available.
func (t *Turn) buildUserContextPrefix() string {
	if t.SystemPromptBuilder == nil {
		return ""
	}
	return t.SystemPromptBuilder.BuildUserContextNote() + "\n\n"
}

// ensureSystemPrompt ensures the system prompt is present in the messages
func (t *Turn) ensureSystemPrompt() {
	if t.systemPrompt == "" {
		t.systemPrompt = t.buildUnifiedSystemPrompt()
	}

	hasSystem := false
	for _, msg := range t.Messages {
		if msg.Role == "system" {
			hasSystem = true
			break
		}
	}
	if !hasSystem && t.systemPrompt != "" {
		t.Messages = append([]llm.Message{{Role: "system", Content: t.systemPrompt}}, t.Messages...)
	}
}

// ensureUserMessage ensures the user message is added exactly once per turn
func (t *Turn) ensureUserMessage() {
	if t.UserInput != "" && !t.userMessageAdded {
		contextPrefix := t.buildUserContextPrefix()
		userMessage := llm.Message{Role: "user", Content: contextPrefix + t.UserInput}

		// Add multimodal images if provided
		if t.images != nil && len(t.images) > 0 { //nolint:staticcheck
			userMessage.Contents = make([]llm.MessageContent, 0, len(t.images)+1)

			// Append image URLs to the text so the LLM knows the exact URLs for tool calls
			textWithUrls := contextPrefix + t.UserInput
			var urlContext string
			for i, img := range t.images {
				if img.URL != "" {
					urlContext += fmt.Sprintf("\n[Image %d URL]: %s", i+1, img.URL)
				}
			}
			if urlContext != "" {
				textWithUrls += "\n\n(System Note: The user has attached the following images. If you need to use them in tools like `image_generate`, use these exact URLs:" + urlContext + "\n)"
			}

			// Add text content first
			userMessage.Contents = append(userMessage.Contents, llm.MessageContent{
				Type: "text",
				Text: textWithUrls,
			})

			// Add image content
			for _, img := range t.images {
				if img.URL != "" {
					userMessage.Contents = append(userMessage.Contents, llm.MessageContent{
						Type: "image_url",
						ImageURL: &llm.ImageURL{
							URL:    img.URL,
							Detail: "auto",
						},
					})
				} else if img.Base64 != "" {
					// Construct data URL for base64 image
					dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Base64)
					userMessage.Contents = append(userMessage.Contents, llm.MessageContent{
						Type: "image_url",
						ImageURL: &llm.ImageURL{
							URL:    dataURL,
							Detail: "auto",
						},
					})
				}
			}
		}

		t.Messages = append(t.Messages, userMessage)
		t.userMessageAdded = true
	}
}
