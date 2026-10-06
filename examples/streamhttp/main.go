// Streamable HTTP demo: a server exposing MCP on a single POST endpoint, and
// a client mode that exercises it (discover, tool call with progress over SSE,
// and a live subscription stream).
//
// Terminal 1: go run ./examples/streamhttp-demo
// Terminal 2: go run ./examples/streamhttp-demo client
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/streamhttp"
)

const addr = "localhost:8080"

type countIn struct {
	Steps int `json:"steps" jsonschema:"required,minimum=1,maximum=10"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "client" {
		runClient()
		return
	}
	runServer()
}

func runServer() {
	srv := server.New(&server.Options{
		Impl: protocol.Implementation{Name: "streamhttp-demo", Version: "1.0.0"},
	})

	server.AddTool(srv, &protocol.Tool{Name: "count", Description: "Count with progress"},
		func(ctx context.Context, req *server.CallRequest, in countIn) (protocol.ToolResponse, string, error) {
			for i := 1; i <= in.Steps; i++ {
				// Progress notifications turn the HTTP response into an SSE stream.
				_ = req.ReportProgress(ctx, float64(i), float64(in.Steps), fmt.Sprintf("step %d", i))
				time.Sleep(200 * time.Millisecond)
			}
			return nil, fmt.Sprintf("counted to %d", in.Steps), nil
		})

	server.AddTool(srv, &protocol.Tool{Name: "touch", Description: "Trigger a resource update"},
		func(ctx context.Context, req *server.CallRequest, _ struct{}) (protocol.ToolResponse, string, error) {
			srv.ResourceUpdated("demo://ticker")
			return nil, "updated", nil
		})

	// Subscriptions to a resource are only honored when resources exist.
	srv.AddResource(&protocol.Resource{URI: "demo://ticker", Name: "ticker"},
		func(ctx context.Context, req *server.ResourceRequest) (protocol.ResourceResponse, error) {
			return protocol.NewReadResourceResult(protocol.NewTextResourceContents("demo://ticker", "tick")), nil
		})

	// Origin validation is on by default (localhost origins and requests
	// without an Origin header are allowed; browser frontends on other
	// domains need Options.AllowedOrigins).
	mux := http.NewServeMux()
	mux.Handle("/mcp", streamhttp.NewHandler(srv, nil))
	hs := &http.Server{Addr: addr, Handler: mux}

	// Graceful shutdown: end the subscription streams first (http.Server.Shutdown
	// would wait on them forever), then drain the HTTP server.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		_ = hs.Shutdown(sctx)
	}()

	log.Printf("MCP server on http://%s/mcp", addr)
	if err := hs.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
	<-drained
}

func runClient() {
	ctx := context.Background()
	c := client.New(streamhttp.New("http://"+addr+"/mcp", nil), &client.Options{
		Info: &protocol.Implementation{Name: "demo-client", Version: "1.0.0"},
		OnProgress: func(p *protocol.ProgressParams) {
			fmt.Printf("  progress %.0f/%.0f %s\n", p.Progress, p.Total, p.Message)
		},
	})
	defer c.Close()

	disc, err := c.Discover(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("connected to %s (protocol %s)\n", disc.Meta.ServerInfo.Name, disc.SupportedVersions[0])

	fmt.Println("calling count(5):")
	res, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "count", Arguments: map[string]any{"steps": 5}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  => %v\n", res.StructuredContent)

	fmt.Println("subscribing to demo://ticker, then touching it:")
	sub, err := c.Listen(ctx, protocol.SubscriptionFilter{ResourceSubscriptions: []string{"demo://ticker"}})
	if err != nil {
		log.Fatal(err)
	}
	defer sub.Close()
	if _, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "touch"}); err != nil {
		log.Fatal(err)
	}
	select {
	case ev := <-sub.Events():
		if uri, ok := ev.ResourceUpdated(); ok {
			fmt.Printf("  resource updated: %s\n", uri)
		}
	case <-time.After(5 * time.Second):
		log.Fatal("no subscription event")
	}
}
