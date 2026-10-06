package streamhttp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/internal/headerbind"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport"
	"github.com/voocel/mcp-sdk-go/transport/streamhttp"
)

type backendState struct {
	blockStarted   chan struct{}
	blockCancelled chan struct{}
}

func newBackend() (*server.Server, *backendState) {
	st := &backendState{
		blockStarted:   make(chan struct{}),
		blockCancelled: make(chan struct{}),
	}
	srv := server.New(&server.Options{Impl: protocol.Implementation{Name: "http-test", Version: "1"}})

	type echoIn struct {
		Text string `json:"text"`
	}
	server.AddTool(srv, &protocol.Tool{Name: "echo", Description: "echo"},
		func(ctx context.Context, req *server.CallRequest, in echoIn) (protocol.ToolResponse, string, error) {
			return nil, in.Text, nil
		})

	srv.AddTool(&protocol.Tool{
		Name: "hdr",
		InputSchema: protocol.JSONSchema{
			"type": "object",
			"properties": map[string]any{
				"region": map[string]any{"type": "string", "x-mcp-header": "Region"},
				"count":  map[string]any{"type": "integer", "x-mcp-header": "Count"},
			},
		},
	}, func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
		return protocol.NewToolResultText("ok"), nil
	})

	server.AddTool(srv, &protocol.Tool{Name: "progress", Description: "reports progress"},
		func(ctx context.Context, req *server.CallRequest, _ struct{}) (protocol.ToolResponse, string, error) {
			_ = req.ReportProgress(ctx, 1, 2, "a")
			_ = req.ReportProgress(ctx, 2, 2, "b")
			return nil, "done", nil
		})

	server.AddTool(srv, &protocol.Tool{Name: "touch", Description: "trigger update"},
		func(ctx context.Context, req *server.CallRequest, _ struct{}) (protocol.ToolResponse, string, error) {
			srv.ResourceUpdated("test://res")
			return nil, "ok", nil
		})

	// Resource subscriptions are only honored when resources are registered.
	srv.AddResource(&protocol.Resource{URI: "test://res", Name: "res"},
		func(ctx context.Context, req *server.ResourceRequest) (protocol.ResourceResponse, error) {
			return protocol.NewReadResourceResult(protocol.NewTextResourceContents("test://res", "v")), nil
		})

	server.AddTool(srv, &protocol.Tool{Name: "block", Description: "blocks until cancelled"},
		func(ctx context.Context, req *server.CallRequest, _ struct{}) (protocol.ToolResponse, string, error) {
			_ = req.ReportProgress(ctx, 0, 0, "started")
			close(st.blockStarted)
			<-ctx.Done()
			close(st.blockCancelled)
			return nil, "", ctx.Err()
		})

	return srv, st
}

const metaFragment = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`

func rpcBody(id int, method, paramsExtra string) string {
	p := "{" + metaFragment
	if paramsExtra != "" {
		p += "," + paramsExtra
	}
	p += "}"
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, method, p)
}

func callBody(id int, tool, args string) string {
	if args == "" {
		args = "{}"
	}
	return rpcBody(id, "tools/call", fmt.Sprintf(`"name":%q,"arguments":%s`, tool, args))
}

func stdHeaders(method string) map[string]string {
	return map[string]string{
		"MCP-Protocol-Version": protocol.Version,
		"Mcp-Method":           method,
	}
}

func doPost(t *testing.T, url, body string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, b
}

func errCode(t *testing.T, body []byte) int {
	t.Helper()
	var probe struct {
		Error *protocol.Error `json:"error"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Error == nil {
		t.Fatalf("no JSON-RPC error in body: %s", body)
	}
	return probe.Error.Code
}

