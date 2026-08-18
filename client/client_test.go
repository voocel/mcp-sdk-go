package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport"
	"github.com/voocel/mcp-sdk-go/transport/mem"
)

type addIn struct {
	A float64 `json:"a" jsonschema:"required"`
	B float64 `json:"b" jsonschema:"required"`
}
type addOut struct {
	Sum float64 `json:"sum"`
}

func newPair(t *testing.T, copts *client.Options) (*server.Server, *client.Client) {
	t.Helper()
	s := server.New(&server.Options{
		Impl: protocol.Implementation{Name: "S", Version: "1"},
	})
	server.AddTool(s, &protocol.Tool{Name: "add", Description: "add two numbers"},
		func(ctx context.Context, req *server.CallRequest, in addIn) (protocol.ToolResponse, addOut, error) {
			_ = req.ReportProgress(ctx, 1, 1, "adding")
			return nil, addOut{Sum: in.A + in.B}, nil
		})
	if copts == nil {
		copts = &client.Options{}
	}
	if copts.Info == nil {
		copts.Info = &protocol.Implementation{Name: "C", Version: "1"}
	}
	c := client.New(mem.New(s), copts)
	t.Cleanup(func() { c.Close() })
	return s, c
}

func TestEndToEnd(t *testing.T) {
	ctx := context.Background()
	var progress []protocol.ProgressParams
	_, c := newPair(t, &client.Options{
		OnProgress: func(p *protocol.ProgressParams) { progress = append(progress, *p) },
	})

	disc, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if disc.SupportedVersions[0] != protocol.Version || disc.Capabilities.Tools == nil {
		t.Fatalf("discover: %+v", disc)
	}
	if disc.Meta.ServerInfo == nil || disc.Meta.ServerInfo.Name != "S" {
		t.Fatalf("serverInfo: %+v", disc.Meta)
	}

	list, err := c.ListTools(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "add" {
		t.Fatalf("tools: %+v", list.Tools)
	}

	res, err := c.CallTool(ctx, &protocol.CallToolParams{
		Name: "add", Arguments: map[string]any{"a": 2, "b": 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out addOut
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil || out.Sum != 5 {
		t.Fatalf("structuredContent: %v", res.StructuredContent)
	}
	if len(progress) != 1 || progress[0].Message != "adding" {
		t.Fatalf("progress: %+v", progress)
	}
}

func TestErrorRoundTrip(t *testing.T) {
	_, c := newPair(t, nil)
	_, err := c.CallTool(context.Background(), &protocol.CallToolParams{Name: "nope"})
	var pe *protocol.Error
	if !errors.As(err, &pe) {
		t.Fatalf("expected *protocol.Error, got %T: %v", err, err)
	}
	if pe.Code != protocol.CodeInvalidParams || !strings.Contains(pe.Message, "Unknown tool") {
		t.Fatalf("error: %+v", pe)
	}
}

func newMRTRServer() *server.Server {
	s := server.New(nil)
	server.AddTool(s, &protocol.Tool{Name: "login"},
		func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
			if len(req.Params.InputResponses) == 0 {
				return protocol.RequireInput(protocol.InputRequests{
					"who": protocol.NewElicitFormRequest("name?", protocol.JSONSchema{"type": "object"}),
				}, "s1"), nil, nil
			}
			if req.Params.RequestState != "s1" {
				return nil, nil, protocol.Errorf(protocol.CodeInvalidParams, "state lost")
			}
			r, err := req.Params.InputResponses.Elicit("who")
			if err != nil {
				return nil, nil, err
			}
			return protocol.NewToolResultText("hi " + r.Content["name"].(string)), nil, nil
		})
	return s
}

func TestMRTRAutoFulfillment(t *testing.T) {
	s := newMRTRServer()
	c := client.New(mem.New(s), &client.Options{
		Elicitor: func(ctx context.Context, p *protocol.ElicitParams) (*protocol.ElicitResult, error) {
			return &protocol.ElicitResult{
				Action:  protocol.ElicitActionAccept,
				Content: map[string]any{"name": "octocat"},
			}, nil
		},
	})
	res, err := c.CallTool(context.Background(), &protocol.CallToolParams{Name: "login"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content[0].(protocol.TextContent).Text != "hi octocat" {
		t.Fatalf("content: %+v", res.Content)
	}
}

func TestMRTRManualContinuation(t *testing.T) {
	s := newMRTRServer()
	c := client.New(mem.New(s), &client.Options{
		NoAutoInput: true,
		// Elicitor still declares the capability; NoAutoInput just disables
		// the loop.
		Elicitor: func(ctx context.Context, p *protocol.ElicitParams) (*protocol.ElicitResult, error) {
			t.Fatal("Elicitor must not be called with NoAutoInput")
			return nil, nil
		},
	})
	params := &protocol.CallToolParams{Name: "login"}
	_, err := c.CallTool(context.Background(), params)
	var ire *client.InputRequiredError
	if !errors.As(err, &ire) {
		t.Fatalf("expected InputRequiredError, got %v", err)
	}
	if ire.State != "s1" || len(ire.Requests) != 1 {
		t.Fatalf("interim: %+v", ire)
	}

	// Manual continuation.
	responses := protocol.InputResponses{}
	_ = responses.Set("who", protocol.ElicitResult{Action: protocol.ElicitActionAccept, Content: map[string]any{"name": "manual"}})
	params.InputResponses = responses
	params.RequestState = ire.State
	res, err := c.CallTool(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content[0].(protocol.TextContent).Text != "hi manual" {
		t.Fatalf("content: %+v", res.Content)
	}
}

func TestMRTRWithoutHandlerFails(t *testing.T) {
	s := newMRTRServer()
	c := client.New(mem.New(s), nil) // no Elicitor => no elicitation capability
	_, err := c.CallTool(context.Background(), &protocol.CallToolParams{Name: "login"})
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.CodeMissingClientCapability {
		t.Fatalf("expected -32021 from server, got %v", err)
	}
}

func TestIterators(t *testing.T) {
	s := server.New(&server.Options{PageSize: 2})
	for _, name := range []string{"t1", "t2", "t3", "t4", "t5"} {
		server.AddTool(s, &protocol.Tool{Name: name},
			func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
				return protocol.NewToolResultText("ok"), nil, nil
			})
	}
	c := client.New(mem.New(s), nil)
	var names []string
	for tool, err := range c.Tools(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "t1,t2,t3,t4,t5" {
		t.Fatalf("names: %v", names)
	}
}

func TestSubscription(t *testing.T) {
	s := server.New(nil)
	server.AddTool(s, &protocol.Tool{Name: "seed"},
		func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
			return protocol.NewToolResultText("ok"), nil, nil
		})
	s.AddResource(&protocol.Resource{URI: "info://w", Name: "w"},
		func(ctx context.Context, req *server.ResourceRequest) (protocol.ResourceResponse, error) {
			return protocol.NewReadResourceResult(protocol.NewTextResourceContents("info://w", "v")), nil
		})
	c := client.New(mem.New(s), nil)

	sub, err := c.Listen(context.Background(), protocol.SubscriptionFilter{
		ToolsListChanged:      true,
		ResourceSubscriptions: []string{"info://w"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Ack().ToolsListChanged {
		t.Fatalf("ack: %+v", sub.Ack())
	}

	server.AddTool(s, &protocol.Tool{Name: "late"},
		func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
			return protocol.NewToolResultText("ok"), nil, nil
		})
	ev := recvEvent(t, sub)
	if ev.Method != protocol.NotificationToolsListChanged {
		t.Fatalf("event: %+v", ev)
	}

	s.ResourceUpdated("info://w")
	ev = recvEvent(t, sub)
	if uri, ok := ev.ResourceUpdated(); !ok || uri != "info://w" {
		t.Fatalf("event: %+v", ev)
	}

	if err := sub.Close(); err != nil {
		t.Fatal(err)
	}
	for range sub.Events() {
		// drain until close
	}
	if sub.Err() != nil {
		t.Fatalf("Err after Close: %v", sub.Err())
	}
}

func recvEvent(t *testing.T, sub *client.Subscription) client.Event {
	t.Helper()
	select {
	case ev, ok := <-sub.Events():
		if !ok {
			t.Fatalf("events closed early: %v", sub.Err())
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return client.Event{}
	}
}

// invalidToolHandler fakes a server that lists a tool with an invalid
// x-mcp-header binding; the client must exclude it from tools/list.
type invalidToolHandler struct{}

func (invalidToolHandler) Handle(ctx context.Context, msg *protocol.Message, emit func(*protocol.Message) error) {
	res := &protocol.ListToolsResult{Tools: []*protocol.Tool{
		{Name: "good", InputSchema: protocol.JSONSchema{"type": "object"}},
		{Name: "evil", InputSchema: protocol.JSONSchema{
			"type": "object",
			"properties": map[string]any{
				"n": map[string]any{"type": "number", "x-mcp-header": "N"},
			},
		}},
	}}
	out, _ := protocol.NewResponse(msg.ID, res)
	_ = emit(out)
}

func TestClientExcludesInvalidTools(t *testing.T) {
	c := client.New(mem.New(invalidToolHandler{}), nil)
	list, err := c.ListTools(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "good" {
		t.Fatalf("tools: %+v", list.Tools)
	}
}

// legacyResultHandler fakes a server whose results lack resultType, i.e. one
// implementing an earlier protocol revision.
type legacyResultHandler struct{}

func (legacyResultHandler) Handle(ctx context.Context, msg *protocol.Message, emit func(*protocol.Message) error) {
	_ = emit(&protocol.Message{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      msg.ID,
		Result:  json.RawMessage(`{"content":[]}`),
	})
}

func TestClientTreatsMissingResultTypeAsComplete(t *testing.T) {
	// Spec (2026-07-28, key changes #8): clients MUST treat results from
	// earlier-protocol servers that omit resultType as "complete".
	c := client.New(mem.New(legacyResultHandler{}), nil)
	res, err := c.CallTool(context.Background(), &protocol.CallToolParams{Name: "x"})
	if err != nil {
		t.Fatalf("missing resultType must be accepted as complete: %v", err)
	}
	if len(res.Content) != 0 {
		t.Fatalf("content = %+v", res.Content)
	}
}

var _ transport.Transport = (*mem.Transport)(nil)
