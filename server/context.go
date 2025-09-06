package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// MCPContext provides MCP-specific context functionality, including elicitation support
type MCPContext struct {
	context.Context
	server   *MCPServer
	elicitor Elicitor // interface for handling elicitation requests
}

// NewMCPContext creates a new MCP context
func NewMCPContext(ctx context.Context, server *MCPServer, elicitor Elicitor) *MCPContext {
	return &MCPContext{
		Context:  ctx,
		server:   server,
		elicitor: elicitor,
	}
}

// Server gets the associated MCP server
func (c *MCPContext) Server() *MCPServer {
	return c.server
}

// Elicit sends an elicitation request
func (c *MCPContext) Elicit(message string, schema protocol.JSONSchema) (*protocol.ElicitationResult, error) {
	if c.elicitor == nil {
		return nil, fmt.Errorf("elicitation not supported: no elicitor configured")
	}

	// create elicitation/create request parameters
	params := protocol.NewElicitationCreateParams(message, schema)

	// send elicitation/create request through elicitor
	response, err := c.elicitor.Elicit(c.Context, "elicitation/create", params)
	if err != nil {
		return nil, fmt.Errorf("failed to send elicitation request: %w", err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("elicitation request failed: %s", response.Error.Message)
	}

	var result protocol.ElicitationResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal elicitation result: %w", err)
	}

	return &result, nil
}

// CreateMessage sends a sampling request to the client
func (c *MCPContext) CreateMessage(request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
	if c.elicitor == nil {
		return nil, fmt.Errorf("sampling not supported: no elicitor configured")
	}

	// validate request
	if err := request.Validate(); err != nil {
		return nil, fmt.Errorf("invalid sampling request: %w", err)
	}

	// send sampling request through elicitor
	result, err := c.elicitor.CreateMessage(c.Context, request)
	if err != nil {
		return nil, fmt.Errorf("failed to send sampling request: %w", err)
	}

	return result, nil
}

// CreateTextMessage creates a simple text-based sampling request
func (c *MCPContext) CreateTextMessage(userMessage string, maxTokens int) (*protocol.CreateMessageResult, error) {
	messages := []protocol.SamplingMessage{
		protocol.NewSamplingMessage(protocol.RoleUser, protocol.NewTextContent(userMessage)),
	}

	request := protocol.NewCreateMessageRequest(messages, maxTokens)
	return c.CreateMessage(request)
}

// CreateTextMessageWithSystem creates a text-based sampling request with system prompt
func (c *MCPContext) CreateTextMessageWithSystem(systemPrompt, userMessage string, maxTokens int) (*protocol.CreateMessageResult, error) {
	messages := []protocol.SamplingMessage{
		protocol.NewSamplingMessage(protocol.RoleUser, protocol.NewTextContent(userMessage)),
	}

	request := protocol.NewCreateMessageRequest(messages, maxTokens).
		WithSystemPrompt(systemPrompt)
	return c.CreateMessage(request)
}

// CreateConversationMessage creates a sampling request with conversation history
func (c *MCPContext) CreateConversationMessage(messages []protocol.SamplingMessage, maxTokens int) (*protocol.CreateMessageResult, error) {
	request := protocol.NewCreateMessageRequest(messages, maxTokens)
	return c.CreateMessage(request)
}

// ElicitString requests user to input a string
func (c *MCPContext) ElicitString(message, name, description string, required bool) (string, error) {
	schema := protocol.JSONSchema{
		"type": "object",
		"properties": map[string]interface{}{
			name: map[string]interface{}{
				"type":        "string",
				"description": description,
			},
		},
	}

	if required {
		schema["required"] = []string{name}
	}

	result, err := c.Elicit(message, schema)
	if err != nil {
		return "", err
	}
	if !result.IsAccepted() {
		return "", fmt.Errorf("user %s the request", result.Action)
	}

	if content, ok := result.Content.(map[string]interface{}); ok {
		if value, exists := content[name]; exists {
			if str, ok := value.(string); ok {
				return str, nil
			}
			return fmt.Sprintf("%v", value), nil
		}
	}

	return "", fmt.Errorf("no value provided for %s", name)
}

