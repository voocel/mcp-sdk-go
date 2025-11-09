package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// sendRequest 发送请求并等待响应
func (cs *ClientSession) sendRequest(ctx context.Context, method string, params interface{}, result interface{}) error {
	cs.mu.Lock()
	cs.nextID++
	id := strconv.FormatInt(cs.nextID, 10)
	cs.mu.Unlock()

	idJSON, _ := json.Marshal(id)
	msg := &protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      idJSON,
		Method:  method,
	}

	if params != nil {
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal params: %w", err)
		}
		msg.Params = paramsJSON
	}

	pending := &pendingRequest{
		method:   method,
		response: make(chan *protocol.JSONRPCMessage, 1),
		err:      make(chan error, 1),
	}

	cs.mu.Lock()
	cs.pending[id] = pending
	cs.mu.Unlock()

	if err := cs.conn.Write(ctx, msg); err != nil {
		cs.mu.Lock()
		delete(cs.pending, id)
		cs.mu.Unlock()
		return fmt.Errorf("failed to write request: %w", err)
	}

	select {
	case <-ctx.Done():
		cs.mu.Lock()
		delete(cs.pending, id)
		cs.mu.Unlock()
		return ctx.Err()
	case err := <-pending.err:
		return err
	case resp := <-pending.response:
		if resp.Error != nil {
			return fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		if result != nil && resp.Result != nil {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("failed to unmarshal result: %w", err)
			}
		}

		return nil
	}
}

// sendNotification 发送通知
func (cs *ClientSession) sendNotification(ctx context.Context, method string, params interface{}) error {
	msg := &protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
	}

	if params != nil {
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal params: %w", err)
		}
		msg.Params = paramsJSON
	}

	if err := cs.conn.Write(ctx, msg); err != nil {
		return fmt.Errorf("failed to write notification: %w", err)
	}

	return nil
}

// handleMessages 处理来自服务器的消息
func (cs *ClientSession) handleMessages(ctx context.Context) error {
	for {
		msg, err := cs.conn.Read(ctx)
		if err != nil {
			return err
		}

		if msg.ID != nil {
			cs.handleResponse(msg)
			continue
		}

		if msg.Method != "" {
			cs.handleRequest(ctx, msg)
			continue
		}
	}
}

// handleResponse 处理响应消息
func (cs *ClientSession) handleResponse(msg *protocol.JSONRPCMessage) {
	if msg.ID == nil {
		return
	}

	var id string
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		return
	}

	cs.mu.Lock()
	pending, ok := cs.pending[id]
	if ok {
		delete(cs.pending, id)
	}
	cs.mu.Unlock()

	if !ok {
		return
	}

	if msg.Error != nil {
		pending.err <- fmt.Errorf("RPC error %d: %s", msg.Error.Code, msg.Error.Message)
	} else {
		pending.response <- msg
	}
}

// handleRequest 处理来自服务器的请求或通知
func (cs *ClientSession) handleRequest(ctx context.Context, msg *protocol.JSONRPCMessage) {
	switch msg.Method {
	case "sampling/createMessage":
		cs.handleCreateMessage(ctx, msg)
	case "elicitation/create":
		cs.handleElicitation(ctx, msg)
	case "notifications/tools/list_changed":
		cs.handleToolListChanged(ctx, msg)
	case "notifications/prompts/list_changed":
		cs.handlePromptListChanged(ctx, msg)
	case "notifications/resources/list_changed":
		cs.handleResourceListChanged(ctx, msg)
	case "notifications/resources/updated":
		cs.handleResourceUpdated(ctx, msg)
	case "notifications/message":
		cs.handleLoggingMessage(ctx, msg)
	case "notifications/progress":
		cs.handleProgressNotification(ctx, msg)
	case "roots/list":
		cs.handleListRoots(ctx, msg)
	}
}

// handleCreateMessage 处理 sampling/createMessage 请求
func (cs *ClientSession) handleCreateMessage(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.CreateMessageHandler == nil {
		cs.sendErrorResponse(ctx, msg, -32601, "Method not found")
		return
	}

	var params protocol.CreateMessageRequest
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		cs.sendErrorResponse(ctx, msg, -32602, "Invalid params")
		return
	}

	result, err := cs.client.opts.CreateMessageHandler(ctx, &params)
	if err != nil {
		cs.sendErrorResponse(ctx, msg, -32603, err.Error())
		return
	}

	cs.sendSuccessResponse(ctx, msg, result)
}