func TestHeaderValidationMatrix(t *testing.T) {
	srv, _ := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(srv, nil))
	defer ts.Close()

	tests := []struct {
		name       string
		body       string
		headers    map[string]string
		wantStatus int
		wantCode   int // 0 = expect success
	}{
		{
			name:       "discover ok",
			body:       rpcBody(1, "server/discover", ""),
			headers:    stdHeaders("server/discover"),
			wantStatus: http.StatusOK,
		},
		{
			name: "missing protocol version header",
			body: rpcBody(1, "server/discover", ""),
			headers: map[string]string{
				"Mcp-Method": "server/discover",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeHeaderMismatch,
		},
		{
			name: "protocol version header mismatch",
			body: rpcBody(1, "server/discover", ""),
			headers: map[string]string{
				"MCP-Protocol-Version": "2025-11-25",
				"Mcp-Method":           "server/discover",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeHeaderMismatch,
		},
		{
			name: "missing Mcp-Method",
			body: rpcBody(1, "server/discover", ""),
			headers: map[string]string{
				"MCP-Protocol-Version": protocol.Version,
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeHeaderMismatch,
		},
		{
			name:       "Mcp-Method mismatch",
			body:       rpcBody(1, "server/discover", ""),
			headers:    stdHeaders("tools/list"),
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeHeaderMismatch,
		},
		{
			name:       "tools/call missing Mcp-Name",
			body:       callBody(1, "echo", `{"text":"x"}`),
			headers:    stdHeaders("tools/call"),
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeHeaderMismatch,
		},
		{
			name: "Mcp-Name mismatch",
			body: callBody(1, "echo", `{"text":"x"}`),
			headers: map[string]string{
				"MCP-Protocol-Version": protocol.Version,
				"Mcp-Method":           "tools/call",
				"Mcp-Name":             "other",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeHeaderMismatch,
		},
		{
			name: "Mcp-Name sentinel-encoded",
			body: callBody(1, "echo", `{"text":"x"}`),
			headers: map[string]string{
				"MCP-Protocol-Version": protocol.Version,
				"Mcp-Method":           "tools/call",
				"Mcp-Name":             "=?base64?ZWNobw==?=", // "echo"
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown method maps to 404",
			body:       rpcBody(1, "foo/bar", ""),
			headers:    stdHeaders("foo/bar"),
			wantStatus: http.StatusNotFound,
			wantCode:   protocol.CodeMethodNotFound,
		},
		{
			name:       "missing _meta",
			body:       `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
			headers:    stdHeaders("tools/list"),
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeInvalidParams,
		},
		{
			name: "unsupported protocol version",
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-11-25","io.modelcontextprotocol/clientCapabilities":{}}}}`,
			headers: map[string]string{
				"MCP-Protocol-Version": "2025-11-25",
				"Mcp-Method":           "tools/list",
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeUnsupportedProtocolVersion,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := doPost(t, ts.URL, tt.body, tt.headers)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, tt.wantStatus, body)
			}
			if tt.wantCode != 0 {
				if got := errCode(t, body); got != tt.wantCode {
					t.Fatalf("error code = %d, want %d; body: %s", got, tt.wantCode, body)
				}
			} else if !strings.Contains(string(body), `"resultType":"complete"`) {
				t.Fatalf("expected complete result, got: %s", body)
			}
		})
	}
}

func TestParamHeaderMatrix(t *testing.T) {
	srv, _ := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(srv, nil))
	defer ts.Close()

	hdrCall := func(extra map[string]string) map[string]string {
		h := stdHeaders("tools/call")
		h["Mcp-Name"] = "hdr"
		maps.Copy(h, extra)
		return h
	}

	tests := []struct {
		name       string
		args       string
		extra      map[string]string
		wantStatus int
		wantCode   int
	}{
		{
			name:       "matching string header",
			args:       `{"region":"us"}`,
			extra:      map[string]string{"Mcp-Param-Region": "us"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "param present but header missing",
			args:       `{"region":"us"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeHeaderMismatch,
		},
		{
			name:       "header present but param absent",
			args:       `{}`,
			extra:      map[string]string{"Mcp-Param-Region": "us"},
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeHeaderMismatch,
		},
		{
			name:       "value mismatch",
			args:       `{"region":"us"}`,
			extra:      map[string]string{"Mcp-Param-Region": "eu"},
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeHeaderMismatch,
		},
		{
			name:       "integer exact",
			args:       `{"count":42}`,
			extra:      map[string]string{"Mcp-Param-Count": "42"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "integer compared numerically",
			args:       `{"count":42}`,
			extra:      map[string]string{"Mcp-Param-Count": "42.0"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "non-ASCII value sentinel-encoded",
			args:       `{"region":"héllo"}`,
			extra:      map[string]string{"Mcp-Param-Region": headerbind.Encode("héllo")},
			wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := doPost(t, ts.URL, callBody(1, "hdr", tt.args), hdrCall(tt.extra))
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, tt.wantStatus, body)
			}
			if tt.wantCode != 0 {
				if got := errCode(t, body); got != tt.wantCode {
					t.Fatalf("error code = %d, want %d; body: %s", got, tt.wantCode, body)
				}
			}
		})
	}
}

// wrapped decorates a server's Handle and inherits the rest by embedding, the
// way an application adds behavior in front of *server.Server.
type wrapped struct{ *server.Server }

func (w wrapped) Handle(ctx context.Context, msg *protocol.Message, emit func(*protocol.Message) error) {
	w.Server.Handle(ctx, msg, emit)
}

var _ streamhttp.Backend = wrapped{}

func TestWrappedBackendStillValidatesParamHeaders(t *testing.T) {
	srv, _ := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(wrapped{srv}, nil))
	defer ts.Close()

	h := stdHeaders("tools/call")
	h["Mcp-Name"] = "hdr"
	// The bound parameter is in the body but its Mcp-Param header is missing.
	resp, body := doPost(t, ts.URL, callBody(1, "hdr", `{"region":"us"}`), h)
	if resp.StatusCode != http.StatusBadRequest || errCode(t, body) != protocol.CodeHeaderMismatch {
		t.Fatalf("status = %d, body: %s; want 400 HeaderMismatch", resp.StatusCode, body)
	}
}

func TestHTTPBasics(t *testing.T) {
	srv, _ := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(srv, nil))
	defer ts.Close()

	t.Run("GET is 405", func(t *testing.T) {
		resp, err := http.Get(ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", resp.StatusCode)
		}
		if resp.Header.Get("Allow") != http.MethodPost {
			t.Fatalf("Allow = %q", resp.Header.Get("Allow"))
		}
	})

	t.Run("invalid JSON is 400 parse error", func(t *testing.T) {
		resp, body := doPost(t, ts.URL, "{not json", stdHeaders("x"))
		if resp.StatusCode != http.StatusBadRequest || errCode(t, body) != protocol.CodeParseError {
			t.Fatalf("status = %d, body: %s", resp.StatusCode, body)
		}
	})

	t.Run("notification is 202 with no body", func(t *testing.T) {
		note := `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`
		resp, body := doPost(t, ts.URL, note, nil)
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", resp.StatusCode)
		}
		if len(body) != 0 {
			t.Fatalf("202 must have no body, got: %s", body)
		}
	})

	t.Run("response-shaped body is 400", func(t *testing.T) {
		resp, body := doPost(t, ts.URL, `{"jsonrpc":"2.0","id":1,"result":{}}`, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, body: %s", resp.StatusCode, body)
		}
	})
}

func TestOriginValidation(t *testing.T) {
	srv, _ := newBackend()
	body := rpcBody(1, "server/discover", "")
	hdr := stdHeaders("server/discover")

	withOrigin := func(origin string) map[string]string {
		h := make(map[string]string, len(hdr)+1)
		maps.Copy(h, hdr)
		if origin != "" {
			h["Origin"] = origin
		}
		return h
	}

	t.Run("defaults", func(t *testing.T) {
		ts := httptest.NewServer(streamhttp.NewHandler(srv, nil))
		defer ts.Close()

		cases := []struct {
			origin string
			want   int
		}{
			{"", http.StatusOK},
			{"http://localhost:3000", http.StatusOK},
			{"http://" + strings.TrimPrefix(ts.URL, "http://"), http.StatusOK}, // same host
			{"https://evil.example.com", http.StatusForbidden},
		}
		for _, c := range cases {
			resp, b := doPost(t, ts.URL, body, withOrigin(c.origin))
			if resp.StatusCode != c.want {
				t.Fatalf("origin %q: status = %d, want %d; body: %s", c.origin, resp.StatusCode, c.want, b)
			}
		}
	})

	t.Run("allow-listed origin", func(t *testing.T) {
		ts := httptest.NewServer(streamhttp.NewHandler(srv, &streamhttp.Options{
			AllowedOrigins: []string{"https://app.example.com"},
		}))
		defer ts.Close()
		resp, b := doPost(t, ts.URL, body, withOrigin("https://app.example.com"))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body: %s", resp.StatusCode, b)
		}
	})

	t.Run("insecure any origin", func(t *testing.T) {
		ts := httptest.NewServer(streamhttp.NewHandler(srv, &streamhttp.Options{
			InsecureAllowAnyOrigin: true,
		}))
		defer ts.Close()
		resp, b := doPost(t, ts.URL, body, withOrigin("https://evil.example.com"))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body: %s", resp.StatusCode, b)
		}
	})
}

func TestJSONvsSSENegotiation(t *testing.T) {
	srv, _ := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(srv, nil))
	defer ts.Close()

	call := func(name, body string) (*http.Response, []byte) {
		h := stdHeaders("tools/call")
		h["Mcp-Name"] = name
		return doPost(t, ts.URL, body, h)
	}

	t.Run("no notifications means plain JSON", func(t *testing.T) {
		resp, body := call("echo", callBody(1, "echo", `{"text":"hi"}`))
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q", ct)
		}
		var m protocol.Message
		if err := json.Unmarshal(body, &m); err != nil || m.Error != nil {
			t.Fatalf("body: %s", body)
		}
	})

	t.Run("progress without token stays JSON", func(t *testing.T) {
		resp, _ := call("progress", callBody(1, "progress", ""))
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("progress with token switches to SSE", func(t *testing.T) {
		body := fmt.Sprintf(
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientCapabilities":{},"progressToken":"pt"},"name":"progress","arguments":{}}}`,
			protocol.Version)
		resp, raw := call("progress", body)
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
			t.Fatalf("Content-Type = %q; body: %s", ct, raw)
		}
		s := string(raw)
		if strings.Count(s, "notifications/progress") != 2 {
			t.Fatalf("want 2 progress notifications, body: %s", s)
		}
		if !strings.Contains(s, `\"resultType\":\"complete\"`) && !strings.Contains(s, `"resultType":"complete"`) {
			t.Fatalf("missing final result, body: %s", s)
		}
	})
}

