// A calculator MCP server showing typed tools: the input schema (with enum and
// required constraints) is inferred from the Go struct, arguments are
// validated against it before the handler runs, and the output schema is
// derived from the return type.
//
// Run: go run ./examples/calculator
package main

import (
	"context"
	"errors"
	"log"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

type calcIn struct {
	Op string  `json:"op" jsonschema:"required,enum=add,enum=sub,enum=mul,enum=div"`
	A  float64 `json:"a" jsonschema:"required"`
	B  float64 `json:"b" jsonschema:"required"`
}

type calcOut struct {
	Result float64 `json:"result"`
}

func main() {
	srv := server.New(&server.Options{
		Impl: protocol.Implementation{Name: "calculator", Version: "1.0.0"},
	})

	server.AddTool(srv, &protocol.Tool{Name: "calculate", Description: "Basic arithmetic"},
		func(ctx context.Context, req *server.CallRequest, in calcIn) (protocol.ToolResponse, calcOut, error) {
			var r float64
			switch in.Op {
			case "add":
				r = in.A + in.B
			case "sub":
				r = in.A - in.B
			case "mul":
				r = in.A * in.B
			case "div":
				if in.B == 0 {
					// A plain error becomes a tool error (isError: true).
					return nil, calcOut{}, errors.New("division by zero")
				}
				r = in.A / in.B
			}
			return nil, calcOut{Result: r}, nil
		})

	if err := stdio.Serve(context.Background(), srv, nil); err != nil {
		log.Fatal(err)
	}
}
