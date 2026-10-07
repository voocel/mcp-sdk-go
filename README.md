# MCP Go SDK

<div align="center">

<strong>An elegant, stateless Go implementation of the Model Context Protocol (MCP)</strong>

[![English](https://img.shields.io/badge/lang-English-blue.svg)](./README.md)
[![中文](https://img.shields.io/badge/lang-中文-red.svg)](./README_CN.md)

![License](https://img.shields.io/badge/license-MIT-blue.svg)
[![Go Reference](https://pkg.go.dev/badge/github.com/voocel/mcp-sdk-go.svg)](https://pkg.go.dev/github.com/voocel/mcp-sdk-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/voocel/mcp-sdk-go)](https://goreportcard.com/report/github.com/voocel/mcp-sdk-go)
[![Build Status](https://github.com/voocel/mcp-sdk-go/workflows/go/badge.svg)](https://github.com/voocel/mcp-sdk-go/actions)

</div>

## Introduction

A clean-slate Go SDK for **MCP 2026-07-28**, the stateless revision of the Model Context Protocol. This SDK targets 2026-07-28 exclusively — no `initialize` handshake, no sessions, no legacy transports — which keeps the API small, explicit and honest about what travels on the wire.

It also ships the first Go implementation of the official **tasks extension** (`io.modelcontextprotocol/tasks`): durable, pollable, cancellable long-running tool calls with an `input_required` rendezvous.

## Highlights

- **Stateless by design** — the server is a pure function: one message in, a stream of messages out. All per-request context travels in `_meta`; there is no session object on either side.
- **Typed tools** — input schemas inferred from Go structs, arguments validated before your handler runs, structured output derived from return types.
- **MRTR (multi round-trip requests)** — the 2026-07-28 replacement for server-initiated requests. Interim `input_required` results are a compile-safe sum type on the server; the client fulfills elicitation requests automatically and retries.
- **Subscriptions** — `subscriptions/listen` notification streams with filters, backed by a non-blocking hub.
- **Tasks extension** — `tasks/get`, `tasks/update`, `tasks/cancel`, `notifications/tasks`, capability-gated per request with synchronous fallback, plus a pluggable `Store`.
- **Two transports** — STDIO (newline-delimited, per-request goroutines, `notifications/cancelled`) and Streamable HTTP (single POST endpoint, JSON or SSE responses, stream-close cancellation, full `Mcp-*` header validation).
- **OAuth authorization** — the client side of MCP authorization: Protected Resource Metadata and authorization server discovery, Client ID Metadata Documents and pre-registered clients, authorization code with PKCE on a loopback redirect, RFC 8707 `resource`, RFC 9207 `iss` validation, step-up scopes, and an `http.RoundTripper` that sends and refreshes the token.
- **Safe defaults** — Origin validation on by default, body/message size limits, path traversal protection (`os.OpenRoot`), HMAC helpers for MRTR `requestState`, panics never leak stacks to the wire.
- **Conformance-tested** — spec wire examples run as fixtures in CI: `resultType` on every result, `_meta` key rules, HTTP status mapping, header/body validation matrix, sentinel encoding.

## Requirements

- Go 1.25+

```bash
go get github.com/voocel/mcp-sdk-go
```

## Quick start

### Server (STDIO)

```go
package main

import (
	"context"
	"log"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

type greetIn struct {
	Name string `json:"name" jsonschema:"required"`
}

func main() {
	srv := server.New(&server.Options{
		Impl: protocol.Implementation{Name: "my-server", Version: "1.0.0"},
	})

	server.AddTool(srv, &protocol.Tool{Name: "greet", Description: "Greet someone"},
		func(ctx context.Context, req *server.CallRequest, in greetIn) (protocol.ToolResponse, string, error) {
			return nil, "Hello, " + in.Name + "!", nil
		})

	if err := stdio.Serve(context.Background(), srv, nil); err != nil {
		log.Fatal(err)
	}
}
```

Serving the same `srv` over HTTP is one line — the server core is transport-agnostic:

```go
http.Handle("/mcp", streamhttp.NewHandler(srv, nil))
```

### Client

```go
c := client.New(streamhttp.New("http://localhost:8080/mcp", nil), &client.Options{
	Info: &protocol.Implementation{Name: "my-client", Version: "1.0.0"},
})
defer c.Close()

res, err := c.CallTool(ctx, &protocol.CallToolParams{
	Name:      "greet",
	Arguments: map[string]any{"name": "Ada"},
})
```

The client stamps `_meta` (protocol version, derived capabilities, client info) and the required `Mcp-*` HTTP headers on every request. For a subprocess server, use `stdio.NewCommand(exec.Command(...), nil)` as the transport.

### Authorization

```go
authz := auth.New(auth.Config{
	Server:            endpoint,
	Store:             store,
	ClientMetadataURL: "https://example.com/oauth/client.json",
})
c := client.New(streamhttp.New(endpoint, &streamhttp.TransportOptions{
	HTTPClient: &http.Client{Transport: authz.Transport(http.DefaultTransport)},
}), nil)

_, err := c.Discover(ctx)
var se *streamhttp.StatusError
if errors.As(err, &se) && se.StatusCode == http.StatusUnauthorized {
	l, _ := authz.Login(ctx) // discovers, listens on a loopback redirect URI
	openBrowser(l.URL)
	err = l.Wait(ctx) // stores the token; requests now carry it
}
```

The client identifies itself as the 2026-07-28 specification orders: a client registered with the authorization server beforehand (`ClientID`, and `ClientSecret` if it has one; redirect URI `http://127.0.0.1/callback` on any port), or else its Client ID Metadata Document (`ClientMetadataURL`), which the authorization server fetches. The document lists loopback redirect URIs on fixed ports — a few, since login listens on the first free one — and `"token_endpoint_auth_method": "none"`. An authorization server that takes neither fails `Login` with `auth.ErrClientRequired`. Dynamic Client Registration, deprecated by the specification, is not supported.

`Store` persists tokens by server URL; they are secrets, so keep them where only the user can read. The transport refreshes a token before it expires and once more when the server rejects it; a 401 or 403 it cannot fix comes back as a `*streamhttp.StatusError` for the caller to log in again.

### Elicitation (MRTR)

A tool asks the user for input by returning an interim result; the client answers and retries automatically:

```go
// Server: re-entrant handler — check for the answer first, otherwise ask.
srv.AddTool(&protocol.Tool{Name: "signup"}, func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
	if er, err := req.Params.InputResponses.Elicit("email"); err == nil {
		return protocol.NewToolResultText("registered " + er.Content["email"].(string)), nil
	}
	return protocol.RequireInput(protocol.InputRequests{
		"email": protocol.NewElicitFormRequest("Your email?", schema),
	}, ""), nil
})

// Client: an Elicitor declares the capability and answers during the loop.
c := client.New(tr, &client.Options{
	Elicitor: func(ctx context.Context, p *protocol.ElicitParams) (*protocol.ElicitResult, error) {
		return &protocol.ElicitResult{Action: protocol.ElicitActionAccept,
			Content: map[string]any{"email": "ada@example.com"}}, nil
	},
})
```

### Tasks (official extension)

```go
// Server
tk := tasks.Install(srv, tasks.NewMemStore(), nil)
tasks.AddTool(tk, &protocol.Tool{Name: "research"},
	func(ctx context.Context, tc *tasks.Context, in researchIn) (*protocol.CallToolResult, error) {
		// long-running work; tc.RequireInput parks the task in input_required
		return protocol.NewToolResultText("done"), nil
	})

// Client
_, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "research"})
if task, ok := tasks.AsTask(err); ok {
	final, _ := tasks.Await(ctx, c, task, nil) // polls until terminal
	res, _ := final.ToolResult()
}
```

Clients that do not declare the capability get the tool executed synchronously (or rejected with `-32021`, per `tasks.Options.Reject`).

## Packages

| Package | Purpose |
|---|---|
| `protocol` | Wire types for 2026-07-28, zero third-party dependencies |
| `server` | Stateless server core: method table, typed registration, hub, middleware |
| `client` | Typed client: `_meta`/header stamping, MRTR loop, paging iterators, subscriptions |
| `transport` | Client transport abstraction (`Do(ctx, req) → Stream`) |
| `transport/stdio` | `Serve` (server) and `Command` (subprocess client) |
| `transport/streamhttp` | `Handler` (server) and `Transport` (client) for Streamable HTTP |
| `transport/mem` | In-process transport for tests and embedding |
| `ext/tasks` | Official tasks extension: server, client and store |
| `auth` | Client side of MCP authorization: discovery, login, token refresh |

## Examples

| Example | Shows |
|---|---|
| [basic](./examples/basic) | Tool + resource + prompt over STDIO |
| [calculator](./examples/calculator) | Typed tools with schema inference and validation |
| [file-server](./examples/file-server) | File resources with path traversal protection |
| [streamhttp](./examples/streamhttp) | HTTP server + client: SSE progress, subscriptions |
| [elicitation](./examples/elicitation) | The full MRTR loop in one process |
| [tasks](./examples/tasks) | Task lifecycle with `input_required` rendezvous |

## Scope

This SDK implements MCP 2026-07-28 and nothing else. Features the revision removed or deprecated are intentionally absent: `initialize`/sessions, Roots, Sampling, Logging, `resources/subscribe`, ping, SSE resumability (`Last-Event-ID`).

Two concessions to other revisions remain, because the spec makes both mandatory: `server/discover` answers callers on any protocol version so they can read `supportedVersions` from it, and the client treats a result that omits `resultType` as `complete`. Unknown `InputRequest` methods (e.g. deprecated `roots/list` from older servers) round-trip untouched and surface to the caller instead of being silently dropped.

## License

MIT
