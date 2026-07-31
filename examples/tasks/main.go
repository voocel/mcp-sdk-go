// The official tasks extension (io.modelcontextprotocol/tasks): a tool call
// becomes a durable task the client polls, including an input_required
// rendezvous answered via tasks/update.
//
// Server and client run in one process over the in-memory transport:
// go run ./examples/tasks
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/ext/tasks"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/mem"
)

type researchIn struct {
	Topic string `json:"topic" jsonschema:"required"`
}

func main() {
	srv := server.New(&server.Options{
		Impl: protocol.Implementation{Name: "task-server", Version: "1.0.0"},
	})
	tk := tasks.Install(srv, tasks.NewMemStore(), &tasks.Options{PollIntervalMs: 100})

	tasks.AddTool(tk, &protocol.Tool{Name: "research", Description: "Long-running research"},
		func(ctx context.Context, tc *tasks.Context, in researchIn) (*protocol.CallToolResult, error) {
			time.Sleep(300 * time.Millisecond) // pretend to work

			// Park the task in input_required until the client answers.
			responses, err := tc.RequireInput(ctx, protocol.InputRequests{
				"depth": protocol.NewElicitFormRequest("How deep should the research go?", protocol.JSONSchema{
					"type": "object",
					"properties": map[string]any{
						"depth": map[string]any{"type": "string", "enum": []string{"quick", "thorough"}},
					},
					"required": []string{"depth"},
				}),
			})
			if err != nil {
				return nil, err
			}
			er, err := responses.Elicit("depth")
			if err != nil {
				return nil, err
			}
			depth, _ := er.Content["depth"].(string)

			time.Sleep(300 * time.Millisecond) // more work
			return protocol.NewToolResultText(fmt.Sprintf("%s research on %q finished", depth, in.Topic)), nil
		})

	// Declaring the capability is what makes the server return a task instead
	// of executing synchronously.
	copts := &client.Options{Info: &protocol.Implementation{Name: "task-client", Version: "1.0.0"}}
	tasks.EnableClient(copts) // declares the capability and the tasks routing headers
	c := client.New(mem.New(srv), copts)
	defer c.Close()

	ctx := context.Background()
	_, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "research", Arguments: map[string]any{"topic": "MCP"}})
	task, ok := tasks.AsTask(err)
	if !ok {
		log.Fatalf("expected a task, got: %v", err)
	}
	fmt.Printf("task %s created (%s)\n", task.TaskID, task.Status)

	final, err := tasks.Await(ctx, c, task, func(ctx context.Context, reqs protocol.InputRequests) (protocol.InputResponses, error) {
		p, _ := reqs["depth"].Elicit()
		fmt.Printf("task asks: %s -> answering \"thorough\"\n", p.Message)
		r := protocol.InputResponses{}
		_ = r.Set("depth", protocol.ElicitResult{
			Action:  protocol.ElicitActionAccept,
			Content: map[string]any{"depth": "thorough"},
		})
		return r, nil
	})
	if err != nil {
		log.Fatal(err)
	}

	res, err := final.ToolResult()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("task %s: %s\n", final.Status, res.Content[0].(protocol.TextContent).Text)
}
