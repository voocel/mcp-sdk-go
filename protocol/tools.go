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
	Content           []Content   `json:"content"`
	IsError           bool        `json:"isError,omitempty"`
	StructuredContent interface{} `json:"structuredContent,omitempty"` // MCP 2025-06-18 新增
}

// UnmarshalJSON 实现自定义JSON反序列化
func (ctr *CallToolResult) UnmarshalJSON(data []byte) error {
	var temp struct {
		Content           []json.RawMessage `json:"content"`
		IsError           bool              `json:"isError,omitempty"`
		StructuredContent interface{}       `json:"structuredContent,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	ctr.IsError = temp.IsError
	ctr.StructuredContent = temp.StructuredContent
	ctr.Content = make([]Content, len(temp.Content))

	for i, raw := range temp.Content {
		content, err := UnmarshalContent(raw)
		if err != nil {
			return err
		}
		ctr.Content[i] = content
	}

	return nil
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

// NewToolResultWithStructured 创建带有结构化内容的工具结果 (MCP 2025-06-18)
func NewToolResultWithStructured(content []Content, structuredContent interface{}) *CallToolResult {
	return &CallToolResult{
		Content:           content,
		StructuredContent: structuredContent,
		IsError:           false,
	}
}

// NewToolResultTextWithStructured 创建带有文本和结构化内容的工具结果
func NewToolResultTextWithStructured(text string, structuredContent interface{}) *CallToolResult {
	return &CallToolResult{
		Content:           []Content{NewTextContent(text)},
		StructuredContent: structuredContent,
		IsError:           false,
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
