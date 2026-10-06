package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
)

type Options struct {
	// PollIntervalMs is advertised on every task (default 500).
	PollIntervalMs int64
	// TTL is how long finished tasks stay retrievable (default 5 minutes).
	TTL time.Duration
	// Reject makes task tools fail with -32021 for clients that did not
	// declare the tasks capability on the request, instead of falling back to
	// synchronous execution.
	Reject bool
	// Owner identifies who may access a task, typically the authenticated
	// principal read from ctx. Task IDs embed a tag of their owner, so a request
	// from anyone else sees the task as not found. Nil puts every caller in one
	// namespace, where the task ID alone (128 random bits) is a bearer token.
	Owner func(ctx context.Context, req *server.Request) string
}

// Tasks is the installed extension; AddTool registers task-capable tools
// against it.
type Tasks struct {
	srv   *server.Server
	store Store
	opts  Options

	mu      sync.Mutex      // guards live
	stateMu sync.Mutex      // serializes setTask's read-modify-write against the Store
	live    map[string]*run // in-flight runs: cancel + input rendezvous
}

type run struct {
	cancel context.CancelFunc
	input  chan protocol.InputResponses
	done   chan struct{} // closed when the runner finished; unblocks updates
}

// Install registers the tasks extension methods, capability and subscription
// filter on s. The server itself stays unaware of tasks; everything flows
// through the generic extension seam.
func Install(s *server.Server, store Store, opts *Options) *Tasks {
	t := &Tasks{srv: s, store: store, live: make(map[string]*run)}
	if opts != nil {
		t.opts = *opts
	}
	if t.opts.PollIntervalMs <= 0 {
		t.opts.PollIntervalMs = 500
	}
	if t.opts.TTL <= 0 {
		t.opts.TTL = 5 * time.Minute
	}
	s.AddExtension(server.Extension{
		ID:       ID,
		Settings: struct{}{},
		Methods: map[string]server.RawHandler{
			MethodGet:    t.handleGet,
			MethodUpdate: t.handleUpdate,
			MethodCancel: t.handleCancel,
		},
		NameParams: map[string]string{
			MethodGet:    "taskId",
			MethodUpdate: "taskId",
			MethodCancel: "taskId",
		},
		Topics: func(ctx context.Context, req *server.Request, key string, value json.RawMessage) ([]string, bool, error) {
			if key != FilterTaskIDs {
				return nil, false, nil
			}
			// The draft requires MissingRequiredClientCapability for
			// non-declaring clients requesting task notifications.
			if err := requireCapability(req); err != nil {
				return nil, true, err
			}
			var ids []string
			if json.Unmarshal(value, &ids) != nil {
				return nil, true, nil
			}
			var topics []string
			for _, id := range ids {
				if t.owns(ctx, req, id) {
					topics = append(topics, taskTopic(id))
				}
			}
			return topics, true, nil
		},
	})
	return t
}

// cloneSchema deep-copies a schema via a JSON round trip. Registration
// errors fail loudly, matching server.AddTool's own panic on bad schemas.
func cloneSchema(name string, m protocol.JSONSchema) protocol.JSONSchema {
	if m == nil {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("tasks: AddTool %q: output schema does not marshal: %v", name, err))
	}
	var cp protocol.JSONSchema
	if err := json.Unmarshal(raw, &cp); err != nil {
		panic(fmt.Sprintf("tasks: AddTool %q: invalid output schema: %v", name, err))
	}
	return cp
}

// requireCapability enforces the draft's MUST: tasks/* requests and taskIds
// subscriptions from clients that did not declare the capability fail with
// MissingRequiredClientCapability.
func requireCapability(req *server.Request) error {
	if _, ok := req.Extension(ID); !ok {
		return protocol.MissingCapabilityError(protocol.ClientCapabilities{
			Extensions: map[string]json.RawMessage{ID: json.RawMessage("{}")},
		})
	}
	return nil
}

