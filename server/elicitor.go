package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/utils"
)

// Elicitor interface for handling elicitation and sampling requests
type Elicitor interface {
	// Elicit sends an elicitation request and waits for response
	Elicit(ctx context.Context, method string, params interface{}) (*protocol.JSONRPCMessage, error)
	// CreateMessage sends a sampling request and waits for response
	CreateMessage(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error)
}

// MockElicitor mock implementation of elicitation and sampling
type MockElicitor struct {
	handler         func(ctx context.Context, method string, params interface{}) (*protocol.ElicitationResult, error)
	samplingHandler func(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error)
}

// NewMockElicitor creates a mock elicitation handler
func NewMockElicitor(handler func(ctx context.Context, method string, params interface{}) (*protocol.ElicitationResult, error)) *MockElicitor {
	return &MockElicitor{
		handler: handler,
		samplingHandler: func(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
			return protocol.NewCreateMessageResult(
				protocol.RoleAssistant,
				protocol.NewTextContent("this mock sampling response。"),
				"mock-model",
				protocol.StopReasonEndTurn,
			), nil
		},
	}
}

// NewMockElicitorWithSampling creates a mock elicitation and sampling handler
func NewMockElicitorWithSampling(
	elicitationHandler func(ctx context.Context, method string, params interface{}) (*protocol.ElicitationResult, error),
	samplingHandler func(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error),
) *MockElicitor {
	return &MockElicitor{
		handler:         elicitationHandler,
		samplingHandler: samplingHandler,
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

// CreateMessage handles sampling requests (returns mock response)
func (m *MockElicitor) CreateMessage(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
	if m.samplingHandler == nil {
		return nil, fmt.Errorf("sampling handler not configured")
	}

	return m.samplingHandler(ctx, request)
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

	request, err := utils.NewJSONRPCRequest(method, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// send through transport layer
	return t.sendMessage(ctx, request)
}

// CreateMessage sends sampling requests through transport layer
func (t *TransportElicitor) CreateMessage(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
	if t.sendMessage == nil {
		return nil, fmt.Errorf("no message sender configured")
	}

	// create JSON-RPC request for sampling/createMessage
	jsonRPCRequest, err := utils.NewJSONRPCRequest("sampling/createMessage", request)
	if err != nil {
		return nil, fmt.Errorf("failed to create sampling request: %w", err)
	}

	// send through transport layer
	response, err := t.sendMessage(ctx, jsonRPCRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to send sampling request: %w", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("sampling request failed: %s", response.Error.Message)
	}

	var result protocol.CreateMessageResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse sampling response: %w", err)
	}

	return &result, nil
}