func TestClientEndToEnd(t *testing.T) {
	srv, _ := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(srv, nil))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var progress atomic.Int32
	tr := streamhttp.New(ts.URL, nil)
	c := client.New(tr, &client.Options{
		Info:       &protocol.Implementation{Name: "test-client", Version: "1"},
		OnProgress: func(p *protocol.ProgressParams) { progress.Add(1) },
	})
	defer c.Close()

	lst, err := c.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(lst.Tools) != 5 {
		t.Fatalf("tools = %d, want 5", len(lst.Tools))
	}

	res, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "hi http"}})
	if err != nil {
		t.Fatalf("CallTool echo: %v", err)
	}
	if res.StructuredContent != "hi http" {
		t.Fatalf("echo structuredContent: %v", res.StructuredContent)
	}

	// The client derives Mcp-Param-* headers from the listed bindings; the
	// server verifies them against the body, including sentinel encoding.
	if _, err := c.CallTool(ctx, &protocol.CallToolParams{
		Name:      "hdr",
		Arguments: map[string]any{"region": "eu-wést-1", "count": 7},
	}); err != nil {
		t.Fatalf("CallTool hdr: %v", err)
	}

	if _, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "progress"}); err != nil {
		t.Fatalf("CallTool progress: %v", err)
	}
	if got := progress.Load(); got != 2 {
		t.Fatalf("progress notifications = %d, want 2", got)
	}

	// Unknown tool surfaces the protocol error typed.
	_, err = c.CallTool(ctx, &protocol.CallToolParams{Name: "nope"})
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.CodeInvalidParams {
		t.Fatalf("unknown tool error = %v", err)
	}

	// Subscription over a long-lived SSE stream.
	sub, err := c.Listen(ctx, protocol.SubscriptionFilter{ResourceSubscriptions: []string{"test://res"}})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if _, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "touch"}); err != nil {
		t.Fatalf("CallTool touch: %v", err)
	}
	select {
	case ev, ok := <-sub.Events():
		if !ok {
			t.Fatalf("subscription closed early: %v", sub.Err())
		}
		if uri, ok := ev.ResourceUpdated(); !ok || uri != "test://res" {
			t.Fatalf("event: %+v", ev)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for subscription event")
	}
	_ = sub.Close()
}