// handleElicitation 处理 elicitation/create 请求
func (cs *ClientSession) handleElicitation(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.ElicitationHandler == nil {
		cs.sendErrorResponse(ctx, msg, -32601, "Method not found")
		return
	}

	var params protocol.ElicitationCreateParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		cs.sendErrorResponse(ctx, msg, -32602, "Invalid params")
		return
	}

	result, err := cs.client.opts.ElicitationHandler(ctx, &params)
	if err != nil {
		cs.sendErrorResponse(ctx, msg, -32603, err.Error())
		return
	}

	cs.sendSuccessResponse(ctx, msg, result)
}

// handleListRoots 处理 roots/list 请求
func (cs *ClientSession) handleListRoots(ctx context.Context, msg *protocol.JSONRPCMessage) {
	rootPtrs := cs.client.ListRoots()
	roots := make([]protocol.Root, len(rootPtrs))
	for i, r := range rootPtrs {
		roots[i] = *r
	}
	result := &protocol.ListRootsResult{
		Roots: roots,
	}
	cs.sendSuccessResponse(ctx, msg, result)
}

// handleToolListChanged 处理工具列表变更通知
func (cs *ClientSession) handleToolListChanged(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.ToolListChangedHandler == nil {
		return
	}

	var params protocol.ToolsListChangedNotification
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	cs.client.opts.ToolListChangedHandler(ctx, &params)
}

// handlePromptListChanged 处理提示列表变更通知
func (cs *ClientSession) handlePromptListChanged(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.PromptListChangedHandler == nil {
		return
	}

	var params protocol.PromptListChangedParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	cs.client.opts.PromptListChangedHandler(ctx, &params)
}

// handleResourceListChanged 处理资源列表变更通知
func (cs *ClientSession) handleResourceListChanged(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.ResourceListChangedHandler == nil {
		return
	}

	var params protocol.ResourceListChangedParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	cs.client.opts.ResourceListChangedHandler(ctx, &params)
}

// handleResourceUpdated 处理资源更新通知
func (cs *ClientSession) handleResourceUpdated(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.ResourceUpdatedHandler == nil {
		return
	}

	var params protocol.ResourceUpdatedParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	cs.client.opts.ResourceUpdatedHandler(ctx, &params)
}

// handleLoggingMessage 处理日志消息通知
func (cs *ClientSession) handleLoggingMessage(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.LoggingMessageHandler == nil {
		return
	}

	var params protocol.LoggingMessageParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	cs.client.opts.LoggingMessageHandler(ctx, &params)
}

// handleProgressNotification 处理进度通知
func (cs *ClientSession) handleProgressNotification(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.ProgressNotificationHandler == nil {
		return
	}

	var params protocol.ProgressNotificationParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	cs.client.opts.ProgressNotificationHandler(ctx, &params)
}

// sendSuccessResponse 发送成功响应
func (cs *ClientSession) sendSuccessResponse(ctx context.Context, req *protocol.JSONRPCMessage, result interface{}) {
	if req.ID == nil {
		return
	}

	resultJSON, _ := json.Marshal(result)
	resp := &protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}

	_ = cs.conn.Write(ctx, resp)
}

// sendErrorResponse 发送错误响应
func (cs *ClientSession) sendErrorResponse(ctx context.Context, req *protocol.JSONRPCMessage, code int, message string) {
	if req.ID == nil {
		return
	}

	resp := &protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      req.ID,
		Error: &protocol.JSONRPCError{
			Code:    code,
			Message: message,
		},
	}

	_ = cs.conn.Write(ctx, resp)
}

// startKeepalive 启动 keepalive 机制
func (cs *ClientSession) startKeepalive(interval time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	cs.keepaliveCancel = cancel

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, cancel := context.WithTimeout(ctx, interval)
				err := cs.Ping(pingCtx, nil)
				cancel()

				if err != nil {
					// Ping 失败,关闭连接
					_ = cs.Close()
					return
				}
			}
		}
	}()
}
