package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
)

// Caller is the slice of *client.Client the task helpers need.
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// RouteNames maps the extension's methods to the params key sent as the
// Mcp-Name routing header, as the draft requires over Streamable HTTP.
var RouteNames = map[string]string{
	MethodGet:    "taskId",
	MethodUpdate: "taskId",
	MethodCancel: "taskId",
}

// EnableClient configures opts to declare the tasks capability and emit the
// extension's routing headers. Call it before client.New:
//
//	opts := &client.Options{...}
//	tasks.EnableClient(opts)
//	c := client.New(transport, opts)
func EnableClient(opts *client.Options) {
	if opts.Extensions == nil {
		opts.Extensions = make(map[string]any)
	}
	opts.Extensions[ID] = Capability{}
	if opts.RouteNames == nil {
		opts.RouteNames = make(map[string]string)
	}
	maps.Copy(opts.RouteNames, RouteNames)
}

// AsTask inspects a CallTool error: when the server turned the call into a
// task (resultType "task"), it returns the CreateTaskResult.
func AsTask(err error) (*Task, bool) {
	var ute *client.UnexpectedResultTypeError
	if !errors.As(err, &ute) || ute.ResultType != ResultTypeTask {
		return nil, false
	}
	var t Task
	if json.Unmarshal(ute.Raw, &t) != nil || t.TaskID == "" {
		return nil, false
	}
	return &t, true
}

// Get fetches the current task snapshot.
func Get(ctx context.Context, c Caller, taskID string) (*DetailedTask, error) {
	var d DetailedTask
	if err := c.Call(ctx, MethodGet, GetParams{TaskID: taskID}, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Update answers an input_required task.
func Update(ctx context.Context, c Caller, taskID string, responses protocol.InputResponses) error {
	return c.Call(ctx, MethodUpdate, UpdateParams{TaskID: taskID, InputResponses: responses}, nil)
}

// Cancel requests cooperative cancellation.
func Cancel(ctx context.Context, c Caller, taskID string) error {
	return c.Call(ctx, MethodCancel, CancelParams{TaskID: taskID}, nil)
}

// OnInputFunc answers a task's outstanding input requests during Await.
type OnInputFunc func(ctx context.Context, requests protocol.InputRequests) (protocol.InputResponses, error)

// Await polls the task until it reaches a terminal status, honoring the
// server's pollIntervalMs. When the task requires input and onInput is
// non-nil, Await answers via tasks/update and keeps waiting; with a nil
// onInput an input_required task is returned as-is.
func Await(ctx context.Context, c Caller, task *Task, onInput OnInputFunc) (*DetailedTask, error) {
	interval := task.PollIntervalMs
	if interval <= 0 {
		interval = 500
	}
	for first := true; ; first = false {
		if !first {
			if err := sleep(ctx, time.Duration(interval)*time.Millisecond); err != nil {
				return nil, err
			}
		}
		d, err := Get(ctx, c, task.TaskID)
		if err != nil {
			return nil, err
		}
		if d.Status.Terminal() {
			return d, nil
		}
		if d.Status == StatusInputRequired {
			if onInput == nil {
				return d, nil
			}
			if len(d.InputRequests) > 0 {
				responses, err := onInput(ctx, d.InputRequests)
				if err != nil {
					return d, err
				}
				if err := Update(ctx, c, d.TaskID, responses); err != nil {
					return d, err
				}
			}
		}
		if d.PollIntervalMs > 0 {
			interval = d.PollIntervalMs
		}
	}
}

// TaskEvent decodes a notifications/tasks subscription event.
func TaskEvent(ev client.Event) (*DetailedTask, bool) {
	if ev.Method != NotificationTasks {
		return nil, false
	}
	var d DetailedTask
	if json.Unmarshal(ev.Params, &d) != nil || d.TaskID == "" {
		return nil, false
	}
	return &d, true
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
