package mcp //nolint:revive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPClientProvider provides access to an MCPClient
type MCPClientProvider interface { //nolint:revive
	MCP() *mcp.MCPClient
}

// MCPTool implements the Tool interface for calling remote MCP tools
type MCPTool struct { //nolint:revive
	name        string
	description string
	serverName  string
	toolName    string
	provider    MCPClientProvider
	schema      *interfaces.ToolSchema
}

// NewMCPTool creates a new MCP tool wrapper with real schema
func NewMCPTool(serverName, toolName, toolDescription string, inputSchema map[string]interface{}, provider MCPClientProvider) *MCPTool {
	name := fmt.Sprintf("mcp_%s_%s", serverName, toolName)
	description := toolDescription
	if description == "" {
		description = fmt.Sprintf("Call %s tool from MCP server %s", toolName, serverName)
	}

	schema := convertMCPSchemaToToolSchema(toolName, inputSchema)
	if schema == nil {
		// Fallback to empty schema if conversion fails
		schema = &interfaces.ToolSchema{
			Type:        "object",
			Description: fmt.Sprintf("Parameters for MCP tool %s - accepts any parameters as the tool schema is dynamic", toolName),
			Properties:  map[string]*interfaces.PropertySchema{},
			Required:    []string{},
		}
	}

	return &MCPTool{
		name:        name,
		description: description,
		serverName:  serverName,
		toolName:    toolName,
		provider:    provider,
		schema:      schema,
	}
}

// Name returns the tool name
func (t *MCPTool) Name() string {
	return t.name
}

// Description returns the tool description
func (t *MCPTool) Description() string {
	return t.description
}

// Schema returns the tool schema
func (t *MCPTool) Schema() *interfaces.ToolSchema {
	return t.schema
}

// Execute executes the remote MCP tool
func (t *MCPTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	client := t.provider.MCP()
	if client == nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "MCP client not available",
		}, fmt.Errorf("MCP client not available")
	}

	var arguments map[string]interface{}

	// Case 1: Arguments are in a nested "arguments" map
	if args, ok := params["arguments"]; ok {
		if argMap, ok := args.(map[string]interface{}); ok {
			arguments = argMap
		} else if argsStr, ok := args.(string); ok {
			// Try to parse JSON string
			var parsedArgs map[string]interface{}
			if json.Unmarshal([]byte(argsStr), &parsedArgs) == nil {
				arguments = parsedArgs
			}
		}
	}

	// Case 2: Arguments are at the top level of params.
	// This is the default if "arguments" key is not present or is invalid.
	if arguments == nil {
		arguments = make(map[string]interface{})
		for k, v := range params {
			// Skip "arguments" key if it exists but was not a valid map/string
			if k != "arguments" {
				arguments[k] = v
			}
		}
	}

	logger.Infof("Calling MCP tool %s on server %s with arguments: %+v", t.toolName, t.serverName, arguments)

	// Call the remote tool using typed result
	result, err := client.CallTool(ctx, t.serverName, t.toolName, arguments)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	// Extract human-readable text from MCP content
	text := formatMCPContents(result.Content)
	if text == "" {
		text = fmt.Sprintf("Tool %s executed with no textual content", t.toolName)
	}

	// Handle error flag
	if result.IsError {
		return &interfaces.ToolResult{
			Success:     false,
			Data:        result,
			Error:       text,
			LLMContent:  text,
			UserContent: text,
			Metadata: map[string]interface{}{
				"server":   t.serverName,
				"tool":     t.toolName,
				"mcp_type": "tool_call",
			},
		}, nil
	}

	contentText := text

	return &interfaces.ToolResult{
		Success:     true,
		Data:        result,
		LLMContent:  contentText,
		UserContent: contentText,
		Metadata: map[string]interface{}{
			"server":   t.serverName,
			"tool":     t.toolName,
			"mcp_type": "tool_call",
		},
	}, nil
}

// RequiresConfirmation returns true: MCP tools may perform arbitrary remote
// operations (database writes, API calls, file modifications) and must always
// require user confirmation unless explicitly allowed via the session allowlist
// or a permission mode that waives confirmation.
func (t *MCPTool) RequiresConfirmation() bool {
	return true
}

// ConcurrencySafe returns false: MCP tools may have arbitrary remote side effects.
func (t *MCPTool) ConcurrencySafe() bool { return false }

// Category returns the tool category
func (t *MCPTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryMCP
}

