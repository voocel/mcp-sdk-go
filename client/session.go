package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

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
	case protocol.MethodSamplingCreateMessage:
		cs.handleCreateMessage(ctx, msg)
	case protocol.MethodElicitationCreate:
		cs.handleElicitation(ctx, msg)
	case protocol.NotificationToolsListChanged:
		cs.handleToolListChanged(ctx, msg)
	case protocol.NotificationPromptsListChanged:
		cs.handlePromptListChanged(ctx, msg)
	case protocol.NotificationResourcesListChanged:
		cs.handleResourceListChanged(ctx, msg)
	case protocol.NotificationResourcesUpdated:
		cs.handleResourceUpdated(ctx, msg)
	case protocol.NotificationLoggingMessage:
		cs.handleLoggingMessage(ctx, msg)
	case protocol.NotificationProgress:
		cs.handleProgressNotification(ctx, msg)
	case protocol.MethodRootsList:
		cs.handleListRoots(ctx, msg)
	}
}

// handleCreateMessage 处理 sampling/createMessage 请求
func (cs *ClientSession) handleCreateMessage(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.CreateMessageHandler == nil {
		cs.sendErrorResponse(ctx, msg, protocol.MethodNotFound, "Method not found")
		return
	}

	var params protocol.CreateMessageRequest
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		cs.sendErrorResponse(ctx, msg, protocol.InvalidParams, "Invalid params")
		return
	}

	result, err := cs.client.opts.CreateMessageHandler(ctx, &params)
	if err != nil {
		cs.sendErrorResponse(ctx, msg, protocol.InternalError, err.Error())
		return
	}

	cs.sendSuccessResponse(ctx, msg, result)
}

// handleElicitation 处理 elicitation/create 请求
func (cs *ClientSession) handleElicitation(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.ElicitationHandler == nil {
		cs.sendErrorResponse(ctx, msg, protocol.MethodNotFound, "Method not found")
		return
	}

	var params protocol.ElicitationCreateParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		cs.sendErrorResponse(ctx, msg, protocol.InvalidParams, "Invalid params")
		return
	}

	result, err := cs.client.opts.ElicitationHandler(ctx, &params)
	if err != nil {
		cs.sendErrorResponse(ctx, msg, protocol.InternalError, err.Error())
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

	resultJSON, err := json.Marshal(result)
	if err != nil {
		// 记录 JSON 序列化错误
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to marshal response result: %v\n", err)
		// 直接构建错误响应，避免递归
		errResp := &protocol.JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &protocol.JSONRPCError{
				Code:    protocol.InternalError,
				Message: fmt.Sprintf("Failed to marshal result: %v", err),
			},
		}
		if writeErr := cs.conn.Write(ctx, errResp); writeErr != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Failed to write error response: %v\n", writeErr)
		}
		return
	}

	resp := &protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}

	if err := cs.conn.Write(ctx, resp); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to write response: %v\n", err)
	}
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

	if err := cs.conn.Write(ctx, resp); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to write error response: %v\n", err)
	}
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
