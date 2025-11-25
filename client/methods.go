package client

import (
	"context"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// Ping sends a ping request to the server
func (cs *ClientSession) Ping(ctx context.Context, params *protocol.PingParams) error {
	if params == nil {
		params = &protocol.PingParams{}
	}
	var result protocol.EmptyResult
	return cs.sendRequest(ctx, protocol.MethodPing, params, &result)
}

// ListTools lists the currently available tools on the server
func (cs *ClientSession) ListTools(ctx context.Context, params *protocol.ListToolsParams) (*protocol.ListToolsResult, error) {
	if params == nil {
		params = &protocol.ListToolsParams{}
	}
	var result protocol.ListToolsResult
	if err := cs.sendRequest(ctx, protocol.MethodToolsList, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CallTool invokes a tool on the server
func (cs *ClientSession) CallTool(ctx context.Context, params *protocol.CallToolParams) (*protocol.CallToolResult, error) {
	var result protocol.CallToolResult
	if err := cs.sendRequest(ctx, protocol.MethodToolsCall, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListResources lists the currently available resources on the server
func (cs *ClientSession) ListResources(ctx context.Context, params *protocol.ListResourcesParams) (*protocol.ListResourcesResult, error) {
	if params == nil {
		params = &protocol.ListResourcesParams{}
	}
	var result protocol.ListResourcesResult
	if err := cs.sendRequest(ctx, protocol.MethodResourcesList, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ReadResource reads a resource from the server
func (cs *ClientSession) ReadResource(ctx context.Context, params *protocol.ReadResourceParams) (*protocol.ReadResourceResult, error) {
	var result protocol.ReadResourceResult
	if err := cs.sendRequest(ctx, protocol.MethodResourcesRead, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListResourceTemplates lists the resource templates on the server
func (cs *ClientSession) ListResourceTemplates(ctx context.Context, params *protocol.ListResourceTemplatesParams) (*protocol.ListResourceTemplatesResult, error) {
	if params == nil {
		params = &protocol.ListResourceTemplatesParams{}
	}
	var result protocol.ListResourceTemplatesResult
	if err := cs.sendRequest(ctx, protocol.MethodResourcesTemplatesList, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SubscribeResource subscribes to resource updates
func (cs *ClientSession) SubscribeResource(ctx context.Context, params *protocol.SubscribeParams) error {
	var result protocol.EmptyResult
	return cs.sendRequest(ctx, protocol.MethodResourcesSubscribe, params, &result)
}

// UnsubscribeResource unsubscribes from resource updates
func (cs *ClientSession) UnsubscribeResource(ctx context.Context, params *protocol.UnsubscribeParams) error {
	var result protocol.EmptyResult
	return cs.sendRequest(ctx, protocol.MethodResourcesUnsubscribe, params, &result)
}

// ListPrompts lists the currently available prompts on the server
func (cs *ClientSession) ListPrompts(ctx context.Context, params *protocol.ListPromptsParams) (*protocol.ListPromptsResult, error) {
	if params == nil {
		params = &protocol.ListPromptsParams{}
	}
	var result protocol.ListPromptsResult
	if err := cs.sendRequest(ctx, protocol.MethodPromptsList, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetPrompt retrieves a prompt from the server
func (cs *ClientSession) GetPrompt(ctx context.Context, params *protocol.GetPromptParams) (*protocol.GetPromptResult, error) {
	var result protocol.GetPromptResult
	if err := cs.sendRequest(ctx, protocol.MethodPromptsGet, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetLoggingLevel sets the logging level on the server
func (cs *ClientSession) SetLoggingLevel(ctx context.Context, params *protocol.SetLoggingLevelParams) error {
	var result protocol.EmptyResult
	return cs.sendRequest(ctx, protocol.MethodLoggingSetLevel, params, &result)
}

// Complete requests auto-completion
func (cs *ClientSession) Complete(ctx context.Context, params *protocol.CompleteRequest) (*protocol.CompleteResult, error) {
	var result protocol.CompleteResult
	if err := cs.sendRequest(ctx, protocol.MethodCompletionComplete, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// NotifyRootsListChanged notifies the server that the roots list has changed
func (cs *ClientSession) NotifyRootsListChanged(ctx context.Context) error {
	return cs.sendNotification(ctx, protocol.NotificationRootsListChanged, &protocol.RootsListChangedNotification{})
}

// NotifyProgress sends a progress notification
func (cs *ClientSession) NotifyProgress(ctx context.Context, params *protocol.ProgressNotificationParams) error {
	return cs.sendNotification(ctx, protocol.NotificationProgress, params)
}

// NotifyCancelled sends a cancellation notification
func (cs *ClientSession) NotifyCancelled(ctx context.Context, params *protocol.CancelledNotificationParams) error {
	return cs.sendNotification(ctx, protocol.NotificationCancelled, params)
}