// convertMCPSchemaToToolSchema converts MCP InputSchema to nano agent ToolSchema
func convertMCPSchemaToToolSchema(toolName string, inputSchema map[string]interface{}) *interfaces.ToolSchema {
	if inputSchema == nil {
		return &interfaces.ToolSchema{
			Type:        "object",
			Description: fmt.Sprintf("Parameters for MCP tool %s - accepts any parameters as the tool schema is dynamic", toolName),
			Properties:  map[string]*interfaces.PropertySchema{},
			Required:    []string{},
		}
	}

	// Extract basic schema information
	schemaType := "object"
	if t, ok := inputSchema["type"].(string); ok {
		schemaType = t
	}

	description := fmt.Sprintf("Parameters for MCP tool %s", toolName)
	if desc, ok := inputSchema["description"].(string); ok && desc != "" {
		description = desc
	}

	var convertPropertySchema func(propDefMap map[string]interface{}) *interfaces.PropertySchema
	convertPropertySchema = func(propDefMap map[string]interface{}) *interfaces.PropertySchema {
		propSchema := &interfaces.PropertySchema{}

		if propType, ok := propDefMap["type"].(string); ok {
			propSchema.Type = propType
		} else {
			propSchema.Type = "string"
		}

		if propDesc, ok := propDefMap["description"].(string); ok {
			propSchema.Description = propDesc
		}

		if enumVal, ok := propDefMap["enum"]; ok {
			if enumSlice, ok := enumVal.([]interface{}); ok {
				var enumStrings []string
				for _, e := range enumSlice {
					if enumStr, ok := e.(string); ok {
						enumStrings = append(enumStrings, enumStr)
					}
				}
				propSchema.Enum = enumStrings
			}
		}

		if itemsVal, ok := propDefMap["items"]; ok {
			if itemsMap, ok := itemsVal.(map[string]interface{}); ok {
				propSchema.Items = convertPropertySchema(itemsMap)
			} else if itemType, ok := itemsVal.(string); ok && itemType != "" {
				propSchema.Items = &interfaces.PropertySchema{Type: itemType}
			}
		}

		return propSchema
	}

	// Convert properties
	properties := make(map[string]*interfaces.PropertySchema)
	if props, ok := inputSchema["properties"].(map[string]interface{}); ok {
		for propName, propDef := range props {
			if propDefMap, ok := propDef.(map[string]interface{}); ok {
				properties[propName] = convertPropertySchema(propDefMap)
			}
		}
	}

	// Extract required fields
	var required []string
	if reqFields, ok := inputSchema["required"].([]interface{}); ok {
		for _, field := range reqFields {
			if fieldName, ok := field.(string); ok {
				required = append(required, fieldName)
			}
		}
	}

	return &interfaces.ToolSchema{
		Type:        schemaType,
		Description: description,
		Properties:  properties,
		Required:    required,
	}
}

// MCPResourceTool implements the Tool interface for reading remote MCP resources
type MCPResourceTool struct { //nolint:revive
	name        string
	description string
	serverName  string
	uri         string
	provider    MCPClientProvider
	schema      *interfaces.ToolSchema
}

// NewMCPResourceTool creates a new MCP resource tool wrapper
func NewMCPResourceTool(serverName, uri, resourceName string, provider MCPClientProvider) *MCPResourceTool {
	name := fmt.Sprintf("mcp_resource_%s_%s", serverName, sanitizeName(resourceName))
	description := fmt.Sprintf("Read %s resource from MCP server %s", resourceName, serverName)

	return &MCPResourceTool{
		name:        name,
		description: description,
		serverName:  serverName,
		uri:         uri,
		provider:    provider,
		schema:      createMCPResourceSchema(resourceName),
	}
}

// Name returns the tool name
func (t *MCPResourceTool) Name() string {
	return t.name
}

// Description returns the tool description
func (t *MCPResourceTool) Description() string {
	return t.description
}

// Schema returns the tool schema
func (t *MCPResourceTool) Schema() *interfaces.ToolSchema {
	return t.schema
}

// Execute reads the remote MCP resource
func (t *MCPResourceTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) { //nolint:revive
	client := t.provider.MCP()
	if client == nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "MCP client not available",
		}, fmt.Errorf("MCP client not available")
	}

	logger.Infof("Reading MCP resource %s from server %s", t.uri, t.serverName)

	// Read the remote resource (typed)
	result, err := client.ReadResource(ctx, t.serverName, t.uri)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	// We don't rely on specific fields of ReadResourceResult here; present JSON for readability
	var text string
	if b, err := json.MarshalIndent(result, "", "  "); err == nil {
		text = string(b)
	} else {
		text = fmt.Sprintf("Resource %s read successfully", t.uri)
	}

	contentText := text

	return &interfaces.ToolResult{
		Success:     true,
		Data:        result,
		LLMContent:  contentText,
		UserContent: contentText,
		Metadata: map[string]interface{}{
			"server":   t.serverName,
			"uri":      t.uri,
			"mcp_type": "resource_read",
		},
	}, nil
}

// RequiresConfirmation returns whether the tool requires confirmation
func (t *MCPResourceTool) RequiresConfirmation() bool {
	return false
}

// ConcurrencySafe returns true: reading a resource is a read-only remote operation.
func (t *MCPResourceTool) ConcurrencySafe() bool { return true }

// Category returns the tool category
func (t *MCPResourceTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryMCP
}

// createMCPResourceSchema creates a schema for MCP resource parameters
func createMCPResourceSchema(resourceName string) *interfaces.ToolSchema {
	return &interfaces.ToolSchema{
		Type:        "object",
		Description: fmt.Sprintf("Parameters for reading MCP resource %s", resourceName),
		Properties:  map[string]*interfaces.PropertySchema{},
		Required:    []string{},
	}
}

