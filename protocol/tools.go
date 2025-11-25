package protocol

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type ToolParameter struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Required    bool       `json:"required,omitempty"`
	Schema      JSONSchema `json:"schema,omitempty"`
}

type Tool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"` // MCP 2025-06-18: Human-friendly title
	Description  string         `json:"description,omitempty"`
	InputSchema  JSONSchema     `json:"inputSchema"`
	OutputSchema JSONSchema     `json:"outputSchema,omitempty"` // MCP 2025-06-18
	Meta         map[string]any `json:"_meta,omitempty"`        // MCP 2025-06-18: Extended metadata
}

type ToolList struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Meta      map[string]any `json:"_meta,omitempty"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ListToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type CallToolResult struct {
	Content           []Content      `json:"content"`
	IsError           bool           `json:"isError,omitempty"`
	StructuredContent any            `json:"structuredContent,omitempty"` // MCP 2025-06-18
	Meta              map[string]any `json:"_meta,omitempty"`             // MCP 2025-06-18: Extended metadata
}

func (ctr *CallToolResult) UnmarshalJSON(data []byte) error {
	var temp struct {
		Content           []json.RawMessage `json:"content"`
		IsError           bool              `json:"isError,omitempty"`
		StructuredContent any               `json:"structuredContent,omitempty"`
		Meta              map[string]any    `json:"_meta,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	ctr.IsError = temp.IsError
	ctr.StructuredContent = temp.StructuredContent
	ctr.Meta = temp.Meta
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
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type ToolsListChangedNotification struct{}

func NewTool(name, description string, inputSchema JSONSchema) Tool {
	return Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
	}
}

// NewToolWithOutput creates a tool with output schema (MCP 2025-06-18)
func NewToolWithOutput(name, description string, inputSchema, outputSchema JSONSchema) Tool {
	return Tool{
		Name:         name,
		Description:  description,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
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

// NewToolResultWithStructured creates a tool result with structured content (MCP 2025-06-18)
func NewToolResultWithStructured(content []Content, structuredContent interface{}) *CallToolResult {
	return &CallToolResult{
		Content:           content,
		StructuredContent: structuredContent,
		IsError:           false,
	}
}

// NewToolResultTextWithStructured creates a tool result with text and structured content
func NewToolResultTextWithStructured(text string, structuredContent interface{}) *CallToolResult {
	return &CallToolResult{
		Content:           []Content{NewTextContent(text)},
		StructuredContent: structuredContent,
		IsError:           false,
	}
}

// Cache compiled schemas to improve performance
var (
	schemaCache = make(map[string]*jsonschema.Schema)
	cacheMutex  sync.RWMutex
)

// ValidateStructuredOutput validates whether structured output conforms to the schema
func ValidateStructuredOutput(data interface{}, schema JSONSchema) error {
	if len(schema) == 0 {
		return nil
	}

	return validateWithJSONSchema(data, schema)
}

// validateWithJSONSchema validates using jsonschema library
func validateWithJSONSchema(data interface{}, schema JSONSchema) error {
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %v", err)
	}
	schemaKey := string(schemaBytes)

	// Check cache
	cacheMutex.RLock()
	compiledSchema, exists := schemaCache[schemaKey]
	cacheMutex.RUnlock()

	if !exists {
		compiler := jsonschema.NewCompiler()

		var schemaInterface interface{}
		if err := json.Unmarshal(schemaBytes, &schemaInterface); err != nil {
			return fmt.Errorf("failed to convert schema: %v", err)
		}

		if err := compiler.AddResource("schema.json", schemaInterface); err != nil {
			return fmt.Errorf("failed to add schema resource: %v", err)
		}

		compiledSchema, err = compiler.Compile("schema.json")
		if err != nil {
			return fmt.Errorf("failed to compile schema: %v", err)
		}

		// Cache compiled schema
		cacheMutex.Lock()
		schemaCache[schemaKey] = compiledSchema
		cacheMutex.Unlock()
	}

	if err := compiledSchema.Validate(data); err != nil {
		return fmt.Errorf("validation failed: %v", err)
	}

	return nil
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
