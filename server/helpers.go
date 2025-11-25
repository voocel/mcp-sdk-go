package server

import (
	"encoding/json"
	"fmt"

	"github.com/voocel/mcp-sdk-go/protocol"
)

func TextResult(text string) *protocol.CallToolResult {
	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: protocol.ContentTypeText,
				Text: text,
			},
		},
	}
}

func JSONResult(data interface{}) (*protocol.CallToolResult, error) {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: protocol.ContentTypeText,
				Text: string(jsonBytes),
			},
		},
	}, nil
}

func ErrorResult(message string, err error) *protocol.CallToolResult {
	errorText := message
	if err != nil {
		errorText = fmt.Sprintf("%s: %v", message, err)
	}

	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.TextContent{
				Type: protocol.ContentTypeText,
				Text: errorText,
			},
		},
		IsError: true,
	}
}

func ImageResult(data string, mimeType string) *protocol.CallToolResult {
	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.ImageContent{
				Type:     protocol.ContentTypeImage,
				Data:     data,
				MimeType: mimeType,
			},
		},
	}
}

func ResourceResult(uri, mimeType, text string) *protocol.CallToolResult {
	return &protocol.CallToolResult{
		Content: []protocol.Content{
			protocol.EmbeddedResourceContent{
				Type: protocol.ContentTypeResource,
				Resource: protocol.ResourceContents{
					URI:      uri,
					MimeType: mimeType,
					Text:     text,
				},
			},
		},
	}
}

func GetString(req *CallToolRequest, key string, defaultValue string) string {
	if req.Params.Arguments == nil {
		return defaultValue
	}

	val, ok := req.Params.Arguments[key]
	if !ok {
		return defaultValue
	}

	strVal, ok := val.(string)
	if !ok {
		return defaultValue
	}

	return strVal
}

func GetInt(req *CallToolRequest, key string, defaultValue int) int {
	if req.Params.Arguments == nil {
		return defaultValue
	}

	val, ok := req.Params.Arguments[key]
	if !ok {
		return defaultValue
	}

	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return defaultValue
	}
}

func GetInt64(req *CallToolRequest, key string, defaultValue int64) int64 {
	if req.Params.Arguments == nil {
		return defaultValue
	}

	val, ok := req.Params.Arguments[key]
	if !ok {
		return defaultValue
	}

	switch v := val.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	default:
		return defaultValue
	}
}

func GetFloat(req *CallToolRequest, key string, defaultValue float64) float64 {
	if req.Params.Arguments == nil {
		return defaultValue
	}

	val, ok := req.Params.Arguments[key]
	if !ok {
		return defaultValue
	}

	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return defaultValue
	}
}

func GetBool(req *CallToolRequest, key string, defaultValue bool) bool {
	if req.Params.Arguments == nil {
		return defaultValue
	}

	val, ok := req.Params.Arguments[key]
	if !ok {
		return defaultValue
	}

	boolVal, ok := val.(bool)
	if !ok {
		return defaultValue
	}

	return boolVal
}

func GetStringSlice(req *CallToolRequest, key string, defaultValue []string) []string {
	if req.Params.Arguments == nil {
		return defaultValue
	}

	val, ok := req.Params.Arguments[key]
	if !ok {
		return defaultValue
	}

	slice, ok := val.([]interface{})
	if !ok {
		return defaultValue
	}

	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if str, ok := item.(string); ok {
			result = append(result, str)
		}
	}

	if len(result) == 0 {
		return defaultValue
	}

	return result
}

func GetMap(req *CallToolRequest, key string, defaultValue map[string]interface{}) map[string]interface{} {
	if req.Params.Arguments == nil {
		return defaultValue
	}

	val, ok := req.Params.Arguments[key]
	if !ok {
		return defaultValue
	}

	mapVal, ok := val.(map[string]interface{})
	if !ok {
		return defaultValue
	}

	return mapVal
}

func MustGetString(req *CallToolRequest, key string) (string, error) {
	if req.Params.Arguments == nil {
		return "", fmt.Errorf("missing parameter: %s", key)
	}

	val, ok := req.Params.Arguments[key]
	if !ok {
		return "", fmt.Errorf("missing parameter: %s", key)
	}

	strVal, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("parameter %s must be a string", key)
	}

	return strVal, nil
}

func MustGetInt(req *CallToolRequest, key string) (int, error) {
	if req.Params.Arguments == nil {
		return 0, fmt.Errorf("missing parameter: %s", key)
	}

	val, ok := req.Params.Arguments[key]
	if !ok {
		return 0, fmt.Errorf("missing parameter: %s", key)
	}

	switch v := val.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case float32:
		return int(v), nil
	default:
		return 0, fmt.Errorf("parameter %s must be an integer", key)
	}
}

func MustGetBool(req *CallToolRequest, key string) (bool, error) {
	if req.Params.Arguments == nil {
		return false, fmt.Errorf("missing parameter: %s", key)
	}

	val, ok := req.Params.Arguments[key]
	if !ok {
		return false, fmt.Errorf("missing parameter: %s", key)
	}

	boolVal, ok := val.(bool)
	if !ok {
		return false, fmt.Errorf("parameter %s must be a boolean", key)
	}

	return boolVal, nil
}