// MCPPromptTool implements the Tool interface for getting remote MCP prompts
type MCPPromptTool struct { //nolint:revive
	name        string
	description string
	serverName  string
	promptName  string
	provider    MCPClientProvider
	schema      *interfaces.ToolSchema
}

// NewMCPPromptTool creates a new MCP prompt tool wrapper
func NewMCPPromptTool(serverName, promptName string, provider MCPClientProvider) *MCPPromptTool {
	name := fmt.Sprintf("mcp_prompt_%s_%s", serverName, promptName)
	description := fmt.Sprintf("Get %s prompt from MCP server %s", promptName, serverName)

	return &MCPPromptTool{
		name:        name,
		description: description,
		serverName:  serverName,
		promptName:  promptName,
		provider:    provider,
		schema:      createMCPPromptSchema(promptName),
	}
}

// Name returns the tool name
func (t *MCPPromptTool) Name() string {
	return t.name
}

// Description returns the tool description
func (t *MCPPromptTool) Description() string {
	return t.description
}

// Schema returns the tool schema
func (t *MCPPromptTool) Schema() *interfaces.ToolSchema {
	return t.schema
}

// Execute gets the remote MCP prompt
func (t *MCPPromptTool) Execute(ctx context.Context, params map[string]interface{}) (*interfaces.ToolResult, error) {
	client := t.provider.MCP()
	if client == nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   "MCP client not available",
		}, fmt.Errorf("MCP client not available")
	}

	// Extract arguments from params
	arguments := make(map[string]string)

	if args, ok := params["arguments"]; ok {
		if argMap, ok := args.(map[string]interface{}); ok {
			for k, v := range argMap {
				arguments[k] = fmt.Sprintf("%v", v)
			}
		} else if argsStr, ok := args.(string); ok {
			var argMap map[string]interface{}
			if json.Unmarshal([]byte(argsStr), &argMap) == nil {
				for k, v := range argMap {
					arguments[k] = fmt.Sprintf("%v", v)
				}
			}
		}
	}

	logger.Infof("Getting MCP prompt %s from server %s with arguments: %+v", t.promptName, t.serverName, arguments)

	// Get the remote prompt (typed)
	result, err := client.GetPrompt(ctx, t.serverName, t.promptName, arguments)
	if err != nil {
		return &interfaces.ToolResult{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	// Format prompt messages
	var sb strings.Builder
	for _, msg := range result.Messages {
		fmt.Fprintf(&sb, "%v:\n", msg.Role)
		switch c := any(msg.Content).(type) {
		case []sdkmcp.Content:
			sb.WriteString(formatMCPContents(c))
		case sdkmcp.Content:
			sb.WriteString(formatMCPContent(c))
		default:
			if b, err := json.Marshal(c); err == nil {
				sb.WriteString(string(b))
			} else {
				fmt.Fprintf(&sb, "%v", c)
			}
		}
		sb.WriteString("\n\n")
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		// Fallback to JSON if no textual content
		if b, err := json.MarshalIndent(result, "", "  "); err == nil {
			text = string(b)
		} else {
			text = fmt.Sprintf("Prompt %s retrieved (no textual content)", t.promptName)
		}
	}

	return &interfaces.ToolResult{
		Success:     true,
		Data:        result,
		LLMContent:  text,
		UserContent: text,
		Metadata: map[string]interface{}{
			"server":   t.serverName,
			"prompt":   t.promptName,
			"mcp_type": "prompt_get",
		},
	}, nil
}

// RequiresConfirmation returns whether the tool requires confirmation
func (t *MCPPromptTool) RequiresConfirmation() bool {
	return false
}

// ConcurrencySafe returns true: retrieving a prompt is a read-only operation.
func (t *MCPPromptTool) ConcurrencySafe() bool { return true }

// Category returns the tool category
func (t *MCPPromptTool) Category() interfaces.ToolCategory {
	return interfaces.CategoryMCP
}

// createMCPPromptSchema creates a schema for MCP prompt parameters
func createMCPPromptSchema(promptName string) *interfaces.ToolSchema {
	return &interfaces.ToolSchema{
		Type:        "object",
		Description: fmt.Sprintf("Parameters for MCP prompt %s", promptName),
		Properties: map[string]*interfaces.PropertySchema{
			"arguments": {
				Type:        "object",
				Description: "Arguments to pass to the MCP prompt",
			},
		},
		Required: []string{},
	}
}

// sanitizeName sanitizes a name for use in tool names
func sanitizeName(name string) string {
	// Replace non-alphanumeric characters with underscores
	result := strings.Builder{}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	return strings.ToLower(result.String())
}

// formatMCPContents converts an array of MCP contents into a readable string
func formatMCPContents(contents []sdkmcp.Content) string {
	var sb strings.Builder
	for _, c := range contents {
		sb.WriteString(formatMCPContent(c))
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// formatMCPContent converts a single MCP content into a readable string
func formatMCPContent(content sdkmcp.Content) string {
	switch v := content.(type) {
	case *sdkmcp.TextContent:
		return v.Text
	case *sdkmcp.ImageContent:
		return fmt.Sprintf("[image %s, %d bytes]", v.MIMEType, len(v.Data))
	default:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}
