package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

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
	if t == nil {
		return nil, fmt.Errorf("input must be a struct or pointer to struct")
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input must be a struct or pointer to struct")
	}

	schema, err := InferSchemaFromType(t, nil)
	if err != nil {
		return nil, err
	}
	return SchemaToJSONMap(schema)
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

	// Notification message: has method but no id (this is valid)
	if msg.Method != "" && msg.ID == nil {
		// This is a notification message, no further validation needed
		return nil
	}

	// Request message: has method and id
	if msg.Method != "" && msg.ID != nil {
		// This is a request message, valid
		return nil
	}

	// Response message: no method, but has id and result or error
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