func TestClientRefreshesBindingsOnHeaderMismatch(t *testing.T) {
	srv, _ := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(srv, nil))
	defer ts.Close()
	c := client.New(streamhttp.New(ts.URL, nil), &client.Options{
		Info: &protocol.Implementation{Name: "t", Version: "1"},
	})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The client never listed tools, so it knows no x-mcp-header bindings and
	// omits Mcp-Param-Region; the server answers HeaderMismatch and the client
	// must recover by refreshing the bindings and retrying.
	res, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "hdr", Arguments: map[string]any{"region": "us"}})
	if err != nil {
		t.Fatalf("CallTool without prior ListTools: %v", err)
	}
	if res.IsError {
		t.Fatalf("result: %+v", res)
	}
}

func TestServerShutdownEndsListenStreams(t *testing.T) {
	srv, _ := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(srv, nil))
	defer ts.Close()
	c := client.New(streamhttp.New(ts.URL, nil), &client.Options{
		Info: &protocol.Implementation{Name: "t", Version: "1"},
	})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sub, err := c.Listen(ctx, protocol.SubscriptionFilter{ResourceSubscriptions: []string{"test://res"}})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	// Runs before ts.Close, so a failing test cannot hang on the open stream.
	defer sub.Close()

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("server Shutdown: %v", err)
	}
	// The stream ended gracefully, so the handler returned and the HTTP server
	// can drain: http.Server.Shutdown would otherwise wait on it until ctx ends.
	if err := ts.Config.Shutdown(ctx); err != nil {
		t.Fatalf("http Shutdown: %v", err)
	}
	for ev := range sub.Events() {
		t.Fatalf("teardown surfaced as an event: %+v", ev)
	}
	if sub.Err() != nil {
		t.Fatalf("Err after graceful server shutdown: %v", sub.Err())
	}
}

