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

// sendRequest sends a request and waits for a response
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

// sendNotification sends a notification
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

// handleMessages handles messages from the server
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

// handleResponse handles response messages
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

// handleRequest handles requests or notifications from the server
func (cs *ClientSession) handleRequest(ctx context.Context, msg *protocol.JSONRPCMessage) {
	switch msg.Method {
	case protocol.MethodPing:
		cs.handlePing(ctx, msg)
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
	case protocol.NotificationCancelled:
		cs.handleCancelled(ctx, msg)
	case protocol.MethodRootsList:
		cs.handleListRoots(ctx, msg)
	}
}

// handlePing handles ping requests
func (cs *ClientSession) handlePing(ctx context.Context, msg *protocol.JSONRPCMessage) {
	// Ping requests don't need parameters, return success response directly
	cs.sendSuccessResponse(ctx, msg, &protocol.EmptyResult{})
}

// handleCreateMessage handles sampling/createMessage requests
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

	// Track request and support cancellation
	requestID := protocol.IDToString(msg.ID)
	requestCtx, cancel := context.WithCancel(ctx)

	cs.mu.Lock()
	cs.incomingRequests[requestID] = cancel
	cs.mu.Unlock()

	// Ensure cleanup after request completes
	defer func() {
		cs.mu.Lock()
		delete(cs.incomingRequests, requestID)
		cs.mu.Unlock()
		cancel()
	}()

	result, err := cs.client.opts.CreateMessageHandler(requestCtx, &params)
	if err != nil {
		cs.sendErrorResponse(ctx, msg, protocol.InternalError, err.Error())
		return
	}

	cs.sendSuccessResponse(ctx, msg, result)
}

// handleElicitation handles elicitation/create requests
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

	requestID := protocol.IDToString(msg.ID)
	requestCtx, cancel := context.WithCancel(ctx)

	cs.mu.Lock()
	cs.incomingRequests[requestID] = cancel
	cs.mu.Unlock()

	defer func() {
		cs.mu.Lock()
		delete(cs.incomingRequests, requestID)
		cs.mu.Unlock()
		cancel()
	}()

	result, err := cs.client.opts.ElicitationHandler(requestCtx, &params)
	if err != nil {
		cs.sendErrorResponse(ctx, msg, protocol.InternalError, err.Error())
		return
	}

	cs.sendSuccessResponse(ctx, msg, result)
}

// handleListRoots handles roots/list requests
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

// handleToolListChanged handles tool list change notifications
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

// handlePromptListChanged handles prompt list change notifications
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

// handleResourceListChanged handles resource list change notifications
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

// handleResourceUpdated handles resource update notifications
func (cs *ClientSession) handleResourceUpdated(ctx context.Context, msg *protocol.JSONRPCMessage) {
	if cs.client.opts.ResourceUpdatedHandler == nil {
		return
	}

	var params protocol.ResourceUpdatedNotificationParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	cs.client.opts.ResourceUpdatedHandler(ctx, &params)
}

// handleLoggingMessage handles logging message notifications
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

// handleProgressNotification handles progress notifications
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

// handleCancelled handles cancellation notifications
func (cs *ClientSession) handleCancelled(ctx context.Context, msg *protocol.JSONRPCMessage) {
	var params protocol.CancelledNotificationParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return
	}

	requestID := ""
	switch v := params.RequestID.(type) {
	case string:
		requestID = v
	case float64:
		requestID = fmt.Sprintf("%.0f", v)
	case json.Number:
		requestID = v.String()
	default:
		return
	}

	cs.mu.Lock()
	cancel, exists := cs.incomingRequests[requestID]
	cs.mu.Unlock()

	if exists {
		cancel()
	}
}

// sendSuccessResponse sends a success response
func (cs *ClientSession) sendSuccessResponse(ctx context.Context, req *protocol.JSONRPCMessage, result interface{}) {
	if req.ID == nil {
		return
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to marshal response result: %v\n", err)
		// Build error response directly to avoid recursion
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

// sendErrorResponse sends an error response
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

// startKeepalive starts the keepalive mechanism
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
					// Ping failed, close connection
					_ = cs.Close()
					return
				}
			}
		}
	}()
}
