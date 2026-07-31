// A file-serving MCP server: files in a directory are exposed as resources
// under file:/// URIs, with path traversal blocked by the SDK
// (filepath.Localize + os.OpenRoot).
//
// Run: go run ./examples/file-server -dir .
package main

import (
	"context"
	"flag"
	"log"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

func main() {
	dir := flag.String("dir", ".", "directory to serve")
	flag.Parse()

	srv := server.New(&server.Options{
		Impl: protocol.Implementation{Name: "file-server", Version: "1.0.0"},
	})

	// The template advertises the URI shape; {path} matches a single path
	// segment, so this serves the files directly inside -dir.
	srv.AddResourceTemplate(&protocol.ResourceTemplate{
		URITemplate: "file:///{path}",
		Name:        "files",
		Description: "Files served from " + *dir,
	}, server.FileResourceHandler(*dir))

	if err := stdio.Serve(context.Background(), srv, nil); err != nil {
		log.Fatal(err)
	}
}
