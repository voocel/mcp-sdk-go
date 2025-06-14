package protocol

import "encoding/json"

type ToolParameter struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Required    bool       `json:"required,omitempty"`
	Schema      JSONSchema `json:"schema,omitempty"`
}

type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	InputSchema JSONSchema `json:"inputSchema"`
}

type ToolList struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type ListToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type ListToolsRequest struct {
	Cursor string `json:"cursor,omitempty"`
}

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
	PaginatedResult
}

type CallToolRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type ToolsListChangedNotification struct{}

func NewTool(name, description string, inputSchema JSONSchema) Tool {
	return Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
	}
}

func NewToolResult(content []Content, isError bool) *CallToolResult {
	return &CallToolResult{
		Content: content,
		IsError: isError,
	}
}

func NewToolResultText(text string) *CallToolResult {
	return &CallToolResult{
		Content: []Content{NewTextContent(text)},
		IsError: false,
	}
}

func NewToolResultError(errorMsg string) *CallToolResult {
	return &CallToolResult{
		Content: []Content{NewTextContent(errorMsg)},
		IsError: true,
	}
}

func ContentToJSON(content []Content) ([]json.RawMessage, error) {
	result := make([]json.RawMessage, len(content))
	for i, c := range content {
		bytes, err := json.Marshal(c)
		if err != nil {
			return nil, err
		}
		result[i] = bytes
	}
	return result, nil
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
