package server_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
)

func newMeta() protocol.RequestMeta {
	return protocol.RequestMeta{ProtocolVersion: protocol.Version}
}

func metaWithElicitation() protocol.RequestMeta {
	m := newMeta()
	m.ClientCapabilities = protocol.ClientCapabilities{
		Elicitation: &protocol.ElicitationCapability{Form: &struct{}{}},
	}
	return m
}

// do sends one request through Handle and separates notifications from the
// final message.
func do(t *testing.T, s *server.Server, method string, params any) (final *protocol.Message, notes []*protocol.Message) {
	t.Helper()
	msg, err := protocol.NewRequest(protocol.StringID("req-1"), method, params)
	if err != nil {
		t.Fatal(err)
	}
	var all []*protocol.Message
	s.Handle(context.Background(), msg, func(m *protocol.Message) error {
		all = append(all, m)
		return nil
	})
	if len(all) == 0 {
		t.Fatalf("%s: no messages emitted", method)
	}
	return all[len(all)-1], all[:len(all)-1]
}

func decodeResult[T any](t *testing.T, msg *protocol.Message) *T {
	t.Helper()
	if msg.Kind() != protocol.KindResponse {
		t.Fatalf("expected response, got error: %+v", msg.Error)
	}
	var v T
	if err := json.Unmarshal(msg.Result, &v); err != nil {
		t.Fatal(err)
	}
	return &v
}

type greetIn struct {
	Name string `json:"name" jsonschema:"required"`
}
type greetOut struct {
	Greeting string `json:"greeting"`
}

func newTestServer(_ *testing.T) *server.Server {
	s := server.New(&server.Options{
		Impl:         protocol.Implementation{Name: "TestServer", Version: "1.0.0"},
		Instructions: "test instructions",
	})
	server.AddTool(s, &protocol.Tool{Name: "greet", Description: "greet"},
		func(ctx context.Context, req *server.CallRequest, in greetIn) (protocol.ToolResponse, greetOut, error) {
			return nil, greetOut{Greeting: "Hello, " + in.Name}, nil
		})
	return s
}

func TestDiscover(t *testing.T) {
	s := newTestServer(t)
	final, _ := do(t, s, protocol.MethodDiscover, protocol.DiscoverParams{Meta: newMeta()})
	res := decodeResult[protocol.DiscoverResult](t, final)
	if len(res.SupportedVersions) != 1 || res.SupportedVersions[0] != protocol.Version {
		t.Fatalf("supportedVersions = %v", res.SupportedVersions)
	}
	if res.Capabilities.Tools == nil || !res.Capabilities.Tools.ListChanged {
		t.Fatalf("tools capability not derived: %+v", res.Capabilities)
	}
	if res.Capabilities.Prompts != nil {
		t.Fatal("prompts capability advertised without prompts")
	}
	if res.Meta.ServerInfo == nil || res.Meta.ServerInfo.Name != "TestServer" {
		t.Fatalf("serverInfo not stamped: %+v", res.Meta)
	}
	if res.CacheScope != protocol.CacheScopePrivate {
		t.Fatalf("cacheScope default = %q", res.CacheScope)
	}
	if res.Instructions != "test instructions" {
		t.Fatalf("instructions = %q", res.Instructions)
	}
}

func TestMetaValidation(t *testing.T) {
	s := newTestServer(t)

	// Missing _meta entirely.
	final, _ := do(t, s, protocol.MethodToolsList, map[string]any{})
	if final.Kind() != protocol.KindError || final.Error.Code != protocol.CodeInvalidParams {
		t.Fatalf("missing _meta: %+v", final.Error)
	}

	// Unsupported version.
	final, _ = do(t, s, protocol.MethodToolsList, map[string]any{
		"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion":    "1900-01-01",
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		},
	})
	if final.Error == nil || final.Error.Code != protocol.CodeUnsupportedProtocolVersion {
		t.Fatalf("wrong version: %+v", final.Error)
	}
	data, ok := final.Error.UnsupportedVersion()
	if !ok || data.Requested != "1900-01-01" || data.Supported[0] != protocol.Version {
		t.Fatalf("version error data: %+v", data)
	}
}

