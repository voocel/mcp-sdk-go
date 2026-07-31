// Package tasks implements the official MCP tasks extension
// io.modelcontextprotocol/tasks (draft): task-augmented tool execution with
// polling, input_required rendezvous and status notifications.
//
// Server side: Install the extension, then register long-running tools with
// AddTool. Client side: declare the capability via client Options.Extensions,
// detect task creation with AsTask, and follow the task with Await.
package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// ID is the extension identifier, declared in capabilities.extensions on both
// sides.
const ID = "io.modelcontextprotocol/tasks"

const (
	MethodGet    = "tasks/get"
	MethodUpdate = "tasks/update"
	MethodCancel = "tasks/cancel"

	// NotificationTasks is the subscriptions/listen notification method; its
	// params are a full DetailedTask.
	NotificationTasks = "notifications/tasks"

	// FilterTaskIDs is the subscriptions/listen filter field selecting task
	// status notifications.
	FilterTaskIDs = "taskIds"

	// ResultTypeTask marks a tools/call result that created a task.
	ResultTypeTask = "task"
)

type Status string

const (
	StatusWorking       Status = "working"
	StatusInputRequired Status = "input_required"
	StatusCompleted     Status = "completed"
	StatusFailed        Status = "failed"
	StatusCancelled     Status = "cancelled"
)

// Terminal reports whether the status is final; a terminal task never changes
// again.
func (s Status) Terminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// Task is the base task object. Returned from tools/call it is the
// CreateTaskResult (resultType "task").
type Task struct {
	protocol.WithMeta
	TaskID        string `json:"taskId"`
	Status        Status `json:"status"`
	StatusMessage string `json:"statusMessage,omitempty"`
	CreatedAt     string `json:"createdAt"`
	LastUpdatedAt string `json:"lastUpdatedAt"`
	// TTLMs is how long the server retains the task; null means indefinitely.
	// The field is required on the wire.
	TTLMs          *int64 `json:"ttlMs"`
	PollIntervalMs int64  `json:"pollIntervalMs,omitempty"`
}

func (*Task) ResultType() string { return ResultTypeTask }

// DetailedTask adds the status-dependent fields. It is the tasks/get result (a
// plain complete result) and the notifications/tasks payload.
type DetailedTask struct {
	Task
	// InputRequests is set while Status is input_required.
	InputRequests protocol.InputRequests `json:"inputRequests,omitempty"`
	// Result is the stored tool result (including resultType) once completed.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is set once failed.
	Error *protocol.Error `json:"error,omitempty"`
}

func (*DetailedTask) ResultType() string { return protocol.ResultTypeComplete }

// ToolResult decodes a completed task's stored tools/call result.
func (d *DetailedTask) ToolResult() (*protocol.CallToolResult, error) {
	if d.Status != StatusCompleted {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams,
			Message: "task " + d.TaskID + " is " + string(d.Status) + ", not completed"}
	}
	var res protocol.CallToolResult
	if err := json.Unmarshal(d.Result, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type GetParams struct {
	TaskID string `json:"taskId"`
}

type UpdateParams struct {
	TaskID         string                  `json:"taskId"`
	InputResponses protocol.InputResponses `json:"inputResponses"`
}

type CancelParams struct {
	TaskID string `json:"taskId"`
}

// Capability is the value to declare under client Options.Extensions[ID].
type Capability struct{}

func newTaskID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func taskTopic(id string) string { return "task:" + id }
