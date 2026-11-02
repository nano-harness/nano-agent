package sendmessage

import (
	"context"
	"testing"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTool_Basic tests basic tool properties without mailbox dependency.
func TestTool_Basic(t *testing.T) {
	tool := New(nil)

	t.Run("Name", func(t *testing.T) {
		assert.Equal(t, "send_message", tool.Name())
	})

	t.Run("Description", func(t *testing.T) {
		desc := tool.Description()
		assert.NotEmpty(t, desc)
		assert.Contains(t, desc, "message")
	})

	t.Run("Category", func(t *testing.T) {
		assert.Equal(t, interfaces.CategoryAgent, tool.Category())
	})

	t.Run("RequiresConfirmation", func(t *testing.T) {
		assert.False(t, tool.RequiresConfirmation())
	})

	t.Run("ConcurrencySafe", func(t *testing.T) {
		assert.True(t, tool.ConcurrencySafe())
	})
}

// TestTool_Schema tests the tool schema.
func TestTool_Schema(t *testing.T) {
	tool := New(nil)
	schema := tool.Schema()

	require.NotNil(t, schema)
	require.NotNil(t, schema.Properties)

	t.Run("Required fields", func(t *testing.T) {
		assert.Contains(t, schema.Required, "to")
		assert.Contains(t, schema.Required, "text")
	})

	t.Run("Has 'to' property", func(t *testing.T) {
		prop, ok := schema.Properties["to"]
		require.True(t, ok)
		assert.Equal(t, "string", prop.Type)
		assert.NotEmpty(t, prop.Description)
	})

	t.Run("Has 'text' property", func(t *testing.T) {
		prop, ok := schema.Properties["text"]
		require.True(t, ok)
		assert.Equal(t, "string", prop.Type)
		assert.NotEmpty(t, prop.Description)
	})

	t.Run("Has 'topic' property", func(t *testing.T) {
		prop, ok := schema.Properties["topic"]
		require.True(t, ok)
		assert.Equal(t, "string", prop.Type)
	})

	t.Run("Has 'body' property", func(t *testing.T) {
		prop, ok := schema.Properties["body"]
		require.True(t, ok)
		assert.Equal(t, "object", prop.Type)
	})
}

// TestTool_Execute_ValidationErrors tests parameter validation.
func TestTool_Execute_ValidationErrors(t *testing.T) {
	tool := New(nil)
	ctx := context.Background()

	t.Run("Missing 'to' field", func(t *testing.T) {
		params := map[string]interface{}{
			"text": "Hello",
		}
		result, err := tool.Execute(ctx, params)
		require.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.LLMContent, "'to' field is required")
	})

	t.Run("Empty 'to' field", func(t *testing.T) {
		params := map[string]interface{}{
			"to":   "",
			"text": "Hello",
		}
		result, err := tool.Execute(ctx, params)
		require.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.LLMContent, "'to' field is required")
	})

	t.Run("Missing 'text' field", func(t *testing.T) {
		params := map[string]interface{}{
			"to": "teammate",
		}
		result, err := tool.Execute(ctx, params)
		require.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.LLMContent, "'text' field is required")
	})

	t.Run("Empty 'text' field", func(t *testing.T) {
		params := map[string]interface{}{
			"to":   "teammate",
			"text": "",
		}
		result, err := tool.Execute(ctx, params)
		require.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.LLMContent, "'text' field is required")
	})

	t.Run("Missing team context", func(t *testing.T) {
		params := map[string]interface{}{
			"to":   "teammate",
			"text": "Hello world",
		}
		result, err := tool.Execute(ctx, params)
		require.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.LLMContent, "team name")
	})
}

// TestNew verifies tool construction.
func TestNew(t *testing.T) {
	t.Run("With nil mailbox", func(t *testing.T) {
		tool := New(nil)
		assert.NotNil(t, tool)
		assert.Nil(t, tool.mailboxBackend)
	})
}
