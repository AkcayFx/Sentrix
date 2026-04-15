package tools

import (
	"github.com/yourorg/sentrix/internal/provider"
)

// toolSchema builds a provider.ToolDef with an explicit JSON Schema object.
// This avoids reflection and keeps schemas readable / editable.
func toolSchema(name, description string, properties map[string]interface{}, required []string) provider.ToolDef {
	if required == nil {
		required = []string{}
	}
	return provider.ToolDef{
		Name:        name,
		Description: description,
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": properties,
			"required":   required,
		},
	}
}

// prop is a shorthand for defining a single JSON Schema property.
func prop(typ, description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        typ,
		"description": description,
	}
}

// propEnum is a shorthand for a string property with an enum constraint.
func propEnum(description string, values []string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": description,
		"enum":        values,
	}
}