// HandlerFor is a task-capable tool handler. ctx is the task's own lifetime
// (cancelled by tasks/cancel), not the creating request's. In synchronous
// fallback mode ctx is the original request context and tc is non-interactive.
type HandlerFor[In any] func(ctx context.Context, tc *Context, in In) (*protocol.CallToolResult, error)

// AddTool registers a task-capable tool. Clients that declare the tasks
// capability on the request receive a CreateTaskResult immediately and follow
// the task via tasks/get; clients without it get the tool executed
// synchronously (or rejected, per Options.Reject).
func AddTool[In any](t *Tasks, tool *protocol.Tool, handler HandlerFor[In]) {
	// Snapshot (deep copy) captured with the handler: in-flight tasks validate
	// against the schema as registered, immune to the tool being removed or
	// re-registered and to later mutation of the caller's schema map. Same
	// marshaled bytes → same cached compiled validator as the sync path.
	outputSchema := cloneSchema(tool.Name, tool.OutputSchema)
	server.AddTool(t.srv, tool, func(ctx context.Context, req *server.CallRequest, in In) (protocol.ToolResponse, any, error) {
		if _, ok := req.Extension(ID); !ok {
			if t.opts.Reject {
				return nil, nil, requireCapability(req.Request)
			}
			res, err := handler(ctx, &Context{}, in)
			return res, nil, err
		}
		task, err := t.start(t.ownerTag(ctx, req.Request), func(runCtx context.Context, tc *Context) (*protocol.CallToolResult, error) {
			res, err := handler(runCtx, tc, in)
			if err != nil {
				return res, err
			}
			// The task's final result is the tools/call result and must satisfy
			// the same outputSchema contract as the synchronous path; a
			// non-conforming result fails the task instead of persisting.
			if verr := server.ValidateToolOutput(outputSchema, res); verr != nil {
				return nil, protocol.Errorf(protocol.CodeInternal,
					"server bug: tool %q output does not conform to its outputSchema: %v", tool.Name, verr)
			}
			return res, nil
		})
		if err != nil {
			return nil, nil, err
		}
		return &protocol.ExtensionResult{R: task}, nil, nil
	})
}

// Context provides task facilities to a running handler.
type Context struct {
	taskID string
	t      *Tasks
	r      *run
	used   map[string]struct{} // input request keys already issued (runner-goroutine only)
}

// TaskID is empty in synchronous fallback mode.
func (c *Context) TaskID() string { return c.taskID }

// Interactive reports whether the handler runs as a task and can use
// RequireInput.
func (c *Context) Interactive() bool { return c.r != nil }