// ElicitChoice requests user to choose from options
func (c *MCPContext) ElicitChoice(message, name, description string, options, optionNames []string, required bool) (string, error) {
	schema := protocol.JSONSchema{
		"type": "object",
		"properties": map[string]interface{}{
			name: map[string]interface{}{
				"type":        "string",
				"description": description,
				"enum":        options,
			},
		},
	}

	// if option names are provided, add them to the schema
	if len(optionNames) == len(options) {
		properties := schema["properties"].(map[string]interface{})
		nameProperty := properties[name].(map[string]interface{})
		nameProperty["enumNames"] = optionNames
	}

	if required {
		schema["required"] = []string{name}
	}

	result, err := c.Elicit(message, schema)
	if err != nil {
		return "", err
	}

	if !result.IsAccepted() {
		return "", fmt.Errorf("user %s the request", result.Action)
	}

	// extract the selected value
	if content, ok := result.Content.(map[string]interface{}); ok {
		if value, exists := content[name]; exists {
			if str, ok := value.(string); ok {
				return str, nil
			}
		}
	}

	return "", fmt.Errorf("no value provided for %s", name)
}

// ElicitNumber requests user to input a number
func (c *MCPContext) ElicitNumber(message, name, description string, min, max *float64, required bool) (float64, error) {
	properties := map[string]interface{}{
		"type":        "number",
		"description": description,
	}

	if min != nil {
		properties["minimum"] = *min
	}
	if max != nil {
		properties["maximum"] = *max
	}

	schema := protocol.JSONSchema{
		"type": "object",
		"properties": map[string]interface{}{
			name: properties,
		},
	}

	if required {
		schema["required"] = []string{name}
	}

	result, err := c.Elicit(message, schema)
	if err != nil {
		return 0, err
	}

	if !result.IsAccepted() {
		return 0, fmt.Errorf("user %s the request", result.Action)
	}

	// extract the number value
	if content, ok := result.Content.(map[string]interface{}); ok {
		if value, exists := content[name]; exists {
			switch v := value.(type) {
			case float64:
				return v, nil
			case int:
				return float64(v), nil
			case int64:
				return float64(v), nil
			default:
				return 0, fmt.Errorf("invalid number type: %T", value)
			}
		}
	}

	return 0, fmt.Errorf("no value provided for %s", name)
}

// ElicitBoolean requests user to input a boolean value
func (c *MCPContext) ElicitBoolean(message, name, description string, defaultValue *bool, required bool) (bool, error) {
	properties := map[string]interface{}{
		"type":        "boolean",
		"description": description,
	}

	if defaultValue != nil {
		properties["default"] = *defaultValue
	}

	schema := protocol.JSONSchema{
		"type": "object",
		"properties": map[string]interface{}{
			name: properties,
		},
	}

	if required {
		schema["required"] = []string{name}
	}

	result, err := c.Elicit(message, schema)
	if err != nil {
		return false, err
	}
	if !result.IsAccepted() {
		return false, fmt.Errorf("user %s the request", result.Action)
	}

	// extract the boolean value
	if content, ok := result.Content.(map[string]interface{}); ok {
		if value, exists := content[name]; exists {
			if b, ok := value.(bool); ok {
				return b, nil
			}
		}
	}

	return false, fmt.Errorf("no value provided for %s", name)
}

// ElicitMultiple requests user to input multiple fields
func (c *MCPContext) ElicitMultiple(message string, schema protocol.JSONSchema) (map[string]interface{}, error) {
	result, err := c.Elicit(message, schema)
	if err != nil {
		return nil, err
	}
	if !result.IsAccepted() {
		return nil, fmt.Errorf("user %s the request", result.Action)
	}

	if content, ok := result.Content.(map[string]interface{}); ok {
		return content, nil
	}

	return nil, fmt.Errorf("invalid content format")
}