func TestDiscoverAnswersAcrossVersions(t *testing.T) {
	// A caller on another revision must still learn what this server speaks.
	s := newTestServer(t)
	final, _ := do(t, s, protocol.MethodDiscover, map[string]any{
		"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion":    "2025-11-25",
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		},
	})
	res := decodeResult[protocol.DiscoverResult](t, final)
	if len(res.SupportedVersions) != 1 || res.SupportedVersions[0] != protocol.Version {
		t.Fatalf("supportedVersions = %v", res.SupportedVersions)
	}
}

// TestConcurrentLists guards the featureSet invariant that reads are pure:
// list handlers iterate under a read lock only. Most valuable under -race.
func TestConcurrentLists(t *testing.T) {
	s := newTestServer(t)
	for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
		s.AddTool(&protocol.Tool{Name: name},
			func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
				return protocol.NewToolResultText("ok"), nil
			})
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 25 {
				msg, err := protocol.NewRequest(protocol.StringID("concurrent"),
					protocol.MethodToolsList, protocol.ListToolsParams{Meta: newMeta()})
				if err != nil {
					t.Error(err)
					return
				}
				var last *protocol.Message
				s.Handle(context.Background(), msg, func(m *protocol.Message) error {
					last = m
					return nil
				})
				var res protocol.ListToolsResult
				if err := json.Unmarshal(last.Result, &res); err != nil {
					t.Error(err)
					return
				}
				names := make([]string, len(res.Tools))
				for i, tool := range res.Tools {
					names[i] = tool.Name
				}
				if len(names) != 5 || !slices.IsSorted(names) {
					t.Errorf("tools = %v, want 5 sorted names", names)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestUnknownMethod(t *testing.T) {
	s := newTestServer(t)
	final, _ := do(t, s, "bogus/method", protocol.DiscoverParams{Meta: newMeta()})
	if final.Error == nil || final.Error.Code != protocol.CodeMethodNotFound {
		t.Fatalf("unknown method: %+v", final.Error)
	}
	if final.Error.HTTPStatus() != 404 {
		t.Fatalf("HTTPStatus = %d", final.Error.HTTPStatus())
	}
}

func TestToolListAndCall(t *testing.T) {
	s := newTestServer(t)

	final, _ := do(t, s, protocol.MethodToolsList, protocol.ListToolsParams{Meta: newMeta()})
	list := decodeResult[protocol.ListToolsResult](t, final)
	if len(list.Tools) != 1 || list.Tools[0].Name != "greet" {
		t.Fatalf("tools = %+v", list.Tools)
	}
	if list.Tools[0].InputSchema == nil {
		t.Fatal("input schema not inferred")
	}
	if list.CacheScope == "" {
		t.Fatal("cacheScope missing on tools/list")
	}

	final, _ = do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{
		Meta: newMeta(), Name: "greet", Arguments: map[string]any{"name": "Go"},
	})
	res := decodeResult[protocol.CallToolResult](t, final)
	var out greetOut
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil || out.Greeting != "Hello, Go" {
		t.Fatalf("structuredContent = %v (%v)", res.StructuredContent, err)
	}
	if len(res.Content) == 0 {
		t.Fatal("content mirror missing")
	}

	// Invalid arguments are rejected before the handler runs.
	final, _ = do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{
		Meta: newMeta(), Name: "greet", Arguments: map[string]any{"name": 42},
	})
	if final.Error == nil || final.Error.Code != protocol.CodeInvalidParams {
		t.Fatalf("invalid args: %+v", final.Error)
	}

	// Unknown tool.
	final, _ = do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{
		Meta: newMeta(), Name: "nope",
	})
	if final.Error == nil || final.Error.Code != protocol.CodeInvalidParams ||
		!strings.Contains(final.Error.Message, "Unknown tool") {
		t.Fatalf("unknown tool: %+v", final.Error)
	}
}

