package protocol

// TaskStatus represents the current state of a task (MCP 2025-11-25)
type TaskStatus string

const (
	// TaskStatusWorking indicates the task is currently being processed
	TaskStatusWorking TaskStatus = "working"
	// TaskStatusInputRequired indicates the task requires additional input from the requestor
	TaskStatusInputRequired TaskStatus = "input_required"
	// TaskStatusCompleted indicates the task has completed successfully
	TaskStatusCompleted TaskStatus = "completed"
	// TaskStatusFailed indicates the task failed during execution
	TaskStatusFailed TaskStatus = "failed"
	// TaskStatusCancelled indicates the task was cancelled
	TaskStatusCancelled TaskStatus = "cancelled"
)

// Task represents a durable state machine that carries information about the underlying
// execution state of a request (MCP 2025-11-25)
type Task struct {
	// TaskID is the unique identifier for this task
	TaskID string `json:"taskId"`
	// Status is the current state of the task execution
	Status TaskStatus `json:"status"`
	// StatusMessage is an optional human-readable message providing additional details
	StatusMessage string `json:"statusMessage,omitempty"`
	// CreatedAt is the ISO 8601 timestamp when the task was created
	CreatedAt string `json:"createdAt"`
	// LastUpdatedAt is the ISO 8601 timestamp when the task status was last updated
	LastUpdatedAt string `json:"lastUpdatedAt"`
	// TTL is the time-to-live in milliseconds from creation before task may be deleted
	// null indicates no TTL is set
	TTL *int `json:"ttl"`
	// PollInterval is the suggested time in milliseconds between status checks
	PollInterval *int `json:"pollInterval,omitempty"`
}

// TaskMetadata is used for augmenting requests with task execution details (MCP 2025-11-25)
type TaskMetadata struct {
	// TTL specifies the retention duration of a task in milliseconds
	TTL *int `json:"ttl,omitempty"`
}

// TaskSupport indicates the level of task support for a tool (MCP 2025-11-25)
type TaskSupport string

const (
	// TaskSupportRequired means clients MUST invoke the tool as a task
	TaskSupportRequired TaskSupport = "required"
	// TaskSupportOptional means clients MAY invoke the tool as a task or normal request
	TaskSupportOptional TaskSupport = "optional"
	// TaskSupportForbidden means clients MUST NOT invoke the tool as a task
	TaskSupportForbidden TaskSupport = "forbidden"
)

// CreateTaskResult is returned when a task-augmented request is accepted (MCP 2025-11-25)
type CreateTaskResult struct {
	Meta map[string]any `json:"_meta,omitempty"`
	Task Task           `json:"task"`
}

// GetTaskParams represents the parameters for tasks/get request (MCP 2025-11-25)
type GetTaskParams struct {
	Meta   map[string]any `json:"_meta,omitempty"`
	TaskID string         `json:"taskId"`
}

// GetTaskResult represents the result of tasks/get request (MCP 2025-11-25)
// Per spec, the result directly contains Task fields (no "task" wrapper)
type GetTaskResult struct {
	Task
}

// ListTasksParams represents the parameters for tasks/list request (MCP 2025-11-25)
type ListTasksParams struct {
	Meta   map[string]any `json:"_meta,omitempty"`
	Cursor string         `json:"cursor,omitempty"`
}

// ListTasksResult represents the result of tasks/list request (MCP 2025-11-25)
type ListTasksResult struct {
	Meta       map[string]any `json:"_meta,omitempty"`
	Tasks      []Task         `json:"tasks"`
	NextCursor *string        `json:"nextCursor,omitempty"`
}

// CancelTaskParams represents the parameters for tasks/cancel request (MCP 2025-11-25)
type CancelTaskParams struct {
	Meta   map[string]any `json:"_meta,omitempty"`
	TaskID string         `json:"taskId"`
	Reason string         `json:"reason,omitempty"`
}

// CancelTaskResult represents the result of tasks/cancel request (MCP 2025-11-25)
// Per spec, the result directly contains Task fields (no "task" wrapper)
type CancelTaskResult struct {
	Task
}

// TaskResultParams represents the parameters for tasks/result request (MCP 2025-11-25)
type TaskResultParams struct {
	Meta   map[string]any `json:"_meta,omitempty"`
	TaskID string         `json:"taskId"`
}

// TaskStatusNotificationParams represents the parameters for notifications/tasks/status (MCP 2025-11-25)
type TaskStatusNotificationParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	Task
}

// TasksCapability represents the tasks capability for servers (MCP 2025-11-25)
type TasksCapability struct {
	// List indicates server supports the tasks/list operation
	List *struct{} `json:"list,omitempty"`
	// Cancel indicates server supports the tasks/cancel operation
	Cancel *struct{} `json:"cancel,omitempty"`
	// Requests specifies which request types support task augmentation
	Requests *ServerTaskRequestsCapability `json:"requests,omitempty"`
}

// ServerTaskRequestsCapability specifies which server-side requests support tasks (MCP 2025-11-25)
type ServerTaskRequestsCapability struct {
	// Tools specifies task support for tool-related requests
	Tools *ToolsTaskCapability `json:"tools,omitempty"`
}

// ToolsTaskCapability specifies task support for tool operations (MCP 2025-11-25)
type ToolsTaskCapability struct {
	// Call specifies task augmentation support for tools/call (MCP 2025-11-25)
	Call *struct{} `json:"call,omitempty"`
}

// ClientTasksCapability represents the tasks capability for clients (MCP 2025-11-25)
type ClientTasksCapability struct {
	// List indicates client supports the tasks/list operation
	List *struct{} `json:"list,omitempty"`
	// Cancel indicates client supports the tasks/cancel operation
	Cancel *struct{} `json:"cancel,omitempty"`
	// Requests specifies which request types support task augmentation
	Requests *ClientTaskRequestsCapability `json:"requests,omitempty"`
}

// ClientTaskRequestsCapability specifies which client-side requests support tasks (MCP 2025-11-25)
type ClientTaskRequestsCapability struct {
	// Sampling specifies task support for sampling-related requests
	Sampling *SamplingTaskCapability `json:"sampling,omitempty"`
	// Elicitation specifies task support for elicitation-related requests
	Elicitation *ElicitationTaskCapability `json:"elicitation,omitempty"`
}

// SamplingTaskCapability specifies task support for sampling operations (MCP 2025-11-25)
type SamplingTaskCapability struct {
	// CreateMessage indicates client supports task-augmented sampling/createMessage requests
	CreateMessage *struct{} `json:"createMessage,omitempty"`
}

// ElicitationTaskCapability specifies task support for elicitation operations (MCP 2025-11-25)
type ElicitationTaskCapability struct {
	// Create indicates client supports task-augmented elicitation/create requests
	Create *struct{} `json:"create,omitempty"`
}
