package protocol

type JSONSchema map[string]interface{}

type ToolParameter struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Required    bool       `json:"required"`
	Schema      JSONSchema `json:"schema"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  []ToolParameter `json:"parameters"`
}

type ToolList struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type CallToolResult struct {
	Content []interface{} `json:"content"`
}

func NewToolResultText(text string) *CallToolResult {
	return &CallToolResult{
		Content: []interface{}{
			TextContent{
				Type: ContentTypeText,
				Text: text,
			},
		},
	}
}

func NewToolResultJSON(data interface{}) (*CallToolResult, error) {
	jsonContent, err := NewJSONContent(data)
	if err != nil {
		return nil, err
	}

	return &CallToolResult{
		Content: []interface{}{jsonContent},
	}, nil
}

func StringParameter(name, description string, required bool) ToolParameter {
	return ToolParameter{
		Name:        name,
		Description: description,
		Required:    required,
		Schema: JSONSchema{
			"type": "string",
		},
	}
}

func NumberParameter(name, description string, required bool) ToolParameter {
	return ToolParameter{
		Name:        name,
		Description: description,
		Required:    required,
		Schema: JSONSchema{
			"type": "number",
		},
	}
}

func BooleanParameter(name, description string, required bool) ToolParameter {
	return ToolParameter{
		Name:        name,
		Description: description,
		Required:    required,
		Schema: JSONSchema{
			"type": "boolean",
		},
	}
}

func ObjectParameter(name, description string, required bool, properties JSONSchema, required_props []string) ToolParameter {
	return ToolParameter{
		Name:        name,
		Description: description,
		Required:    required,
		Schema: JSONSchema{
			"type":       "object",
			"properties": properties,
			"required":   required_props,
		},
	}
}
