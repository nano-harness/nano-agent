package llm

import (
	"strings"
	"sync"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// apiNameToOriginal maps sanitized API-safe tool names back to their original
// canonical names. Some LLM providers (e.g. DeepSeek) reject function names
// that contain dots or other characters outside ^[a-zA-Z0-9_-]+$.
var (
	apiNameToOriginal = map[string]string{}
	apiNameMu         sync.RWMutex
)

// sanitizeToolName replaces characters that are rejected by strict OpenAI-
// compatible providers (DeepSeek, etc.) with underscores.
func sanitizeToolName(name string) string {
	return strings.NewReplacer(
		".", "_",
		" ", "_",
	).Replace(name)
}

// ResolveToolName maps an API-facing tool name back to the original canonical
// name used by the tool registry. If no mapping exists the input is returned
// unchanged.
func ResolveToolName(apiName string) string {
	apiNameMu.RLock()
	defer apiNameMu.RUnlock()
	if orig, ok := apiNameToOriginal[apiName]; ok {
		return orig
	}
	return apiName
}

type ToolSchemaConverter struct{}

func NewToolSchemaConverter() ToolSchemaConverter {
	return ToolSchemaConverter{}
}

func (ToolSchemaConverter) ConvertTools(tools []interfaces.Tool) []openai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	out := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		schema := tool.Schema()
		if schema == nil {
			continue
		}

		parameters := openai.FunctionParameters{
			"type":       "object",
			"properties": make(map[string]interface{}),
		}
		if len(schema.Required) > 0 {
			parameters["required"] = schema.Required
		}
		if schema.Properties != nil {
			for name, prop := range schema.Properties {
				parameters["properties"].(map[string]interface{})[name] = convertPropertySchema(prop)
			}
		}

		originalName := tool.Name()
		apiName := sanitizeToolName(originalName)
		if apiName != originalName {
			apiNameMu.Lock()
			apiNameToOriginal[apiName] = originalName
			apiNameMu.Unlock()
		}

		out = append(out, openai.ChatCompletionFunctionTool(
			shared.FunctionDefinitionParam{
				Name:        apiName,
				Description: openai.String(tool.Description()),
				Parameters:  parameters,
			},
		))
	}
	return out
}

func convertPropertySchema(prop *interfaces.PropertySchema) map[string]interface{} {
	if prop == nil {
		return map[string]interface{}{"type": "string"}
	}
	propType := prop.Type
	if propType == "" {
		propType = "string"
	}
	propDef := map[string]interface{}{"type": propType}
	if prop.Description != "" {
		propDef["description"] = prop.Description
	}
	if prop.Enum != nil {
		enumValues := make([]string, 0, len(prop.Enum))
		for _, value := range prop.Enum {
			if value != "" {
				enumValues = append(enumValues, value)
			}
		}
		if len(enumValues) > 0 {
			propDef["enum"] = enumValues
		}
	}
	if prop.Default != nil {
		propDef["default"] = prop.Default
	}
	if prop.Pattern != "" {
		propDef["pattern"] = prop.Pattern
	}
	if prop.MinLength != nil {
		propDef["minLength"] = *prop.MinLength
	}
	if prop.MaxLength != nil {
		propDef["maxLength"] = *prop.MaxLength
	}
	if prop.Minimum != nil {
		propDef["minimum"] = *prop.Minimum
	}
	if prop.Maximum != nil {
		propDef["maximum"] = *prop.Maximum
	}
	if prop.Examples != nil {
		propDef["examples"] = prop.Examples
	}
	if strings.EqualFold(propType, "array") {
		if prop.Items != nil {
			propDef["items"] = convertPropertySchema(prop.Items)
		} else {
			propDef["items"] = map[string]interface{}{"type": "string"}
		}
	}
	return propDef
}