func TestListenStreamGetsKeepAlive(t *testing.T) {
	srv, _ := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(srv, &streamhttp.Options{KeepAlive: 20 * time.Millisecond}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body := rpcBody(1, "subscriptions/listen", `"notifications":{"toolsListChanged":true}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range stdHeaders("subscriptions/listen") {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// An idle subscription stream must emit SSE comment lines on its own.
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if sc.Text() == ":" {
			return
		}
	}
	t.Fatalf("no keep-alive comment on the idle stream: %v", sc.Err())
}

func TestClientRetriesTransientStatus(t *testing.T) {
	srv, _ := newBackend()
	real := streamhttp.NewHandler(srv, nil)
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		real.ServeHTTP(w, r)
	}))
	defer ts.Close()

	c := client.New(streamhttp.New(ts.URL, nil), &client.Options{
		Info: &protocol.Implementation{Name: "t", Version: "1"},
	})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := c.Discover(ctx); err != nil {
		t.Fatalf("Discover after retry: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", calls.Load())
	}
}

func TestClientNoRetryForSideEffects(t *testing.T) {
	// tools/call may have executed server-side even when the response is lost;
	// the transport must fail fast instead of re-sending it.
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	c := client.New(streamhttp.New(ts.URL, nil), &client.Options{
		Info: &protocol.Implementation{Name: "t", Version: "1"},
	})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "echo"}); err == nil {
		t.Fatal("expected an error from the 503")
	}
	if calls.Load() != 1 {
		t.Fatalf("tools/call attempts = %d, want 1 (no retry)", calls.Load())
	}
}

func TestClientNotificationGets202(t *testing.T) {
	srv, _ := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(srv, nil))
	defer ts.Close()

	note, err := protocol.NewNotification(protocol.NotificationCancelled,
		protocol.CancelledParams{RequestID: protocol.IntID(1)})
	if err != nil {
		t.Fatal(err)
	}
	tr := streamhttp.New(ts.URL, nil)
	defer tr.Close()
	st, err := tr.Do(context.Background(), &transport.Request{Message: note})
	if err != nil || st != nil {
		t.Fatalf("Do(notification) = %v, %v; want nil, nil", st, err)
	}
}

func TestStreamCloseCancelsHandler(t *testing.T) {
	srv, st := newBackend()
	ts := httptest.NewServer(streamhttp.NewHandler(srv, nil))
	defer ts.Close()

	body := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientCapabilities":{},"progressToken":"pt9"},"name":"block","arguments":{}}}`,
		protocol.Version)
	var msg protocol.Message
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatal(err)
	}

	tr := streamhttp.New(ts.URL, nil)
	defer tr.Close()
	stream, err := tr.Do(context.Background(), &transport.Request{
		Message: &msg,
		Headers: map[string]string{
			"MCP-Protocol-Version": protocol.Version,
			"Mcp-Method":           "tools/call",
			"Mcp-Name":             "block",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// First message is the progress notification the tool emits on entry.
	if m, err := stream.Recv(); err != nil || m.Method != protocol.NotificationProgress {
		t.Fatalf("first message = %+v, %v", m, err)
	}
	select {
	case <-st.blockStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started")
	}

	// Closing the response stream is the cancellation signal.
	_ = stream.Close()
	select {
	case <-st.blockCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("closing the stream did not cancel the handler")
	}
}
