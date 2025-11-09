package client

import (
	"context"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// Ping 向服务器发送 ping 请求
func (cs *ClientSession) Ping(ctx context.Context, params *protocol.PingParams) error {
	if params == nil {
		params = &protocol.PingParams{}
	}
	var result protocol.EmptyResult
	return cs.sendRequest(ctx, "ping", params, &result)
}

// ListTools 列出服务器上当前可用的工具
func (cs *ClientSession) ListTools(ctx context.Context, params *protocol.ListToolsParams) (*protocol.ListToolsResult, error) {
	if params == nil {
		params = &protocol.ListToolsParams{}
	}
	var result protocol.ListToolsResult
	if err := cs.sendRequest(ctx, "tools/list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CallTool 调用服务器上的工具
func (cs *ClientSession) CallTool(ctx context.Context, params *protocol.CallToolParams) (*protocol.CallToolResult, error) {
	var result protocol.CallToolResult
	if err := cs.sendRequest(ctx, "tools/call", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListResources 列出服务器上当前可用的资源
func (cs *ClientSession) ListResources(ctx context.Context, params *protocol.ListResourcesParams) (*protocol.ListResourcesResult, error) {
	if params == nil {
		params = &protocol.ListResourcesParams{}
	}
	var result protocol.ListResourcesResult
	if err := cs.sendRequest(ctx, "resources/list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ReadResource 读取服务器上的资源
func (cs *ClientSession) ReadResource(ctx context.Context, params *protocol.ReadResourceParams) (*protocol.ReadResourceResult, error) {
	var result protocol.ReadResourceResult
	if err := cs.sendRequest(ctx, "resources/read", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListResourceTemplates 列出服务器上的资源模板
func (cs *ClientSession) ListResourceTemplates(ctx context.Context, params *protocol.ListResourcesParams) (*protocol.ListResourcesResult, error) {
	if params == nil {
		params = &protocol.ListResourcesParams{}
	}
	var result protocol.ListResourcesResult
	if err := cs.sendRequest(ctx, "resources/templates/list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SubscribeResource 订阅资源更新
func (cs *ClientSession) SubscribeResource(ctx context.Context, params *protocol.SubscribeParams) error {
	var result protocol.EmptyResult
	return cs.sendRequest(ctx, "resources/subscribe", params, &result)
}

// UnsubscribeResource 取消订阅资源更新
func (cs *ClientSession) UnsubscribeResource(ctx context.Context, params *protocol.UnsubscribeParams) error {
	var result protocol.EmptyResult
	return cs.sendRequest(ctx, "resources/unsubscribe", params, &result)
}

// ListPrompts 列出服务器上当前可用的提示
func (cs *ClientSession) ListPrompts(ctx context.Context, params *protocol.ListPromptsParams) (*protocol.ListPromptsResult, error) {
	if params == nil {
		params = &protocol.ListPromptsParams{}
	}
	var result protocol.ListPromptsResult
	if err := cs.sendRequest(ctx, "prompts/list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetPrompt 获取服务器上的提示
func (cs *ClientSession) GetPrompt(ctx context.Context, params *protocol.GetPromptParams) (*protocol.GetPromptResult, error) {
	var result protocol.GetPromptResult
	if err := cs.sendRequest(ctx, "prompts/get", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetLoggingLevel 设置服务器的日志级别
func (cs *ClientSession) SetLoggingLevel(ctx context.Context, params *protocol.SetLoggingLevelParams) error {
	var result protocol.EmptyResult
	return cs.sendRequest(ctx, "logging/setLevel", params, &result)
}

// Complete 请求自动补全
func (cs *ClientSession) Complete(ctx context.Context, params *protocol.CompleteRequest) (*protocol.CompleteResult, error) {
	var result protocol.CompleteResult
	if err := cs.sendRequest(ctx, "completion/complete", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// NotifyRootsListChanged 通知服务器根目录列表已更改
func (cs *ClientSession) NotifyRootsListChanged(ctx context.Context) error {
	return cs.sendNotification(ctx, "notifications/roots/list_changed", &protocol.RootsListChangedNotification{})
}

// NotifyProgress 发送进度通知
func (cs *ClientSession) NotifyProgress(ctx context.Context, params *protocol.ProgressNotificationParams) error {
	return cs.sendNotification(ctx, "notifications/progress", params)
}

// NotifyCancelled 发送取消通知
func (cs *ClientSession) NotifyCancelled(ctx context.Context, params *protocol.CancelledNotificationParams) error {
	return cs.sendNotification(ctx, "notifications/cancelled", params)
}