func TestToolError(t *testing.T) {
	s := newTestServer(t)
	server.AddTool(s, &protocol.Tool{Name: "fail"},
		func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
			return nil, nil, context.DeadlineExceeded
		})
	final, _ := do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{Meta: newMeta(), Name: "fail"})
	res := decodeResult[protocol.CallToolResult](t, final)
	if !res.IsError {
		t.Fatal("expected isError result")
	}

	server.AddTool(s, &protocol.Tool{Name: "protoerr"},
		func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
			return nil, nil, protocol.Errorf(protocol.CodeInvalidParams, "bad juju")
		})
	final, _ = do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{Meta: newMeta(), Name: "protoerr"})
	if final.Error == nil || final.Error.Code != protocol.CodeInvalidParams {
		t.Fatalf("protocol error passthrough: %+v", final.Error)
	}
}

func TestProgress(t *testing.T) {
	s := newTestServer(t)
	server.AddTool(s, &protocol.Tool{Name: "slow"},
		func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
			_ = req.ReportProgress(ctx, 0.5, 1, "halfway")
			return protocol.NewToolResultText("done"), nil, nil
		})
	meta := newMeta()
	meta.ProgressToken = protocol.StringID("tok")
	final, notes := do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{Meta: meta, Name: "slow"})
	if final.Kind() != protocol.KindResponse {
		t.Fatalf("call failed: %+v", final.Error)
	}
	if len(notes) != 1 || notes[0].Method != protocol.NotificationProgress {
		t.Fatalf("expected 1 progress notification, got %d", len(notes))
	}
	var p protocol.ProgressParams
	if err := json.Unmarshal(notes[0].Params, &p); err != nil {
		t.Fatal(err)
	}
	if p.ProgressToken != protocol.StringID("tok") || p.Progress != 0.5 {
		t.Fatalf("progress params: %+v", p)
	}

	// Without a token, ReportProgress is a no-op.
	_, notes = do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{Meta: newMeta(), Name: "slow"})
	if len(notes) != 0 {
		t.Fatalf("expected no notifications, got %d", len(notes))
	}
}

func TestMRTR(t *testing.T) {
	s := newTestServer(t)
	server.AddTool(s, &protocol.Tool{Name: "login"},
		func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
			if len(req.Params.InputResponses) == 0 {
				return protocol.RequireInput(protocol.InputRequests{
					"user": protocol.NewElicitFormRequest("Who are you?", protocol.JSONSchema{"type": "object"}),
				}, "state-1"), nil, nil
			}
			res, err := req.Params.InputResponses.Elicit("user")
			if err != nil {
				return nil, nil, err
			}
			return protocol.NewToolResultText("hi " + res.Content["name"].(string)), nil, nil
		})

	// Client without elicitation capability → -32021.
	final, _ := do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{Meta: newMeta(), Name: "login"})
	if final.Error == nil || final.Error.Code != protocol.CodeMissingClientCapability {
		t.Fatalf("missing capability: %+v", final.Error)
	}

	// With capability → input_required interim result.
	final, _ = do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{Meta: metaWithElicitation(), Name: "login"})
	rt, err := protocol.PeekResultType(final.Result)
	if err != nil || rt != protocol.ResultTypeInputRequired {
		t.Fatalf("resultType = %q, %v", rt, err)
	}
	interim := decodeResult[protocol.InputRequired](t, final)
	if interim.State != "state-1" {
		t.Fatalf("state = %q", interim.State)
	}

	// Retry with responses → final result.
	responses := protocol.InputResponses{}
	_ = responses.Set("user", protocol.ElicitResult{Action: protocol.ElicitActionAccept, Content: map[string]any{"name": "octocat"}})
	final, _ = do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{
		Meta: metaWithElicitation(), Name: "login",
		InputResponses: responses, RequestState: "state-1",
	})
	res := decodeResult[protocol.CallToolResult](t, final)
	if res.Content[0].(protocol.TextContent).Text != "hi octocat" {
		t.Fatalf("final content: %+v", res.Content)
	}
}

