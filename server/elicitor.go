package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/utils"
)

// Elicitor interface for handling elicitation requests
type Elicitor interface {
	// Elicit sends an elicitation request and waits for response
	Elicit(ctx context.Context, method string, params interface{}) (*protocol.JSONRPCMessage, error)
}

// MockElicitor mock implementation of elicitation
type MockElicitor struct {
	handler func(ctx context.Context, method string, params interface{}) (*protocol.ElicitationResult, error)
}

// NewMockElicitor creates a mock elicitation handler
func NewMockElicitor(handler func(ctx context.Context, method string, params interface{}) (*protocol.ElicitationResult, error)) *MockElicitor {
	return &MockElicitor{
		handler: handler,
	}
}

// Elicit handles elicitation requests (returns mock response)
func (m *MockElicitor) Elicit(ctx context.Context, method string, params interface{}) (*protocol.JSONRPCMessage, error) {
	if m.handler == nil {
		return nil, fmt.Errorf("no handler configured")
	}

	// for elicitation/create requests, call the handler
	if method == "elicitation/create" {
		result, err := m.handler(ctx, method, params)
		if err != nil {
			return nil, err
		}

		resultBytes, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result: %w", err)
		}

		return &protocol.JSONRPCMessage{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.StringToID(uuid.New().String()),
			Result:  resultBytes,
		}, nil
	}

	return nil, fmt.Errorf("unsupported method: %s", method)
}

// TransportElicitor implementation that sends requests through transport layer
type TransportElicitor struct {
	sendMessage func(ctx context.Context, message *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error)
}

// NewTransportElicitor creates a transport layer elicitation handler
func NewTransportElicitor(sendMessage func(ctx context.Context, message *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error)) *TransportElicitor {
	return &TransportElicitor{
		sendMessage: sendMessage,
	}
}

// Elicit sends elicitation requests through transport layer
func (t *TransportElicitor) Elicit(ctx context.Context, method string, params interface{}) (*protocol.JSONRPCMessage, error) {
	if t.sendMessage == nil {
		return nil, fmt.Errorf("no message sender configured")
	}

	// create JSON-RPC request
	request, err := utils.NewJSONRPCRequest(method, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// send through transport layer
	return t.sendMessage(ctx, request)
}
