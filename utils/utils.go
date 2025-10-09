package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/voocel/mcp-sdk-go/protocol"
)

func NewJSONRPCRequest(method string, params any) (*protocol.JSONRPCMessage, error) {
	id := uuid.New().String()

	var paramsBytes json.RawMessage
	if params != nil {
		bytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		paramsBytes = bytes
	}

	return &protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.StringToID(id),
		Method:  method,
		Params:  paramsBytes,
	}, nil
}

func NewJSONRPCResponse(id string, result any) (*protocol.JSONRPCMessage, error) {
	var resultBytes json.RawMessage
	if result != nil {
		bytes, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result: %w", err)
		}
		resultBytes = bytes
	}

	return &protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.StringToID(id),
		Result:  resultBytes,
	}, nil
}

func NewJSONRPCError(id string, code int, message string, data any) (*protocol.JSONRPCMessage, error) {
	return &protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      protocol.StringToID(id),
		Error: &protocol.JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}, nil
}

func NewJSONRPCNotification(method string, params any) (*protocol.JSONRPCMessage, error) {
	var paramsBytes json.RawMessage
	if params != nil {
		bytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		paramsBytes = bytes
	}

	return &protocol.JSONRPCMessage{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  method,
		Params:  paramsBytes,
	}, nil
}

func StructToJSONSchema(v any) (protocol.JSONSchema, error) {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input must be a struct or pointer to struct")
	}

	return createStructSchema(t), nil
}

func createStructSchema(t reflect.Type) protocol.JSONSchema {
	schema := protocol.JSONSchema{
		"type":       "object",
		"properties": make(map[string]interface{}),
		"required":   []string{},
	}

	properties := schema["properties"].(map[string]interface{})
	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // 跳过私有字段
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		jsonName := field.Name
		tagParts := strings.Split(jsonTag, ",")
		if tagParts[0] != "" {
			jsonName = tagParts[0]
		}

		isRequired := !contains(tagParts, "omitempty")
		if isRequired {
			required = append(required, jsonName)
		}

		// 添加字段描述 (从jsonschema tag获取)
		fieldSchema := createFieldSchema(field.Type)
		if desc := field.Tag.Get("jsonschema"); desc != "" {
			parts := strings.Split(desc, ",")
			for _, part := range parts {
				if strings.HasPrefix(part, "description=") {
					fieldSchema["description"] = strings.TrimPrefix(part, "description=")
				}
			}
		}

		properties[jsonName] = fieldSchema
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

func createFieldSchema(t reflect.Type) map[string]interface{} {
	schema := make(map[string]interface{})

	switch t.Kind() {
	case reflect.String:
		schema["type"] = "string"
	case reflect.Bool:
		schema["type"] = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema["type"] = "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema["type"] = "integer"
		schema["minimum"] = 0
	case reflect.Float32, reflect.Float64:
		schema["type"] = "number"
	case reflect.Array, reflect.Slice:
		schema["type"] = "array"
		schema["items"] = createFieldSchema(t.Elem())
	case reflect.Map:
		schema["type"] = "object"
		if t.Elem().Kind() != reflect.Interface {
			schema["additionalProperties"] = createFieldSchema(t.Elem())
		}
	case reflect.Struct:
		schema = createStructSchema(t)
	case reflect.Ptr:
		return createFieldSchema(t.Elem())
	case reflect.Interface:
		// 对于interface{}类型，允许任何类型
		schema = map[string]interface{}{}
	default:
		schema["type"] = "string"
	}

	return schema
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func JSONToStruct(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func StructToJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func IsCanceled(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}

func ValidateJSONRPCMessage(msg *protocol.JSONRPCMessage) error {
	if msg.JSONRPC != protocol.JSONRPCVersion {
		return fmt.Errorf("invalid jsonrpc version: %s", msg.JSONRPC)
	}

	// 通知消息：有method但没有id（这是合法的）
	if msg.Method != "" && msg.ID == nil {
		// 这是通知消息，不需要验证更多
		return nil
	}

	// 请求消息：有method和id
	if msg.Method != "" && msg.ID != nil {
		// 这是请求消息，合法
		return nil
	}

	// 响应消息：没有method，但有id和result或error
	if msg.Method == "" {
		if msg.ID == nil {
			return fmt.Errorf("response must have an id")
		}
		if msg.Result == nil && msg.Error == nil {
			return fmt.Errorf("response must have result or error")
		}
		if msg.Result != nil && msg.Error != nil {
			return fmt.Errorf("response cannot have both result and error")
		}
	}

	return nil
}
