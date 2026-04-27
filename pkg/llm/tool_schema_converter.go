package llm

import (
	"strings"

	"github.com/nano-harness/nano-agent/pkg/interfaces"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

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

		out = append(out, openai.ChatCompletionFunctionTool(
			shared.FunctionDefinitionParam{
				Name:        tool.Name(),
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
