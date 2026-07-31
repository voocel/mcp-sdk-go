// Elicitation via MRTR (multi round-trip requests), the 2026-07-28
// replacement for server-initiated requests: the server answers tools/call
// with an input_required interim result; the client fulfills the input
// requests and retries automatically.
//
// Server and client run in one process over the in-memory transport so the
// whole loop is visible in a single run: go run ./examples/elicitation
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/mem"
)

func main() {
	srv := server.New(&server.Options{
		Impl: protocol.Implementation{Name: "signup-server", Version: "1.0.0"},
	})

	// MRTR handlers are re-entrant: the retry runs the handler again with
	// inputResponses filled in, so it first checks for an answer.
	srv.AddTool(&protocol.Tool{Name: "signup", Description: "Register with an email address"},
		func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
			if er, err := req.Params.InputResponses.Elicit("email"); err == nil {
				if er.Action != protocol.ElicitActionAccept {
					return protocol.NewToolResultError("signup " + string(er.Action) + "d by user"), nil
				}
				email, _ := er.Content["email"].(string)
				return protocol.NewToolResultText("registered " + email), nil
			}
			return protocol.RequireInput(protocol.InputRequests{
				"email": protocol.NewElicitFormRequest("What is your email address?", protocol.JSONSchema{
					"type": "object",
					"properties": map[string]any{
						"email": map[string]any{"type": "string", "format": "email"},
					},
					"required": []string{"email"},
				}),
			}, ""), nil
		})

	// The Elicitor both declares the elicitation capability and answers the
	// input requests during the automatic MRTR loop.
	c := client.New(mem.New(srv), &client.Options{
		Info: &protocol.Implementation{Name: "signup-client", Version: "1.0.0"},
		Elicitor: func(ctx context.Context, p *protocol.ElicitParams) (*protocol.ElicitResult, error) {
			fmt.Printf("server asks: %s\n", p.Message)
			return &protocol.ElicitResult{
				Action:  protocol.ElicitActionAccept,
				Content: map[string]any{"email": "ada@example.com"},
			}, nil
		},
	})
	defer c.Close()

	res, err := c.CallTool(context.Background(), &protocol.CallToolParams{Name: "signup"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("result: %s\n", res.Content[0].(protocol.TextContent).Text)
}
