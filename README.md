# MCP Go SDK

<div align="center">

<strong>An elegant and efficient Go implementation of the Model Context Protocol (MCP)</strong>

[![English](https://img.shields.io/badge/lang-English-blue.svg)](./README.md)
[![中文](https://img.shields.io/badge/lang-中文-red.svg)](./README_CN.md)

![License](https://img.shields.io/badge/license-MIT-blue.svg)
[![Go Reference](https://pkg.go.dev/badge/github.com/voocel/mcp-sdk-go.svg)](https://pkg.go.dev/github.com/voocel/mcp-sdk-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/voocel/mcp-sdk-go)](https://goreportcard.com/report/github.com/voocel/mcp-sdk-go)
[![Build Status](https://github.com/voocel/mcp-sdk-go/workflows/go/badge.svg)](https://github.com/voocel/mcp-sdk-go/actions)

</div>

<div align="center">

**Build Smarter Applications, Connect Powerful Models**
*Easily integrate large language model capabilities with MCP Go SDK*

</div>

## Introduction

MCP Go SDK is a Go implementation of the Model Context Protocol, fully supporting the latest **MCP 2025-06-18** specification, while maintaining backward compatibility with **MCP 2025-03-26** and **MCP 2024-11-05**.

## Core Features

- **Fully MCP Compliant** - Supports latest MCP 2025-06-18 spec, backward compatible with 2025-03-26, 2024-11-05
- **Elegant Architecture** - Client/Server + Session pattern, high cohesion and low coupling
- **Server SDK** - Quickly build MCP servers with tools, resources, and prompt templates
- **Client SDK** - Complete client implementation for connecting to any MCP-compatible server
- **Multiple Transport Protocols** - STDIO (recommended), Streamable HTTP (latest), SSE (backward compatible)
- **Multi-Session Support** - Both Server and Client can manage multiple concurrent connections
- **High Performance** - Concurrency-safe with optimized message processing
- **Security Protection** - Built-in input validation, path traversal protection, resource limits

## MCP Protocol Version Support

This SDK tracks and supports the latest developments in the MCP protocol, ensuring compatibility with the ecosystem:

### Supported Versions

| Version | Release Date | Key Features | Support Status |
|---------|--------------|--------------|----------------|
| **2025-06-18** | June 2025 | Structured tool output, tool annotations, **Elicitation user interaction**, **Sampling LLM inference** | **Fully Supported** |
| **2025-03-26** | March 2025 | OAuth 2.1 authorization, Streamable HTTP, JSON-RPC batching | **Fully Supported** |
| **2024-11-05** | November 2024 | HTTP+SSE transport, basic tools and resources | **Fully Supported** |

### Latest Features (2025-06-18)

- **Structured Tool Output**: Tools can return typed JSON data for programmatic processing
- **Tool Annotations**: Describe tool behavior characteristics (read-only, destructive, caching strategy, etc.)
- **User Interaction Requests**: Tools can proactively request user input or confirmation
- **Resource Links**: Support for associations and references between resources
- **Protocol Version Header**: HTTP transport requires `MCP-Protocol-Version` header
- **Extended Metadata (_meta)**: Add custom metadata to tools, resources, and prompts

### Major Change History

**2025-03-26 → 2025-06-18**:

- Added structured tool output support
- Enhanced tool annotation system
- Added user interaction request mechanism
- Support for resource linking functionality
- Added `_meta` field for extended metadata

**2024-11-05 → 2025-03-26**:

- Introduced OAuth 2.1 authorization framework
- Replaced HTTP+SSE with Streamable HTTP
- Added JSON-RPC batching support
- Added audio content type support

## Installation

```bash
go get github.com/voocel/mcp-sdk-go
```

## Quick Start

### Server Side - STDIO Transport (Recommended)

The simplest way is to use STDIO transport, suitable for command-line tools and Claude Desktop integration:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/voocel/mcp-sdk-go/protocol"
    "github.com/voocel/mcp-sdk-go/server"
    "github.com/voocel/mcp-sdk-go/transport/stdio"
)

func main() {
    ctx := context.Background()

    // Create MCP server
    mcpServer := server.NewServer(&protocol.ServerInfo{
        Name:    "Quick Start Server",
        Version: "1.0.0",
    }, nil)

    // Register a simple greeting tool
    mcpServer.AddTool(
        &protocol.Tool{
            Name:        "greet",
            Description: "Greet the user",
            InputSchema: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "name": map[string]interface{}{
                        "type":        "string",
                        "description": "User name",
                    },
                },
                "required": []string{"name"},
            },
        },
        func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
            name := req.Params.Arguments["name"].(string)
            greeting := fmt.Sprintf("Hello, %s! Welcome to MCP Go SDK!", name)
            return protocol.NewToolResultText(greeting), nil
        },
    )

    // Run server with STDIO transport
    if err := mcpServer.Run(ctx, &stdio.StdioTransport{}); err != nil {
        log.Fatal(err)
    }
}
```

### Server Side - HTTP Transport

Build web services using Streamable HTTP transport:

```go
package main

import (
    "context"
    "log"
    "net/http"

    "github.com/voocel/mcp-sdk-go/protocol"
    "github.com/voocel/mcp-sdk-go/server"
    "github.com/voocel/mcp-sdk-go/transport/streamable"
)

func main() {
    // Create MCP server
    mcpServer := server.NewServer(&protocol.ServerInfo{
        Name:    "HTTP Server",
        Version: "1.0.0",
    }, nil)

    // Register tools...
    mcpServer.AddTool(...)

    // Create HTTP handler
    handler := streamable.NewHTTPHandler(func(*http.Request) *server.Server {
        return mcpServer
    })

    // Start HTTP server
    log.Println("Server started at http://localhost:8081")
    if err := http.ListenAndServe(":8081", handler); err != nil {
        log.Fatal(err)
    }
}
```

### Client Side - Connect to MCP Server

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os/exec"

    "github.com/voocel/mcp-sdk-go/client"
    "github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
    ctx := context.Background()

    // Create client
    mcpClient := client.NewClient(&client.ClientInfo{
        Name:    "Demo Client",
        Version: "1.0.0",
    }, nil)

    // Connect to server via STDIO (launch subprocess)
    transport := client.NewCommandTransport(exec.Command("./server"))
    session, err := mcpClient.Connect(ctx, transport, nil)
    if err != nil {
        log.Fatalf("Connection failed: %v", err)
    }
    defer session.Close()

    fmt.Printf("Connected successfully! Server: %s v%s\n",
        session.ServerInfo().Name, session.ServerInfo().Version)

    // List available tools
    tools, err := session.ListTools(ctx, nil)
    if err != nil {
        log.Fatalf("Failed to list tools: %v", err)
    }

    for _, tool := range tools.Tools {
        fmt.Printf("Tool: %s - %s\n", tool.Name, tool.Description)
    }

    // Call tool
    result, err := session.CallTool(ctx, &protocol.CallToolParams{
        Name: "greet",
        Arguments: map[string]interface{}{
            "name": "Go Developer",
        },
    })
    if err != nil {
        log.Fatalf("Failed to call tool: %v", err)
    }

    if len(result.Content) > 0 {
        if textContent, ok := result.Content[0].(protocol.TextContent); ok {
            fmt.Printf("Result: %s\n", textContent.Text)
        }
    }

    // Read resource
    resource, err := session.ReadResource(ctx, &protocol.ReadResourceParams{
        URI: "info://server",
    })
    if err != nil {
        log.Fatalf("Failed to read resource: %v", err)
    }

    if len(resource.Contents) > 0 {
        fmt.Printf("Server info: %s\n", resource.Contents[0].Text)
    }
}
```

## Example Projects

| Example | Description | Transport | Features |
|---------|-------------|-----------|----------|
| [**Basic**](./examples/basic/) | **Complete comprehensive example** | STDIO | All core features + client |
| [Calculator](./examples/calculator/) | Math calculator service | STDIO | Tools, resources |
| [SSE Demo](./examples/sse-demo/) | SSE transport demo | SSE | SSE transport |
| [Chatbot](./examples/chatbot/) | Chatbot service | SSE | Conversational interaction |
| [File Server](./examples/file-server/) | File operation service | SSE | File operations |
| [Streamable Demo](./examples/streamable-demo/) | Streamable HTTP demo | Streamable HTTP | Streaming transport |

**Recommended to start with Basic example**: Complete demonstration of all core features, including server and client implementations.

**How to run**:

```bash
# Server
cd examples/basic && go run main.go

# Client
cd examples/basic/client && go run main.go
```

## Core Architecture

### Server Side API

```go
// Create MCP server
mcpServer := server.NewServer(&protocol.ServerInfo{
    Name:    "My Server",
    Version: "1.0.0",
}, nil)

// Register tool
mcpServer.AddTool(
    &protocol.Tool{
        Name:        "greet",
        Description: "Greet the user",
        InputSchema: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "name": map[string]interface{}{
                    "type":        "string",
                    "description": "User name",
                },
            },
            "required": []string{"name"},
        },
    },
    func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
        name := req.Params.Arguments["name"].(string)
        return protocol.NewToolResultText(fmt.Sprintf("Hello, %s!", name)), nil
    },
)

// Register resource
mcpServer.AddResource(
    &protocol.Resource{
        URI:         "info://server",
        Name:        "Server Info",
        Description: "Get basic server information",
        MimeType:    "text/plain",
    },
    func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
        contents := protocol.NewTextResourceContents("info://server", "Server information content")
        return protocol.NewReadResourceResult(contents), nil
    },
)

// Register resource template
mcpServer.AddResourceTemplate(
    &protocol.ResourceTemplate{
        URITemplate: "log://app/{date}",
        Name:        "Application Logs",
        Description: "Get application logs for a specific date",
        MimeType:    "text/plain",
    },
    func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
        // Extract parameters from URI
        date := extractDateFromURI(req.Params.URI)
        contents := protocol.NewTextResourceContents(req.Params.URI, fmt.Sprintf("Log content: %s", date))
        return protocol.NewReadResourceResult(contents), nil
    },
)

// Register prompt template
mcpServer.AddPrompt(
    &protocol.Prompt{
        Name:        "code_review",
        Description: "Code review prompt",
        Arguments: []protocol.PromptArgument{
            {Name: "language", Description: "Programming language", Required: true},
            {Name: "code", Description: "Code content", Required: true},
        },
    },
    func(ctx context.Context, req *server.GetPromptRequest) (*protocol.GetPromptResult, error) {
        language := req.Params.Arguments["language"]
        code := req.Params.Arguments["code"]

        messages := []protocol.PromptMessage{
            protocol.NewPromptMessage(protocol.RoleUser,
                protocol.NewTextContent(fmt.Sprintf("Please review this %s code:\n%s", language, code))),
        }
        return protocol.NewGetPromptResult("Code Review", messages...), nil
    },
)

// Run server (STDIO)
if err := mcpServer.Run(ctx, &stdio.StdioTransport{}); err != nil {
    log.Fatal(err)
}

// Or use HTTP transport
handler := streamable.NewHTTPHandler(func(r *http.Request) *server.Server {
    return mcpServer
})
http.ListenAndServe(":8081", handler)
```

### Client Side API

```go
// Create client
mcpClient := client.NewClient(&client.ClientInfo{
    Name:    "My Client",
    Version: "1.0.0",
}, nil)

// Connect via STDIO (launch subprocess)
transport := client.NewCommandTransport(exec.Command("./server"))
session, err := mcpClient.Connect(ctx, transport, nil)
if err != nil {
    log.Fatal(err)
}
defer session.Close()

// List tools
tools, err := session.ListTools(ctx, nil)
for _, tool := range tools.Tools {
    fmt.Printf("Tool: %s\n", tool.Name)
}

// Call tool
result, err := session.CallTool(ctx, &protocol.CallToolParams{
    Name:      "greet",
    Arguments: map[string]interface{}{"name": "World"},
})

// List resources
resources, err := session.ListResources(ctx, nil)
for _, res := range resources.Resources {
    fmt.Printf("Resource: %s\n", res.URI)
}

// Read resource
resource, err := session.ReadResource(ctx, &protocol.ReadResourceParams{
    URI: "info://server",
})

// Get prompt
prompt, err := session.GetPrompt(ctx, &protocol.GetPromptParams{
    Name: "code_review",
    Arguments: map[string]string{
        "language": "Go",
        "code":     "func main() { ... }",
    },
})
```

### Advanced Features

#### Resource Templates

```go
// Server side: register resource template
mcpServer.AddResourceTemplate(
    &protocol.ResourceTemplate{
        URITemplate: "log://app/{date}",
        Name:        "Application Logs",
        Description: "Get application logs for a specific date",
    },
    func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
        // Handle dynamic resource request
        return protocol.NewReadResourceResult(contents), nil
    },
)

// Client side: list resource templates
templates, err := session.ListResourceTemplates(ctx, nil)
for _, tpl := range templates.ResourceTemplates {
    fmt.Printf("Template: %s\n", tpl.URITemplate)
}

// Read specific resource
resource, err := session.ReadResource(ctx, &protocol.ReadResourceParams{
    URI: "log://app/2025-01-15",
})
```

#### Roots Management

```go
// Client side: set roots
mcpClient := client.NewClient(&client.ClientInfo{
    Name:    "Client",
    Version: "1.0.0",
}, &client.ClientOptions{
    Roots: []*protocol.Root{
        protocol.NewRoot("file:///home/user/projects", "Projects Directory"),
        protocol.NewRoot("file:///home/user/documents", "Documents Directory"),
    },
})

// Server side: request client roots list
// Note: Must be called within ServerSession
rootsList, err := session.ListRoots(ctx)
for _, root := range rootsList.Roots {
    fmt.Printf("Root: %s - %s\n", root.URI, root.Name)
}
```

#### Sampling (LLM Inference)

```go
// Client side: set Sampling handler
mcpClient := client.NewClient(&client.ClientInfo{
    Name:    "Client",
    Version: "1.0.0",
}, &client.ClientOptions{
    SamplingHandler: func(ctx context.Context, req *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
        // Call actual LLM API
        response := callLLMAPI(req.Messages)
        return protocol.NewCreateMessageResult(
            protocol.RoleAssistant,
            protocol.NewTextContent(response),
            "gpt-4",
            protocol.StopReasonEndTurn,
        ), nil
    },
})

// Server side: initiate Sampling request
// Note: Must be called within ServerSession
result, err := session.CreateMessage(ctx, &protocol.CreateMessageRequest{
    Messages: []protocol.SamplingMessage{
        {Role: protocol.RoleUser, Content: protocol.NewTextContent("Calculate 2+2")},
    },
    MaxTokens: 100,
})
```

## Transport Protocols

**Fully compliant with MCP 2025-06-18 specification**, backward compatible with MCP 2025-03-26, 2024-11-05

### Supported Transports

| Protocol | Use Case | Official Support | Protocol Version |
|----------|----------|------------------|------------------|
| **STDIO** | Subprocess communication | Official standard | 2024-11-05+ |
| **SSE** | Web applications | Official standard | 2024-11-05+ |
| **Streamable HTTP** | Modern web applications | Official standard | 2025-06-18 |
| ~~**WebSocket**~~ | ~~Real-time applications~~ | Unofficial | - |
| ~~**gRPC**~~ | ~~Microservices~~ | Unofficial | - |

### STDIO Transport (Recommended)

```go
// Server side
mcpServer.Run(ctx, &stdio.StdioTransport{})

// Client side (launch subprocess)
transport := client.NewCommandTransport(exec.Command("./server"))
session, err := mcpClient.Connect(ctx, transport, nil)
```

### Streamable HTTP Transport (Web Applications)

```go
// Server side
handler := streamable.NewHTTPHandler(func(r *http.Request) *server.Server {
    return mcpServer
})
http.ListenAndServe(":8081", handler)

// Client side
transport, err := streamable.NewStreamableTransport("http://localhost:8081/mcp")
session, err := mcpClient.Connect(ctx, transport, nil)
```

### SSE Transport (Backward Compatible)

```go
// Server side
handler := sse.NewHTTPHandler(func(r *http.Request) *server.Server {
    return mcpServer
})
http.ListenAndServe(":8080", handler)

// Client side
transport, err := sse.NewSSETransport("http://localhost:8080")
session, err := mcpClient.Connect(ctx, transport, nil)
```

## Development Guide

### Learning Path

1. **Quick Start** → Understand basic concepts
2. [**Basic Example**](./examples/basic/) → Complete feature demonstration
3. [**Streamable Demo**](./examples/streamable-demo/) → HTTP transport
4. [**Client Example**](./examples/client-example/) → Client development

## Contributing

We welcome all forms of contributions!

1. **Report Bugs** - Submit issues describing problems
2. **Feature Suggestions** - Propose new feature ideas
3. **Improve Documentation** - Enhance documentation and examples
4. **Code Contributions** - Submit Pull Requests

Please see [Contributing Guide](CONTRIBUTING.md) for details.

## License

MIT License - See [LICENSE](LICENSE) file for details

## Roadmap

### Completed (MCP 2025-06-18 Fully Supported)

**Core Architecture**:
- [x] **Client/Server + Session Pattern**
- [x] **Transport Abstraction Layer** - Unified Transport/Connection interface
- [x] **Multi-Session Support** - Both Server and Client support multiple concurrent connections

**Transport Protocols**:
- [x] **STDIO Transport** - Standard input/output, suitable for CLI and Claude Desktop
- [x] **Streamable HTTP Transport** - Latest HTTP transport protocol (MCP 2025-06-18)
- [x] **SSE Transport** - Backward compatible with legacy HTTP+SSE (MCP 2024-11-05)

**MCP 2025-06-18 Features**:
- [x] **Tools** - Complete tool registration and invocation
- [x] **Resources** - Resource management and subscription
- [x] **Resource Templates** - Dynamic resource URI templates
- [x] **Prompts** - Prompt template management
- [x] **Roots** - Client roots management
- [x] **Sampling** - LLM inference request support
- [x] **Progress Tracking** - Progress feedback for long-running operations
- [x] **Logging** - Structured log messages
- [x] **Request Cancellation** - Cancel long-running operations

### Planned

- [ ] **CLI Tool** - Command-line tool for developing, testing, and debugging MCP servers
- [ ] **OAuth 2.1 Authorization** - Enterprise-grade security authentication (MCP 2025-03-26)
- [ ] **Middleware System** - Request/response interception and processing
- [ ] **More Examples** - More example code for real-world scenarios

## Related Projects

- [MCP Official Specification](https://github.com/modelcontextprotocol/modelcontextprotocol) - Protocol specification definition
- [MCP Python SDK](https://github.com/modelcontextprotocol/python-sdk) - Python implementation
- [MCP TypeScript SDK](https://github.com/modelcontextprotocol/typescript-sdk) - TypeScript implementation

---
