package tasks_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/ext/tasks"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/mem"
	"github.com/voocel/mcp-sdk-go/transport/streamhttp"
)

type jobIn struct {
	Name string `json:"name"`
}

type fixture struct {
	srv     *server.Server
	tasks   *tasks.Tasks
	release chan struct{} // gates the "slow" tool
}

func newFixture(t *testing.T, topts *tasks.Options) *fixture {
	t.Helper()
	f := &fixture{release: make(chan struct{})}
	f.srv = server.New(&server.Options{Impl: protocol.Implementation{Name: "task-srv", Version: "1"}})
	if topts == nil {
		topts = &tasks.Options{}
	}
	if topts.PollIntervalMs == 0 {
		topts.PollIntervalMs = 20
	}
	f.tasks = tasks.Install(f.srv, tasks.NewMemStore(), topts)

	tasks.AddTool(f.tasks, &protocol.Tool{Name: "job", Description: "does work"},
		func(ctx context.Context, tc *tasks.Context, in jobIn) (*protocol.CallToolResult, error) {
			return protocol.NewToolResultText("done: " + in.Name), nil
		})

	tasks.AddTool(f.tasks, &protocol.Tool{Name: "slow", Description: "waits for release"},
		func(ctx context.Context, tc *tasks.Context, _ struct{}) (*protocol.CallToolResult, error) {
			select {
			case <-f.release:
				return protocol.NewToolResultText("released"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})

	tasks.AddTool(f.tasks, &protocol.Tool{Name: "ask", Description: "asks for a name"},
		func(ctx context.Context, tc *tasks.Context, _ struct{}) (*protocol.CallToolResult, error) {
			if !tc.Interactive() {
				return nil, errors.New("needs interactivity")
			}
			responses, err := tc.RequireInput(ctx, protocol.InputRequests{
				"who": protocol.NewElicitFormRequest("Your name?", protocol.JSONSchema{
					"type":       "object",
					"properties": map[string]any{"name": map[string]any{"type": "string"}},
				}),
			})
			if err != nil {
				return nil, err
			}
			er, err := responses.Elicit("who")
			if err != nil {
				return nil, err
			}
			name, _ := er.Content["name"].(string)
			return protocol.NewToolResultText("hello " + name), nil
		})

	tasks.AddTool(f.tasks, &protocol.Tool{Name: "ask2", Description: "asks two things"},
		func(ctx context.Context, tc *tasks.Context, _ struct{}) (*protocol.CallToolResult, error) {
			schema := protocol.JSONSchema{"type": "object",
				"properties": map[string]any{"v": map[string]any{"type": "string"}}}
			responses, err := tc.RequireInput(ctx, protocol.InputRequests{
				"a": protocol.NewElicitFormRequest("A?", schema),
				"b": protocol.NewElicitFormRequest("B?", schema),
			})
			if err != nil {
				return nil, err
			}
			ea, _ := responses.Elicit("a")
			eb, _ := responses.Elicit("b")
			return protocol.NewToolResultText(ea.Content["v"].(string) + "+" + eb.Content["v"].(string)), nil
		})

	tasks.AddTool(f.tasks, &protocol.Tool{Name: "bizfail", Description: "business error"},
		func(ctx context.Context, tc *tasks.Context, _ struct{}) (*protocol.CallToolResult, error) {
			return nil, errors.New("boom")
		})

	outSchema := protocol.JSONSchema{
		"type":       "object",
		"properties": map[string]any{"n": map[string]any{"type": "number"}},
		"required":   []any{"n"},
	}
	tasks.AddTool(f.tasks, &protocol.Tool{Name: "goodout", OutputSchema: outSchema},
		func(ctx context.Context, tc *tasks.Context, _ struct{}) (*protocol.CallToolResult, error) {
			return &protocol.CallToolResult{StructuredContent: map[string]any{"n": 1.0}}, nil
		})
	tasks.AddTool(f.tasks, &protocol.Tool{Name: "badout", OutputSchema: outSchema},
		func(ctx context.Context, tc *tasks.Context, _ struct{}) (*protocol.CallToolResult, error) {
			return &protocol.CallToolResult{StructuredContent: map[string]any{"x": true}}, nil
		})
	tasks.AddTool(f.tasks, &protocol.Tool{Name: "slowbad", OutputSchema: outSchema},
		func(ctx context.Context, tc *tasks.Context, _ struct{}) (*protocol.CallToolResult, error) {
			select {
			case <-f.release:
				return &protocol.CallToolResult{StructuredContent: map[string]any{"x": true}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})

	tasks.AddTool(f.tasks, &protocol.Tool{Name: "protofail", Description: "protocol error"},
		func(ctx context.Context, tc *tasks.Context, _ struct{}) (*protocol.CallToolResult, error) {
			return nil, protocol.Errorf(protocol.CodeInternal, "wire-level failure")
		})

	return f
}

// waitStatus polls tasks/get until the task reaches the wanted status.
func waitStatus(t *testing.T, ctx context.Context, c *client.Client, taskID string, want tasks.Status) *tasks.DetailedTask {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		d, err := tasks.Get(ctx, c, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if d.Status == want {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("task never reached %s, last: %+v", want, d)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newClient(t *testing.T, f *fixture, withCapability bool) *client.Client {
	t.Helper()
	opts := &client.Options{Info: &protocol.Implementation{Name: "C", Version: "1"}}
	if withCapability {
		tasks.EnableClient(opts)
	}
	c := client.New(mem.New(f.srv), opts)
	t.Cleanup(func() { c.Close() })
	return c
}

// callAsTask invokes the tool and requires that the server created a task.
func callAsTask(t *testing.T, ctx context.Context, c *client.Client, name string, args map[string]any) *tasks.Task {
	t.Helper()
	_, err := c.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: args})
	task, ok := tasks.AsTask(err)
	if !ok {
		t.Fatalf("expected a task result, got err = %v", err)
	}
	if task.Status != tasks.StatusWorking || task.TaskID == "" || task.CreatedAt == "" {
		t.Fatalf("create task result: %+v", task)
	}
	// While the task runs it is never evicted, so the honest ttlMs is null
	// ("unlimited"); the retention window is stamped at the terminal
	// transition.
	if task.TTLMs != nil {
		t.Fatalf("working task ttlMs = %d, want null", *task.TTLMs)
	}
	return task
}

func TestDiscoverAdvertisesExtension(t *testing.T) {
	f := newFixture(t, nil)
	c := newClient(t, f, true)
	d, err := c.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Capabilities.Extensions[tasks.ID]; !ok {
		t.Fatalf("capabilities.extensions missing %s: %+v", tasks.ID, d.Capabilities)
	}
}

func TestTaskLifecycleCompleted(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, true)

	task := callAsTask(t, ctx, c, "job", map[string]any{"name": "alpha"})

	d, err := tasks.Await(ctx, c, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusCompleted {
		t.Fatalf("status = %s, want completed; %+v", d.Status, d)
	}
	// The terminal transition stamps the retention window, anchored at
	// createdAt per the draft.
	if d.TTLMs == nil || *d.TTLMs <= 0 {
		t.Fatalf("terminal task ttlMs = %v, want a positive value", d.TTLMs)
	}
	res, err := d.ToolResult()
	if err != nil {
		t.Fatal(err)
	}
	if res.Content[0].(protocol.TextContent).Text != "done: alpha" {
		t.Fatalf("result content: %+v", res.Content)
	}
}

func TestTaskInputRequired(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, true)

	task := callAsTask(t, ctx, c, "ask", nil)

	d, err := tasks.Await(ctx, c, task, func(ctx context.Context, reqs protocol.InputRequests) (protocol.InputResponses, error) {
		p, err := reqs["who"].Elicit()
		if err != nil {
			return nil, err
		}
		if p.Message != "Your name?" {
			return nil, errors.New("unexpected elicitation message " + p.Message)
		}
		r := protocol.InputResponses{}
		_ = r.Set("who", protocol.ElicitResult{
			Action:  protocol.ElicitActionAccept,
			Content: map[string]any{"name": "Ada"},
		})
		return r, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusCompleted {
		t.Fatalf("status = %s; %+v", d.Status, d)
	}
	res, err := d.ToolResult()
	if err != nil {
		t.Fatal(err)
	}
	if res.Content[0].(protocol.TextContent).Text != "hello Ada" {
		t.Fatalf("result content: %+v", res.Content)
	}
}

func TestTaskInputRequiredWithoutHandler(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, true)

	task := callAsTask(t, ctx, c, "ask", nil)

	// With no onInput, Await surfaces the input_required snapshot as-is.
	d, err := tasks.Await(ctx, c, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusInputRequired || len(d.InputRequests) != 1 {
		t.Fatalf("snapshot: %+v", d)
	}

	// Manual continuation: answer via Update, then poll to completion. Update
	// acknowledges the submission; the working/completed transition persists
	// asynchronously, so an immediate Await could still observe the stale
	// input_required snapshot.
	r := protocol.InputResponses{}
	_ = r.Set("who", protocol.ElicitResult{Action: protocol.ElicitActionAccept, Content: map[string]any{"name": "Bob"}})
	if err := tasks.Update(ctx, c, task.TaskID, r); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, ctx, c, task.TaskID, tasks.StatusCompleted)
}

func TestTaskPartialInputUpdate(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, true)

	task := callAsTask(t, ctx, c, "ask2", nil)
	d := waitStatus(t, ctx, c, task.TaskID, tasks.StatusInputRequired)
	if len(d.InputRequests) != 2 {
		t.Fatalf("outstanding = %d, want 2", len(d.InputRequests))
	}

	answer := func(key, v string) protocol.InputResponses {
		r := protocol.InputResponses{}
		_ = r.Set(key, protocol.ElicitResult{Action: protocol.ElicitActionAccept, Content: map[string]any{"v": v}})
		return r
	}

	// Partial submission: the task stays input_required with only the
	// unanswered key outstanding.
	if err := tasks.Update(ctx, c, task.TaskID, answer("a", "one")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		d, err := tasks.Get(ctx, c, task.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if d.Status == tasks.StatusInputRequired && len(d.InputRequests) == 1 {
			if _, ok := d.InputRequests["b"]; !ok {
				t.Fatalf("outstanding after partial update: %+v", d.InputRequests)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("partial update not reflected, last: %+v", d)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Completing the second key finishes the task with both answers merged.
	// Poll rather than Await: the completed transition is eventually
	// consistent after the update acknowledgment.
	if err := tasks.Update(ctx, c, task.TaskID, answer("b", "two")); err != nil {
		t.Fatal(err)
	}
	final := waitStatus(t, ctx, c, task.TaskID, tasks.StatusCompleted)
	res, err := final.ToolResult()
	if err != nil {
		t.Fatal(err)
	}
	if res.Content[0].(protocol.TextContent).Text != "one+two" {
		t.Fatalf("merged result: %+v", res.Content)
	}
}

func TestTaskErrorSemantics(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, true)

	// A business error completes the task with an isError tool result —
	// identical to what the synchronous path returns.
	task := callAsTask(t, ctx, c, "bizfail", nil)
	d, err := tasks.Await(ctx, c, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusCompleted {
		t.Fatalf("business error status = %s, want completed; %+v", d.Status, d)
	}
	res, err := d.ToolResult()
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || res.Content[0].(protocol.TextContent).Text != "boom" {
		t.Fatalf("business error result: %+v", res)
	}

	// A *protocol.Error fails the task.
	task = callAsTask(t, ctx, c, "protofail", nil)
	d, err = tasks.Await(ctx, c, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusFailed || d.Error == nil || d.Error.Code != protocol.CodeInternal {
		t.Fatalf("protocol error task: %+v", d)
	}
}

func TestTaskCancel(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, true)

	task := callAsTask(t, ctx, c, "slow", nil)
	if err := tasks.Cancel(ctx, c, task.TaskID); err != nil {
		t.Fatal(err)
	}
	d, err := tasks.Await(ctx, c, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusCancelled {
		t.Fatalf("status = %s, want cancelled; %+v", d.Status, d)
	}

	// Cancelling a terminal task is an idempotent ack.
	if err := tasks.Cancel(ctx, c, task.TaskID); err != nil {
		t.Fatal(err)
	}
}

func TestSyncFallbackWithoutCapability(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, false)

	res, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "job", Arguments: map[string]any{"name": "sync"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content[0].(protocol.TextContent).Text != "done: sync" {
		t.Fatalf("sync result: %+v", res.Content)
	}
}

func TestRejectWithoutCapability(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, &tasks.Options{Reject: true})
	c := newClient(t, f, false)

	_, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "job", Arguments: map[string]any{"name": "x"}})
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.CodeMissingClientCapability {
		t.Fatalf("err = %v, want -32021", err)
	}
	caps, ok := pe.MissingCapabilities()
	if !ok {
		t.Fatalf("no capability data: %v", pe)
	}
	if _, ok := caps.Extensions[tasks.ID]; !ok {
		t.Fatalf("required capabilities: %+v", caps)
	}
}

func TestTaskOutputSchemaEnforced(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, true)

	// Conforming structured output completes normally.
	task := callAsTask(t, ctx, c, "goodout", nil)
	d, err := tasks.Await(ctx, c, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusCompleted {
		t.Fatalf("goodout status = %s; %+v", d.Status, d)
	}

	// A non-conforming result must fail the task, not persist as completed:
	// the async path shares the sync path's outputSchema contract.
	task = callAsTask(t, ctx, c, "badout", nil)
	d, err = tasks.Await(ctx, c, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusFailed || d.Error == nil || d.Error.Code != protocol.CodeInternal {
		t.Fatalf("badout task: %+v", d)
	}

	// The synchronous fallback enforces the same contract via handleCallTool.
	cs := newClient(t, f, false)
	if _, err := cs.CallTool(ctx, &protocol.CallToolParams{Name: "goodout"}); err != nil {
		t.Fatalf("sync goodout: %v", err)
	}
	if _, err := cs.CallTool(ctx, &protocol.CallToolParams{Name: "badout"}); err == nil {
		t.Fatal("sync badout must be rejected")
	}

	// The validator is captured at registration: removing the tool while a
	// task is in flight must not skip validation of its final result.
	task = callAsTask(t, ctx, c, "slowbad", nil)
	f.srv.RemoveTools("slowbad")
	close(f.release)
	d = waitStatus(t, ctx, c, task.TaskID, tasks.StatusFailed)
	if d.Error == nil || d.Error.Code != protocol.CodeInternal {
		t.Fatalf("in-flight validation after removal: %+v", d)
	}
}

func TestTaskOutputSchemaSnapshot(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	mutable := protocol.JSONSchema{
		"type":       "object",
		"properties": map[string]any{"n": map[string]any{"type": "number"}},
		"required":   []any{"n"},
	}
	tasks.AddTool(f.tasks, &protocol.Tool{Name: "snap", OutputSchema: mutable},
		func(ctx context.Context, tc *tasks.Context, _ struct{}) (*protocol.CallToolResult, error) {
			return &protocol.CallToolResult{StructuredContent: map[string]any{"x": true}}, nil
		})
	// Mutating the caller's schema map after registration must not relax the
	// contract in-flight tasks are validated against.
	delete(mutable, "required")

	c := newClient(t, f, true)
	task := callAsTask(t, ctx, c, "snap", nil)
	d := waitStatus(t, ctx, c, task.TaskID, tasks.StatusFailed)
	if d.Error == nil || d.Error.Code != protocol.CodeInternal {
		t.Fatalf("mutated schema must not affect validation: %+v", d)
	}
}

func TestMemStoreDeepCopy(t *testing.T) {
	ctx := context.Background()
	s := tasks.NewMemStore()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	orig := &tasks.DetailedTask{
		Task: tasks.Task{TaskID: "iso", Status: tasks.StatusInputRequired, CreatedAt: now, LastUpdatedAt: now},
		InputRequests: protocol.InputRequests{
			"k": {Method: protocol.MethodElicitationCreate, Params: json.RawMessage(`{"a":1}`)},
		},
		Result: json.RawMessage(`{"r":1}`),
		Error:  &protocol.Error{Code: protocol.CodeInternal, Message: "e", Data: json.RawMessage(`{"d":1}`)},
	}
	orig.Meta.ServerInfo = &protocol.Implementation{Name: "srv", Version: "1"}
	orig.Meta.Extra = map[string]json.RawMessage{"m": json.RawMessage(`{"x":1}`)}
	if err := s.Put(ctx, orig); err != nil {
		t.Fatal(err)
	}

	// Mutating the object handed to Put must not reach the stored task, even
	// through nested RawMessage backing arrays or shared meta pointers.
	orig.InputRequests["k"].Params[2] = 'X'
	orig.Result[2] = 'X'
	orig.Error.Data[2] = 'X'
	orig.Meta.ServerInfo.Name = "mut"
	orig.Meta.Extra["m"][2] = 'X'
	got, err := s.Get(ctx, "iso")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.InputRequests["k"].Params) != `{"a":1}` || string(got.Result) != `{"r":1}` || string(got.Error.Data) != `{"d":1}` {
		t.Fatalf("stored task corrupted by Put-side mutation: %+v", got)
	}
	if got.Meta.ServerInfo.Name != "srv" || string(got.Meta.Extra["m"]) != `{"x":1}` {
		t.Fatalf("stored meta corrupted by Put-side mutation: %+v", got.Meta)
	}

	// Mutating a Get snapshot must not affect later Gets.
	got.InputRequests["k"].Params[2] = 'Y'
	got.Result[2] = 'Y'
	got.Error.Data[2] = 'Y'
	again, err := s.Get(ctx, "iso")
	if err != nil {
		t.Fatal(err)
	}
	if string(again.InputRequests["k"].Params) != `{"a":1}` || string(again.Result) != `{"r":1}` || string(again.Error.Data) != `{"d":1}` {
		t.Fatalf("stored task corrupted by Get-side mutation: %+v", again)
	}
}

// failingStore wraps MemStore to simulate persistence outages.
type failingStore struct {
	*tasks.MemStore
	failPut atomic.Bool
}

func (f *failingStore) Put(ctx context.Context, d *tasks.DetailedTask) error {
	if f.failPut.Load() {
		return errors.New("store down")
	}
	return f.MemStore.Put(ctx, d)
}

func TestOrphanCancelStoreFailure(t *testing.T) {
	ctx := context.Background()
	st := &failingStore{MemStore: tasks.NewMemStore()}
	srv := server.New(&server.Options{Impl: protocol.Implementation{Name: "s", Version: "1"}})
	tasks.Install(srv, st, nil)

	// Seed an orphaned working task: present in the store, no live runner.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := st.Put(ctx, &tasks.DetailedTask{
		Task: tasks.Task{TaskID: "orph", Status: tasks.StatusWorking, CreatedAt: now, LastUpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}

	opts := &client.Options{Info: &protocol.Implementation{Name: "C", Version: "1"}}
	tasks.EnableClient(opts)
	c := client.New(mem.New(srv), opts)
	defer c.Close()

	// A persistence failure must surface: acknowledging an unpersisted cancel
	// would lie to the client.
	st.failPut.Store(true)
	if err := tasks.Cancel(ctx, c, "orph"); err == nil {
		t.Fatal("cancel with a failing store must error")
	}
	d, err := tasks.Get(ctx, c, "orph")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusWorking {
		t.Fatalf("status = %s, want still working after failed cancel", d.Status)
	}

	// Once the store recovers, the orphan settles as cancelled.
	st.failPut.Store(false)
	if err := tasks.Cancel(ctx, c, "orph"); err != nil {
		t.Fatal(err)
	}
	d, err = tasks.Get(ctx, c, "orph")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusCancelled {
		t.Fatalf("status = %s, want cancelled", d.Status)
	}
}

func TestTaskMethodsRequireCapability(t *testing.T) {
	// The draft: servers MUST return MissingRequiredClientCapability for
	// non-declaring clients issuing tasks/get, tasks/update and tasks/cancel.
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, false)

	assert32021 := func(what string, err error) {
		t.Helper()
		var pe *protocol.Error
		if !errors.As(err, &pe) || pe.Code != protocol.CodeMissingClientCapability {
			t.Fatalf("%s err = %v, want -32021", what, err)
		}
	}
	_, err := tasks.Get(ctx, c, "any")
	assert32021("tasks/get", err)
	assert32021("tasks/update", tasks.Update(ctx, c, "any", protocol.InputResponses{}))
	assert32021("tasks/cancel", tasks.Cancel(ctx, c, "any"))
}

func TestTaskListenRequiresCapability(t *testing.T) {
	// Same MUST for task notifications requested via subscriptions/listen.
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, false)

	_, err := c.Listen(ctx, protocol.SubscriptionFilter{
		Extra: map[string]json.RawMessage{
			tasks.FilterTaskIDs: mustJSON(t, []string{"some-task"}),
		},
	})
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.CodeMissingClientCapability {
		t.Fatalf("listen err = %v, want -32021", err)
	}
}

func TestTasksOverStreamableHTTP(t *testing.T) {
	// End to end over real HTTP: the client must route tasks/* with the
	// Mcp-Name: taskId header (the draft's Streamable HTTP MUST), and the
	// server must reject tasks/* requests that lack it.
	ctx := context.Background()
	f := newFixture(t, nil)
	hs := httptest.NewServer(streamhttp.NewHandler(f.srv, nil))
	defer hs.Close()

	opts := &client.Options{Info: &protocol.Implementation{Name: "C", Version: "1"}}
	tasks.EnableClient(opts)
	c := client.New(streamhttp.New(hs.URL, nil), opts)
	defer c.Close()

	task := callAsTask(t, ctx, c, "job", map[string]any{"name": "http"})
	d, err := tasks.Await(ctx, c, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusCompleted {
		t.Fatalf("status = %s, want completed; %+v", d.Status, d)
	}

	// tasks/update over HTTP: the input_required rendezvous routes with the
	// same Mcp-Name header.
	task = callAsTask(t, ctx, c, "ask", nil)
	d, err = tasks.Await(ctx, c, task, func(ctx context.Context, reqs protocol.InputRequests) (protocol.InputResponses, error) {
		r := protocol.InputResponses{}
		_ = r.Set("who", protocol.ElicitResult{Action: protocol.ElicitActionAccept, Content: map[string]any{"name": "Net"}})
		return r, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusCompleted {
		t.Fatalf("ask over http: %+v", d)
	}

	// tasks/cancel over HTTP.
	task = callAsTask(t, ctx, c, "slow", nil)
	if err := tasks.Cancel(ctx, c, task.TaskID); err != nil {
		t.Fatal(err)
	}
	d, err = tasks.Await(ctx, c, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != tasks.StatusCancelled {
		t.Fatalf("cancel over http: %+v", d)
	}

	// Raw tasks/get without Mcp-Name → 400 with -32020.
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":99,"method":"tasks/get","params":{"taskId":%q,"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientCapabilities":{"extensions":{%q:{}}}}}}`,
		task.TaskID, protocol.Version, tasks.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hs.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", protocol.Version)
	req.Header.Set("Mcp-Method", tasks.MethodGet)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var msg protocol.Message
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		t.Fatal(err)
	}
	if msg.Error == nil || msg.Error.Code != protocol.CodeHeaderMismatch {
		t.Fatalf("error = %+v, want -32020", msg.Error)
	}
}

func TestUnknownTask(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, nil)
	c := newClient(t, f, true)

	_, err := tasks.Get(ctx, c, "does-not-exist")
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.CodeInvalidParams {
		t.Fatalf("err = %v, want -32602", err)
	}
}

func TestTaskNotifications(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f := newFixture(t, nil)
	c := newClient(t, f, true)

	task := callAsTask(t, ctx, c, "slow", nil)

	sub, err := c.Listen(ctx, protocol.SubscriptionFilter{
		Extra: map[string]json.RawMessage{
			tasks.FilterTaskIDs: mustJSON(t, []string{task.TaskID}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	close(f.release)

	select {
	case ev, ok := <-sub.Events():
		if !ok {
			t.Fatalf("subscription closed early: %v", sub.Err())
		}
		d, ok := tasks.TaskEvent(ev)
		if !ok {
			t.Fatalf("event: %+v", ev)
		}
		if d.TaskID != task.TaskID || d.Status != tasks.StatusCompleted {
			t.Fatalf("notification task: %+v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for notifications/tasks")
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
