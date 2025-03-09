package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
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

	mcp := server.New("Calculator Service", "1.0.0")

	mcp.Tool("add", "Add two numbers").
		WithNumberParam("a", "First number", true).
		WithNumberParam("b", "Second number", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			a := args["a"].(float64)
			b := args["b"].(float64)
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", a+b)), nil
		})

	mcp.Tool("subtract", "Subtract one number from another").
		WithNumberParam("a", "Minuend", true).
		WithNumberParam("b", "Subtrahend", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			a := args["a"].(float64)
			b := args["b"].(float64)
			return protocol.NewToolResultText(fmt.Sprintf("%.2f", a-b)), nil
		})

	mcp.AddTool("multiply", func(a, b float64) float64 {
		return a * b
	}, "Multiply two numbers")

	mcp.Tool("divide", "Divide one number by another").
		WithNumberParam("a", "Dividend", true).
		WithNumberParam("b", "Divisor", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			a := args["a"].(float64)
			b := args["b"].(float64)

			if b == 0 {
				return nil, fmt.Errorf("Cannot divide by zero")
			}

			return protocol.NewToolResultText(fmt.Sprintf("%.2f", a/b)), nil
		})

	mcp.Prompt("calculator_help", "Calculator help information").
		Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(
					"This is a simple calculator service supporting the four basic operations: addition, subtraction, multiplication, and division.")),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					"How do I use this calculator?")),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					"Use the add, subtract, multiply, and divide tools to perform calculations. Each tool accepts two parameters: a and b.")),
			}
			return protocol.NewGetPromptResult("calculator_help", messages), nil
		})

	log.Println("Starting WebSocket server on :8080...")
	if err := mcp.ServeWebSocket(ctx, ":8080"); err != nil && err != context.Canceled {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server closed")
}
