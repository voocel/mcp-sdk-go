// A minimal MCP stdio server: one typed tool, one resource, one prompt.
//
// Run: go run ./examples/basic
// Then speak MCP 2026-07-28 over stdin/stdout (see examples/streamhttp-demo
// for a runnable client).
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
		Impl:         protocol.Implementation{Name: "basic-server", Version: "1.0.0"},
		Instructions: "A minimal MCP server: one tool, one resource, one prompt.",
	})

	server.AddTool(srv, &protocol.Tool{Name: "greet", Description: "Greet someone by name"},
		func(ctx context.Context, req *server.CallRequest, in greetIn) (protocol.ToolResponse, string, error) {
			return nil, "Hello, " + in.Name + "!", nil
		})

	srv.AddResource(&protocol.Resource{
		URI:      "info://about",
		Name:     "about",
		MimeType: "text/plain",
	}, func(ctx context.Context, req *server.ResourceRequest) (protocol.ResourceResponse, error) {
		return protocol.NewReadResourceResult(
			protocol.NewTextResourceContents("info://about", "basic-server: an MCP 2026-07-28 demo")), nil
	})

	srv.AddPrompt(&protocol.Prompt{
		Name:        "review",
		Description: "Ask for a code review",
		Arguments:   []protocol.PromptArgument{{Name: "code", Required: true}},
	}, func(ctx context.Context, req *server.PromptRequest) (protocol.PromptResponse, error) {
		return protocol.NewGetPromptResult("code review request",
			protocol.NewPromptMessage(protocol.RoleUser,
				protocol.NewTextContent("Please review this code:\n\n"+req.Params.Arguments["code"]))), nil
	})

	if err := stdio.Serve(context.Background(), srv, nil); err != nil {
		log.Fatal(err)
	}
}