func TestResources(t *testing.T) {
	s := newTestServer(t)
	s.AddResource(&protocol.Resource{URI: "info://x", Name: "x"},
		func(ctx context.Context, req *server.ResourceRequest) (protocol.ResourceResponse, error) {
			return protocol.NewReadResourceResult(protocol.NewTextResourceContents("info://x", "hello")), nil
		})
	s.AddResourceTemplate(&protocol.ResourceTemplate{URITemplate: "log://app/{date}", Name: "logs"},
		func(ctx context.Context, req *server.ResourceRequest) (protocol.ResourceResponse, error) {
			return protocol.NewReadResourceResult(
				protocol.NewTextResourceContents(req.Params.URI, "log for "+req.TemplateVars()["date"])), nil
		})

	final, _ := do(t, s, protocol.MethodResourcesRead, protocol.ReadResourceParams{Meta: newMeta(), URI: "info://x"})
	res := decodeResult[protocol.ReadResourceResult](t, final)
	if res.Contents[0].Text != "hello" {
		t.Fatalf("contents: %+v", res.Contents)
	}
	if res.CacheScope == "" {
		t.Fatal("cacheScope missing on resources/read")
	}

	final, _ = do(t, s, protocol.MethodResourcesRead, protocol.ReadResourceParams{Meta: newMeta(), URI: "log://app/2026-07-30"})
	res = decodeResult[protocol.ReadResourceResult](t, final)
	if res.Contents[0].Text != "log for 2026-07-30" {
		t.Fatalf("template read: %+v", res.Contents)
	}

	final, _ = do(t, s, protocol.MethodResourcesRead, protocol.ReadResourceParams{Meta: newMeta(), URI: "info://missing"})
	if final.Error == nil || final.Error.Code != protocol.CodeInvalidParams {
		t.Fatalf("not found: %+v", final.Error)
	}
	var data map[string]string
	_ = json.Unmarshal(final.Error.Data, &data)
	if data["uri"] != "info://missing" {
		t.Fatalf("error data: %+v", data)
	}
}

func TestPromptRequiredArguments(t *testing.T) {
	s := newTestServer(t)
	s.AddPrompt(&protocol.Prompt{
		Name:      "review",
		Arguments: []protocol.PromptArgument{{Name: "code", Required: true}},
	}, func(ctx context.Context, req *server.PromptRequest) (protocol.PromptResponse, error) {
		return protocol.NewGetPromptResult("r",
			protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(req.Params.Arguments["code"]))), nil
	})

	final, _ := do(t, s, protocol.MethodPromptsGet, protocol.GetPromptParams{Meta: newMeta(), Name: "review"})
	if final.Error == nil || final.Error.Code != protocol.CodeInvalidParams {
		t.Fatalf("missing required arg: %+v", final.Error)
	}

	final, _ = do(t, s, protocol.MethodPromptsGet, protocol.GetPromptParams{
		Meta: newMeta(), Name: "review", Arguments: map[string]string{"code": "x=1"},
	})
	res := decodeResult[protocol.GetPromptResult](t, final)
	if len(res.Messages) != 1 {
		t.Fatalf("messages: %+v", res.Messages)
	}
}

func TestPagination(t *testing.T) {
	s := server.New(&server.Options{PageSize: 2})
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		server.AddTool(s, &protocol.Tool{Name: name},
			func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
				return protocol.NewToolResultText("ok"), nil, nil
			})
	}
	var names []string
	cursor := ""
	for range 10 {
		final, _ := do(t, s, protocol.MethodToolsList, protocol.ListToolsParams{Meta: newMeta(), Cursor: cursor})
		res := decodeResult[protocol.ListToolsResult](t, final)
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
		}
		if res.NextCursor == nil {
			break
		}
		cursor = *res.NextCursor
	}
	if strings.Join(names, "") != "abcde" {
		t.Fatalf("paged names = %v", names)
	}

	final, _ := do(t, s, protocol.MethodToolsList, protocol.ListToolsParams{Meta: newMeta(), Cursor: "garbage!!"})
	if final.Error == nil || final.Error.Code != protocol.CodeInvalidParams {
		t.Fatalf("invalid cursor: %+v", final.Error)
	}
}