// RequireInput parks the task in input_required and blocks until the client
// has answered every request via tasks/update (partial submissions merge; the
// task stays input_required with the remaining subset outstanding) or ctx
// ends. In synchronous fallback mode it fails immediately; tools that cannot
// proceed without input should be installed with Options.Reject.
func (c *Context) RequireInput(ctx context.Context, requests protocol.InputRequests) (protocol.InputResponses, error) {
	if c.r == nil {
		return nil, errors.New("tasks: input unavailable (client did not declare the tasks capability)")
	}
	// The draft: each request key MUST be unique over the task's lifetime.
	if c.used == nil {
		c.used = make(map[string]struct{})
	}
	for key := range requests {
		if _, dup := c.used[key]; dup {
			return nil, fmt.Errorf("tasks: input request key %q reused within task %s", key, c.taskID)
		}
		c.used[key] = struct{}{}
	}
	outstanding := maps.Clone(requests)
	answers := protocol.InputResponses{}
	if err := c.t.setTask(c.taskID, func(d *DetailedTask) {
		d.Status = StatusInputRequired
		d.InputRequests = maps.Clone(outstanding)
	}); err != nil {
		return nil, err
	}
	for len(outstanding) > 0 {
		select {
		case resp := <-c.r.input:
			// Keys never issued or already answered are ignored, per spec.
			for key, v := range resp {
				if _, ok := outstanding[key]; ok {
					answers[key] = v
					delete(outstanding, key)
				}
			}
			remaining := maps.Clone(outstanding)
			status := StatusInputRequired
			if len(remaining) == 0 {
				remaining, status = nil, StatusWorking
			}
			if err := c.t.setTask(c.taskID, func(d *DetailedTask) {
				d.Status = status
				d.InputRequests = remaining
			}); err != nil {
				return nil, err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return answers, nil
}

// ownerTag is the ID prefix of the tasks the request's owner may access.
func (t *Tasks) ownerTag(ctx context.Context, req *server.Request) string {
	var owner string
	if t.opts.Owner != nil {
		owner = t.opts.Owner(ctx, req)
	}
	return ownerTagOf(owner)
}

// owns reports whether the request's owner may access the task.
func (t *Tasks) owns(ctx context.Context, req *server.Request, id string) bool {
	return strings.HasPrefix(id, t.ownerTag(ctx, req))
}

// task loads the task a request addresses. One the owner may not access is
// indistinguishable from one that does not exist.
func (t *Tasks) task(ctx context.Context, req *server.Request, id string) (*DetailedTask, error) {
	notFound := protocol.Errorf(protocol.CodeInvalidParams, "Failed to retrieve task: Task not found")
	if !t.owns(ctx, req, id) {
		return nil, notFound
	}
	d, err := t.store.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil, notFound
	}
	return d, err
}

// start durably creates the task, then launches the handler on its own
// context. The spec forbids returning a CreateTaskResult before the task is
// durable.
func (t *Tasks) start(ownerTag string, exec func(context.Context, *Context) (*protocol.CallToolResult, error)) (*Task, error) {
	id, err := newTaskID(ownerTag)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// ttlMs is anchored at createdAt per the draft, and a live task is never
	// evicted here, so while the task runs the honest advertisement is null
	// ("unlimited"). The terminal transition stamps the real value (setTask).
	d := &DetailedTask{Task: Task{
		TaskID:         id,
		Status:         StatusWorking,
		CreatedAt:      now,
		LastUpdatedAt:  now,
		TTLMs:          nil,
		PollIntervalMs: t.opts.PollIntervalMs,
	}}
	if err := t.store.Put(context.Background(), d); err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	r := &run{cancel: cancel, input: make(chan protocol.InputResponses, 1), done: make(chan struct{})}
	t.mu.Lock()
	t.live[id] = r
	t.mu.Unlock()

	go func() {
		defer cancel()
		res, err := exec(runCtx, &Context{taskID: id, t: t, r: r})
		t.finish(id, r, runCtx, res, err)
	}()

	cp := d.Task
	return &cp, nil
}

func (t *Tasks) finish(id string, r *run, runCtx context.Context, res *protocol.CallToolResult, err error) {
	t.mu.Lock()
	delete(t.live, id)
	t.mu.Unlock()
	close(r.done)

	serr := t.setTask(id, func(d *DetailedTask) {
		d.InputRequests = nil
		switch {
		case err == nil:
			complete(d, res)
		case runCtx.Err() != nil:
			d.Status = StatusCancelled
		default:
			var pe *protocol.Error
			if errors.As(err, &pe) {
				// Protocol-level failure.
				d.Status = StatusFailed
				d.Error = pe
				return
			}
			// Tool-level errors keep tool semantics: the task completes with
			// an isError result, exactly as the synchronous path returns them.
			complete(d, protocol.NewToolResultError(err.Error()))
		}
	})
	if serr != nil {
		slog.Error("tasks: failed to persist final task state", "taskId", id, "error", serr)
	}
}

func complete(d *DetailedTask, res *protocol.CallToolResult) {
	if res == nil {
		res = &protocol.CallToolResult{}
	}
	raw, err := protocol.MarshalResult(res)
	if err != nil {
		d.Status = StatusFailed
		d.Error = protocol.Errorf(protocol.CodeInternal, "failed to marshal task result: %v", err)
		return
	}
	d.Status = StatusCompleted
	d.Result = raw
}

// setTask applies mutate to the stored snapshot, bumps lastUpdatedAt, persists
// and publishes a notifications/tasks. The read-modify-write runs under
// t.stateMu so concurrent transitions (runner finish vs. orphan cancel) cannot
// overwrite each other, for any Store implementation. Terminal tasks are
// immutable; mutations against them (or evicted tasks) are dropped.
func (t *Tasks) setTask(id string, mutate func(*DetailedTask)) error {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()
	d, err := t.store.Get(context.Background(), id)
	if err != nil {
		return err
	}
	if d.Status.Terminal() {
		return nil
	}
	mutate(d)
	d.LastUpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if d.Status.Terminal() && d.TTLMs == nil {
		// Entering a terminal state: stamp the retention promise. ttlMs counts
		// from createdAt per the draft, so elapsed runtime plus the configured
		// retention makes createdAt+ttlMs equal terminal-time+retention.
		ttl := t.opts.TTL.Milliseconds()
		if created, perr := time.Parse(time.RFC3339Nano, d.CreatedAt); perr == nil {
			ttl += time.Since(created).Milliseconds()
		}
		d.TTLMs = &ttl
	}
	if err := t.store.Put(context.Background(), d); err != nil {
		return err
	}
	// The state is durably persisted; the notification is the spec's MAY
	// supplement, so an encoding failure is reported loudly, not propagated.
	if perr := t.srv.Publish(taskTopic(id), NotificationTasks, d); perr != nil {
		slog.Error("tasks: failed to publish task notification", "taskId", id, "error", perr)
	}
	return nil
}

func (t *Tasks) handleGet(ctx context.Context, req *server.Request) (protocol.Result, error) {
	if err := requireCapability(req); err != nil {
		return nil, err
	}
	var p GetParams
	if err := json.Unmarshal(req.RawParams(), &p); err != nil || p.TaskID == "" {
		return nil, protocol.Errorf(protocol.CodeInvalidParams, "invalid tasks/get params")
	}
	d, err := t.task(ctx, req, p.TaskID)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (t *Tasks) handleUpdate(ctx context.Context, req *server.Request) (protocol.Result, error) {
	if err := requireCapability(req); err != nil {
		return nil, err
	}
	var p UpdateParams
	if err := json.Unmarshal(req.RawParams(), &p); err != nil || p.TaskID == "" {
		return nil, protocol.Errorf(protocol.CodeInvalidParams, "invalid tasks/update params")
	}
	d, err := t.task(ctx, req, p.TaskID)
	if err != nil {
		return nil, err
	}
	// Responses for anything not currently outstanding are ignored per spec.
	if d.Status == StatusInputRequired {
		t.mu.Lock()
		r := t.live[p.TaskID]
		t.mu.Unlock()
		if r != nil {
			// Block until the runner takes the responses: acknowledging an
			// update and then dropping it would silently lose data. done
			// unblocks if the runner finishes meanwhile (no longer
			// outstanding — ignored per spec).
			select {
			case r.input <- p.InputResponses:
			case <-r.done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return &protocol.EmptyResult{}, nil
}

func (t *Tasks) handleCancel(ctx context.Context, req *server.Request) (protocol.Result, error) {
	if err := requireCapability(req); err != nil {
		return nil, err
	}
	var p CancelParams
	if err := json.Unmarshal(req.RawParams(), &p); err != nil || p.TaskID == "" {
		return nil, protocol.Errorf(protocol.CodeInvalidParams, "invalid tasks/cancel params")
	}
	d, err := t.task(ctx, req, p.TaskID)
	if err != nil {
		return nil, err
	}
	if !d.Status.Terminal() {
		t.mu.Lock()
		r := t.live[p.TaskID]
		t.mu.Unlock()
		if r != nil {
			r.cancel() // cooperative: the runner records the cancelled status
		} else {
			// Orphaned non-terminal task (e.g. lost runner): settle it directly.
			// A persistence failure here means the cancellation did not happen;
			// acknowledging it anyway would lie to the client.
			if err := t.setTask(p.TaskID, func(d *DetailedTask) {
				d.Status = StatusCancelled
				d.InputRequests = nil
			}); err != nil {
				return nil, err
			}
		}
	}
	return &protocol.EmptyResult{}, nil
}
