package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetWorkspaceToolNames tests tool name listing.
func TestGetWorkspaceToolNames(t *testing.T) {
	names := GetWorkspaceToolNames()

	require.NotEmpty(t, names)
	assert.Contains(t, names, "oss_manager")
	assert.Equal(t, []string{"oss_manager"}, names)
}

// TestNewOSSManagerTool tests OSS manager tool creation.
func TestNewOSSManagerTool(t *testing.T) {
	t.Run("Create with valid config", func(t *testing.T) {
		workingDir := "/tmp/test"
		config := map[string]interface{}{
			"endpoint":  "test-endpoint",
			"bucket":    "test-bucket",
			"accessKey": "test-key",
		}

		tool := NewOSSManagerTool(workingDir, config)
		assert.NotNil(t, tool)
	})

	t.Run("Create with nil config", func(t *testing.T) {
		tool := NewOSSManagerTool("/tmp", nil)
		assert.NotNil(t, tool)
	})

	t.Run("Create with empty config", func(t *testing.T) {
		tool := NewOSSManagerTool("/tmp", map[string]interface{}{})
		assert.NotNil(t, tool)
	})
}

// TestOSSManagerTool_BasicProperties tests basic tool properties.
func TestOSSManagerTool_BasicProperties(t *testing.T) {
	tool := NewOSSManagerTool("/tmp", nil)

	t.Run("Name", func(t *testing.T) {
		name := tool.Name()
		assert.Equal(t, "oss_manager", name)
	})

	t.Run("Description", func(t *testing.T) {
		desc := tool.Description()
		assert.NotEmpty(t, desc)
	})

	t.Run("Category", func(t *testing.T) {
		cat := tool.Category()
		// Workspace tools are typically in Workspace or System category
		assert.NotEmpty(t, cat)
	})
}