func TestListen(t *testing.T) {
	s := newTestServer(t)
	// The honored subset is derived from registrations; a resource must exist
	// for resource subscriptions to be honored.
	s.AddResource(&protocol.Resource{URI: "info://watch", Name: "watch"},
		func(ctx context.Context, req *server.ResourceRequest) (protocol.ResourceResponse, error) {
			return protocol.NewReadResourceResult(protocol.NewTextResourceContents("info://watch", "v")), nil
		})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	msg, err := protocol.NewRequest(protocol.IntID(1), protocol.MethodSubscriptionsListen, protocol.ListenParams{
		Meta: newMeta(),
		Notifications: protocol.SubscriptionFilter{
			ToolsListChanged:      true,
			ResourceSubscriptions: []string{"info://watch"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs := make(chan *protocol.Message, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Handle(ctx, msg, func(m *protocol.Message) error {
			msgs <- m
			return nil
		})
	}()

	// 1. Ack must be first, carrying the honored subset + subscription ID.
	ack := recvMsg(t, msgs)
	if ack.Method != protocol.NotificationSubscriptionsAcknowledged {
		t.Fatalf("first message = %q", ack.Method)
	}
	var ackParams protocol.AckParams
	if err := json.Unmarshal(ack.Params, &ackParams); err != nil {
		t.Fatal(err)
	}
	if ackParams.Meta.SubscriptionID != protocol.IntID(1) || !ackParams.Notifications.ToolsListChanged {
		t.Fatalf("ack: %+v", ackParams)
	}

	// 2. Registering a tool triggers tools/list_changed with the sub ID.
	server.AddTool(s, &protocol.Tool{Name: "late"},
		func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
			return protocol.NewToolResultText("ok"), nil, nil
		})
	note := recvMsg(t, msgs)
	if note.Method != protocol.NotificationToolsListChanged {
		t.Fatalf("notification = %q", note.Method)
	}
	var np struct {
		Meta protocol.NotificationMeta `json:"_meta"`
	}
	if err := json.Unmarshal(note.Params, &np); err != nil {
		t.Fatal(err)
	}
	if np.Meta.SubscriptionID != protocol.IntID(1) {
		t.Fatalf("subscriptionId missing: %s", note.Params)
	}

	// 3. Resource update for a subscribed URI.
	s.ResourceUpdated("info://watch")
	note = recvMsg(t, msgs)
	if note.Method != protocol.NotificationResourcesUpdated {
		t.Fatalf("notification = %q", note.Method)
	}

	// 4. Unsubscribed events are not delivered (prompts not in filter).
	s.AddPrompt(&protocol.Prompt{Name: "p"}, func(ctx context.Context, req *server.PromptRequest) (protocol.PromptResponse, error) {
		return protocol.NewGetPromptResult("d"), nil
	})

	// 5. Cancel → graceful empty complete result with subscription ID.
	cancel()
	final := recvMsg(t, msgs)
	if final.Kind() != protocol.KindResponse {
		t.Fatalf("expected final response, got %+v", final)
	}
	res := decodeResult[protocol.ListenResult](t, final)
	if res.Meta.SubscriptionID != protocol.IntID(1) {
		t.Fatalf("final result meta: %+v", res.Meta)
	}
	<-done
}

func TestCapabilityGating(t *testing.T) {
	s := newTestServer(t) // registers tools only

	// A method behind an unadvertised capability is treated as not found.
	final, _ := do(t, s, protocol.MethodPromptsList, protocol.ListPromptsParams{Meta: newMeta()})
	if final.Error == nil || final.Error.Code != protocol.CodeMethodNotFound {
		t.Fatalf("prompts/list without prompts: %+v", final.Error)
	}
	final, _ = do(t, s, protocol.MethodResourcesRead, protocol.ReadResourceParams{Meta: newMeta(), URI: "x://y"})
	if final.Error == nil || final.Error.Code != protocol.CodeMethodNotFound {
		t.Fatalf("resources/read without resources: %+v", final.Error)
	}

	// The listen ack omits unsupported types — the spec's example verbatim:
	// promptsListChanged requested from a server with no prompts.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msg, err := protocol.NewRequest(protocol.IntID(7), protocol.MethodSubscriptionsListen, protocol.ListenParams{
		Meta: newMeta(),
		Notifications: protocol.SubscriptionFilter{
			ToolsListChanged:   true,
			PromptsListChanged: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := make(chan *protocol.Message, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Handle(ctx, msg, func(m *protocol.Message) error { msgs <- m; return nil })
	}()
	ack := recvMsg(t, msgs)
	var ap protocol.AckParams
	if err := json.Unmarshal(ack.Params, &ap); err != nil {
		t.Fatal(err)
	}
	if !ap.Notifications.ToolsListChanged || ap.Notifications.PromptsListChanged {
		t.Fatalf("ack must include tools and omit prompts: %+v", ap.Notifications)
	}
	cancel()
	<-done
}

func TestEmptyListAfterRemoval(t *testing.T) {
	// A declared capability is sticky: "This set MAY be empty and MAY change
	// over time" — removing the last tool yields an empty list, not -32601.
	s := newTestServer(t)
	s.RemoveTools("greet")
	final, _ := do(t, s, protocol.MethodToolsList, protocol.ListToolsParams{Meta: newMeta()})
	res := decodeResult[protocol.ListToolsResult](t, final)
	if len(res.Tools) != 0 {
		t.Fatalf("tools = %+v, want empty list", res.Tools)
	}
	// Never-registered features still gate to -32601 (TestCapabilityGating).
}

func TestPublishEncodeError(t *testing.T) {
	s := newTestServer(t)
	if err := s.Publish("t", "notifications/x", func() {}); err == nil {
		t.Fatal("unmarshalable params must error")
	}
	if err := s.Publish("t", "notifications/x", 42); err == nil {
		t.Fatal("non-object params must error")
	}
	if err := s.Publish("t", "notifications/x", nil); err != nil {
		t.Fatal(err)
	}
}

func TestListenOverflowTerminates(t *testing.T) {
	s := newTestServer(t)
	s.AddResource(&protocol.Resource{URI: "info://of", Name: "of"},
		func(ctx context.Context, req *server.ResourceRequest) (protocol.ResourceResponse, error) {
			return protocol.NewReadResourceResult(protocol.NewTextResourceContents("info://of", "v")), nil
		})
	msg, err := protocol.NewRequest(protocol.IntID(9), protocol.MethodSubscriptionsListen, protocol.ListenParams{
		Meta:          newMeta(),
		Notifications: protocol.SubscriptionFilter{ResourceSubscriptions: []string{"info://of"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	msgs := make(chan *protocol.Message, 256)
	done := make(chan struct{})
	go func() {
		defer close(done)
		first := true
		s.Handle(context.Background(), msg, func(m *protocol.Message) error {
			if !first {
				<-release // stall the stream after the ack
			}
			first = false
			msgs <- m
			return nil
		})
	}()
	if ack := recvMsg(t, msgs); ack.Method != protocol.NotificationSubscriptionsAcknowledged {
		t.Fatalf("first message = %q", ack.Method)
	}

	// Overfill the subscriber buffer while the stream is stalled, then let it
	// drain: the stream must end with a visible overflow error rather than
	// dropping notifications silently.
	for range 70 {
		s.ResourceUpdated("info://of")
	}
	close(release)

	deadline := time.After(5 * time.Second)
	var sawCancelled bool
	for {
		select {
		case m := <-msgs:
			switch m.Kind() {
			case protocol.KindNotification:
				if m.Method != protocol.NotificationCancelled {
					continue
				}
				sawCancelled = true
				var p protocol.CancelledParams
				if err := json.Unmarshal(m.Params, &p); err != nil {
					t.Fatal(err)
				}
				if p.RequestID != protocol.IntID(9) {
					t.Fatalf("cancelled requestId = %v, want the listen request id", p.RequestID)
				}
			case protocol.KindError:
				// Server-initiated teardown must announce itself first.
				if !sawCancelled {
					t.Fatal("subscription torn down without notifications/cancelled")
				}
				if m.Error.Code != protocol.CodeInternal || !strings.Contains(m.Error.Message, "overflow") {
					t.Fatalf("error = %+v", m.Error)
				}
				<-done
				return
			case protocol.KindResponse:
				t.Fatal("stream ended gracefully despite overflow")
			}
		case <-deadline:
			t.Fatal("no overflow error emitted")
		}
	}
}

func listenMsg(t *testing.T, id int64) *protocol.Message {
	t.Helper()
	msg, err := protocol.NewRequest(protocol.IntID(id), protocol.MethodSubscriptionsListen, protocol.ListenParams{
		Meta:          newMeta(),
		Notifications: protocol.SubscriptionFilter{ToolsListChanged: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

// wantTeardown asserts the spec's sequence for server-initiated teardown of
// listen request id: notifications/cancelled, then a complete result.
func wantTeardown(t *testing.T, msgs chan *protocol.Message, id int64) {
	t.Helper()
	cancelled := recvMsg(t, msgs)
	if cancelled.Method != protocol.NotificationCancelled {
		t.Fatalf("first teardown message = %+v, want notifications/cancelled", cancelled)
	}
	var p protocol.CancelledParams
	if err := json.Unmarshal(cancelled.Params, &p); err != nil {
		t.Fatal(err)
	}
	if p.RequestID != protocol.IntID(id) {
		t.Fatalf("cancelled requestId = %v, want %d", p.RequestID, id)
	}
	res := decodeResult[protocol.ListenResult](t, recvMsg(t, msgs))
	if res.Meta.SubscriptionID != protocol.IntID(id) {
		t.Fatalf("final result meta: %+v", res.Meta)
	}
}

func TestShutdownEndsListenGracefully(t *testing.T) {
	s := newTestServer(t)
	msgs := make(chan *protocol.Message, 8)
	done := make(chan struct{})
	first := listenMsg(t, 1)
	go func() {
		defer close(done)
		s.Handle(context.Background(), first, func(m *protocol.Message) error { msgs <- m; return nil })
	}()
	if ack := recvMsg(t, msgs); ack.Method != protocol.NotificationSubscriptionsAcknowledged {
		t.Fatalf("first message = %q", ack.Method)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	// Shutdown returns only after the final message was handed to the
	// transport, so both teardown messages are already queued.
	if len(msgs) != 2 {
		t.Fatalf("queued messages after Shutdown = %d, want cancelled + result", len(msgs))
	}
	wantTeardown(t, msgs, 1)
	<-done

	// A stream opened after Shutdown is acknowledged, then torn down at once.
	msgs = make(chan *protocol.Message, 8)
	s.Handle(context.Background(), listenMsg(t, 2), func(m *protocol.Message) error { msgs <- m; return nil })
	if ack := recvMsg(t, msgs); ack.Method != protocol.NotificationSubscriptionsAcknowledged {
		t.Fatalf("first message = %q", ack.Method)
	}
	wantTeardown(t, msgs, 2)

	// Idempotent, and a no-op without streams.
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownHonorsContext(t *testing.T) {
	s := newTestServer(t)
	release := make(chan struct{})
	done := make(chan struct{})
	acked := make(chan struct{})
	msg := listenMsg(t, 1)
	go func() {
		defer close(done)
		first := true
		s.Handle(context.Background(), msg, func(m *protocol.Message) error {
			if first {
				first = false
				close(acked)
				return nil
			}
			<-release // a transport that cannot take the teardown messages
			return nil
		})
	}()
	<-acked

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); err != context.DeadlineExceeded {
		t.Fatalf("Shutdown with a stuck stream = %v, want deadline exceeded", err)
	}

	close(release)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := s.Shutdown(ctx2); err != nil {
		t.Fatal(err)
	}
	<-done
}

func TestOutputSchemaEnforced(t *testing.T) {
	s := newTestServer(t)
	schema := protocol.JSONSchema{
		"type":       "object",
		"properties": map[string]any{"n": map[string]any{"type": "number"}},
		"required":   []any{"n"},
	}
	var result *protocol.CallToolResult
	s.AddTool(&protocol.Tool{Name: "typed-out", OutputSchema: schema},
		func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
			return result, nil
		})
	call := func() *protocol.Message {
		final, _ := do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{Meta: newMeta(), Name: "typed-out"})
		return final
	}

	// Conforming structured content passes.
	result = &protocol.CallToolResult{StructuredContent: map[string]any{"n": 1}}
	if final := call(); final.Error != nil {
		t.Fatalf("conforming output rejected: %+v", final.Error)
	}
	// Non-conforming output is a server bug (the spec's MUST), not something
	// to ship to the client.
	result = &protocol.CallToolResult{StructuredContent: map[string]any{"x": true}}
	if final := call(); final.Error == nil || final.Error.Code != protocol.CodeInternal {
		t.Fatalf("non-conforming output: %+v", final.Error)
	}
	// A declared schema with no structured content at all is equally a bug.
	result = &protocol.CallToolResult{Content: protocol.ContentList{protocol.NewTextContent("x")}}
	if final := call(); final.Error == nil || final.Error.Code != protocol.CodeInternal {
		t.Fatalf("missing structured output: %+v", final.Error)
	}
	// Execution errors carry no structured result and are exempt.
	result = protocol.NewToolResultError("failed")
	if final := call(); final.Error != nil {
		t.Fatalf("isError result must be exempt: %+v", final.Error)
	}
}

func recvMsg(t *testing.T, ch chan *protocol.Message) *protocol.Message {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
		return nil
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	s := newTestServer(t)
	s.Use(server.Recovery())
	server.AddTool(s, &protocol.Tool{Name: "boom"},
		func(ctx context.Context, req *server.CallRequest, in any) (protocol.ToolResponse, any, error) {
			panic("kaboom")
		})
	final, _ := do(t, s, protocol.MethodToolsCall, protocol.CallToolParams{Meta: newMeta(), Name: "boom"})
	if final.Error == nil || final.Error.Code != protocol.CodeInternal {
		t.Fatalf("recovery: %+v", final.Error)
	}
	if strings.Contains(final.Error.Message, "kaboom") {
		t.Fatal("panic detail leaked to the wire")
	}
}

func TestStateSigning(t *testing.T) {
	s := server.New(&server.Options{StateKey: []byte("k")})
	state, err := s.SignState([]byte(`{"step":1}`))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := s.VerifyState(state)
	if err != nil || string(payload) != `{"step":1}` {
		t.Fatalf("verify: %s, %v", payload, err)
	}
	if _, err := s.VerifyState(state + "x"); err == nil {
		t.Fatal("tampered state accepted")
	}
}

func TestInvalidHeaderBindingPanics(t *testing.T) {
	s := server.New(nil)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on invalid x-mcp-header binding")
		}
	}()
	s.AddTool(&protocol.Tool{
		Name: "bad",
		InputSchema: protocol.JSONSchema{
			"type": "object",
			"properties": map[string]any{
				"n": map[string]any{"type": "number", "x-mcp-header": "N"},
			},
		},
	}, func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
		return protocol.NewToolResultText("x"), nil
	})
}
