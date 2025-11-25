package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/streamable"
)

// ========== Generic API Type Definitions ==========

// UserInfoInput user information query input (Generic API)
type UserInfoInput struct {
	UserID string `json:"user_id" jsonschema:"required,description=User ID"`
}

// Address address information
type Address struct {
	City    string `json:"city" jsonschema:"required,description=City"`
	Country string `json:"country" jsonschema:"required,description=Country"`
	Zipcode string `json:"zipcode" jsonschema:"required,description=Zipcode"`
}

// Metadata user metadata
type Metadata struct {
	CreatedAt    string `json:"created_at" jsonschema:"required,description=Creation time"`
	LastLogin    string `json:"last_login" jsonschema:"required,description=Last login time"`
	ProfileViews int    `json:"profile_views" jsonschema:"required,description=Profile view count"`
	IsVerified   bool   `json:"is_verified" jsonschema:"required,description=Verification status"`
}

// UserInfoOutput user information output (Generic API)
type UserInfoOutput struct {
	UserID   string   `json:"user_id" jsonschema:"required,description=User ID"`
	Name     string   `json:"name" jsonschema:"required,description=Name"`
	Age      int      `json:"age" jsonschema:"required,description=Age"`
	Email    string   `json:"email" jsonschema:"required,description=Email"`
	Address  Address  `json:"address" jsonschema:"required,description=Address information"`
	Skills   []string `json:"skills" jsonschema:"required,description=Skills list"`
	Metadata Metadata `json:"metadata" jsonschema:"required,description=Metadata"`
}

var (
	serverStartTime = time.Now()
	requestCounter  = 0
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalCh
		log.Println("Received shutdown signal")
		cancel()
	}()

	mcpServer := server.NewServer(&protocol.ServerInfo{
		Name:    "Streamable HTTP Demo Service",
		Version: "1.0.0",
	}, nil)

	// Register greeting tool
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
					"language": map[string]interface{}{
						"type":        "string",
						"description": "Language (optional)",
					},
				},
				"required": []string{"name"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			name, ok := req.Params.Arguments["name"].(string)
			if !ok {
				return protocol.NewToolResultError("Parameter 'name' must be a string"), nil
			}

			language, _ := req.Params.Arguments["language"].(string)
			var greeting string

			switch language {
			case "zh", "chinese":
				greeting = fmt.Sprintf("Hello, %s! Welcome to Streamable HTTP transport! (Chinese mode)", name)
			case "en", "english":
				greeting = fmt.Sprintf("Hello, %s! Welcome to Streamable HTTP transport!", name)
			default:
				greeting = fmt.Sprintf("Hello, %s! Welcome to Streamable HTTP transport!", name)
			}

			return protocol.NewToolResultText(greeting), nil
		},
	)

	// Register calculation tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "calculate",
			Description: "Perform mathematical calculations",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type":        "string",
						"description": "Operation type (add, subtract, multiply, divide)",
					},
					"a": map[string]interface{}{
						"type":        "number",
						"description": "First number",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "Second number",
					},
				},
				"required": []string{"operation", "a", "b"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			requestCounter++

			operation, _ := req.Params.Arguments["operation"].(string)
			a, _ := req.Params.Arguments["a"].(float64)
			b, _ := req.Params.Arguments["b"].(float64)

			var result float64
			var opSymbol string

			switch operation {
			case "add":
				result = a + b
				opSymbol = "+"
			case "subtract":
				result = a - b
				opSymbol = "-"
			case "multiply":
				result = a * b
				opSymbol = "*"
			case "divide":
				if b == 0 {
					return protocol.NewToolResultError("Divisor cannot be zero"), nil
				}
				result = a / b
				opSymbol = "/"
			default:
				return protocol.NewToolResultError("Unsupported operation type"), nil
			}

			resultText := fmt.Sprintf("%.2f %s %.2f = %.2f (Request #%d, Time: %s)",
				a, opSymbol, b, result, requestCounter, time.Now().Format("15:04:05"))
			return protocol.NewToolResultText(resultText), nil
		},
	)

	// Register structured output tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "get_weather",
			Description: "Get weather information for a specified city (returns structured data)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"city"},
			},

			OutputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{
						"type":        "string",
						"description": "City name",
					},
					"temperature": map[string]interface{}{
						"type":        "number",
						"description": "Temperature (Celsius)",
					},
					"humidity": map[string]interface{}{
						"type":        "integer",
						"description": "Humidity (percentage)",
					},
					"condition": map[string]interface{}{
						"type":        "string",
						"description": "Weather condition",
					},
					"wind_speed": map[string]interface{}{
						"type":        "number",
						"description": "Wind speed (km/h)",
					},
					"timestamp": map[string]interface{}{
						"type":        "string",
						"description": "Query time",
					},
				},
				"required": []string{"city", "temperature", "humidity", "condition"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			requestCounter++
			city, _ := req.Params.Arguments["city"].(string)

			weatherData := map[string]interface{}{
				"city":        city,
				"temperature": 22.5 + float64(requestCounter%10), // Simulate variation
				"humidity":    65 + requestCounter%20,
				"condition":   []string{"Sunny", "Cloudy", "Light Rain", "Overcast"}[requestCounter%4],
				"wind_speed":  12.3,
				"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
			}

			// Return structured content (using StructuredContent)
			result := &protocol.CallToolResult{
				StructuredContent: weatherData,
				IsError:           false,
			}

			return result, nil
		},
	)

	// Register user info tool (using Generic API)
	server.AddTool(mcpServer, &protocol.Tool{
		Name:        "get_user_info",
		Description: "Get detailed user information (demonstrates Generic API - auto-generated Schema)",
	}, func(ctx context.Context, req *server.CallToolRequest, input UserInfoInput) (*protocol.CallToolResult, UserInfoOutput, error) {
		requestCounter++

		output := UserInfoOutput{
			UserID: input.UserID,
			Name:   "John Doe",
			Age:    28,
			Email:  fmt.Sprintf("user_%s@example.com", input.UserID),
			Address: Address{
				City:    "Beijing",
				Country: "China",
				Zipcode: "100000",
			},
			Skills: []string{"Go", "Python", "JavaScript"},
			Metadata: Metadata{
				CreatedAt:    "2025-01-15 10:30:00",
				LastLogin:    time.Now().Format("2006-01-02 15:04:05"),
				ProfileViews: 1234 + requestCounter,
				IsVerified:   true,
			},
		}

		return nil, output, nil
	})

	// Register server statistics resource
	mcpServer.AddResource(
		&protocol.Resource{
			URI:         "stats://server",
			Name:        "Server Statistics",
			Description: "Get server runtime statistics",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			uptime := time.Since(serverStartTime)
			stats := fmt.Sprintf(`Server Statistics:
Uptime: %s
Total Requests: %d
Protocol Version: %s
Start Time: %s`,
				uptime.Round(time.Second),
				requestCounter,
				protocol.MCPVersion,
				serverStartTime.Format("2006-01-02 15:04:05"))

			contents := protocol.NewTextResourceContents("stats://server", stats)
			return protocol.NewReadResourceResult(contents), nil
		},
	)

	handler := streamable.NewHTTPHandler(func(*http.Request) *server.Server {
		return mcpServer
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	httpServer := &http.Server{
		Addr:    ":8083",
		Handler: mux,
	}

	go func() {
		log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		log.Println("Streamable HTTP MCP Server Started")
		log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		log.Println("Endpoint: http://localhost:8081/mcp")
		log.Println("Transport: Streamable HTTP")
		log.Println("MCP Version: 2025-06-18")
		log.Println()
		log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		log.Println()

		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Closed!")
}
