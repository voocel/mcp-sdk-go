package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	ctx := context.Background()
	mcpClient := client.NewClient(&client.ClientInfo{
		Name:    "chatbot-client",
		Version: "1.0.0",
	}, nil)

	transport, err := sse.NewSSETransport("http://localhost:8082")
	if err != nil {
		log.Fatalf("Failed to create transport: %v", err)
	}

	fmt.Println("Connecting to chatbot service...")
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer session.Close()

	initResult := session.InitializeResult()
	fmt.Printf("Connected successfully! Server: %s v%s\n",
		initResult.ServerInfo.Name, initResult.ServerInfo.Version)

	fmt.Print("\nPlease enter your name: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	username := strings.TrimSpace(scanner.Text())
	if username == "" {
		username = "Friend"
	}

	// Get greeting
	result, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "greeting",
		Arguments: map[string]any{
			"name": username,
		},
	})
	if err != nil {
		log.Fatalf("Failed to call greeting tool: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Println("\n" + textContent.Text)
		}
	}

	// Get chat template
	promptResult, err := session.GetPrompt(ctx, &protocol.GetPromptParams{
		Name: "chat_template",
		Arguments: map[string]string{
			"username": username,
		},
	})
	if err != nil {
		log.Fatalf("Failed to get chat template: %v", err)
	}

	// Display assistant's welcome message
	if len(promptResult.Messages) >= 3 {
		assistantMessage := promptResult.Messages[len(promptResult.Messages)-1]
		if textContent, ok := assistantMessage.Content.(protocol.TextContent); ok {
			fmt.Println(textContent.Text)
		}
	}

	fmt.Println("\nYou can enter the following commands:")
	fmt.Println("- 'weather [city]' to check weather")
	fmt.Println("- 'translate [text] to [zh/en]' to translate text")
	fmt.Println("- 'exit' to quit chat")
	fmt.Println()

	for {
		fmt.Print("> ")
		scanner.Scan()
		input := strings.TrimSpace(scanner.Text())

		if input == "exit" || input == "quit" {
			break
		}

		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "weather ") {
			// Handle weather query
			var city string
			if strings.HasPrefix(input, "weather ") {
				city = strings.TrimPrefix(input, "weather ")
			}

			city = strings.TrimSpace(city)
			if city == "" {
				fmt.Println("Please specify a city name")
				continue
			}

			result, err := session.CallTool(ctx, &protocol.CallToolParams{
				Name: "weather",
				Arguments: map[string]any{
					"city": city,
				},
			})
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			if len(result.Content) > 0 {
				if textContent, ok := result.Content[0].(protocol.TextContent); ok {
					fmt.Printf(" %s\n", textContent.Text)
				}
			}
		} else if strings.Contains(input, " to ") {
			// Handle translation request
			parts := strings.Split(input, " to ")
			if len(parts) != 2 || !strings.HasPrefix(parts[0], "translate ") {
				fmt.Println("Invalid format. Please use: translate [text] to [zh/en]")
				continue
			}

			text := strings.TrimSpace(strings.TrimPrefix(parts[0], "translate "))
			targetLang := strings.TrimSpace(parts[1])

			if text == "" {
				fmt.Println("Please provide text to translate")
				continue
			}

			if targetLang != "zh" && targetLang != "en" {
				fmt.Println("Target language must be 'zh' or 'en'")
				continue
			}

			result, err := session.CallTool(ctx, &protocol.CallToolParams{
				Name: "translate",
				Arguments: map[string]any{
					"text":        text,
					"target_lang": targetLang,
				},
			})
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			if result.IsError && len(result.Content) > 0 {
				if textContent, ok := result.Content[0].(protocol.TextContent); ok {
					fmt.Printf("%s\n", textContent.Text)
				}
			} else if len(result.Content) > 0 {
				if textContent, ok := result.Content[0].(protocol.TextContent); ok {
					fmt.Printf("Translation result: %s\n", textContent.Text)
				}
			}
		} else if input == "help" {
			// Display help information
			fmt.Println("Available commands:")
			fmt.Println("  - weather [city] - Check weather for specified city")
			fmt.Println("  - translate [text] to [zh/en] - Translate between Chinese and English")
			fmt.Println("  - help - Display this help information")
			fmt.Println("  - exit - Exit the program")
		} else {
			// Unrecognized command
			fmt.Printf("Unknown command: '%s'\n", input)
			fmt.Println("Please try 'weather [city]', 'translate [text] to [zh/en]', 'help' or 'exit'")
		}
	}

	fmt.Println("\nGoodbye!")
}
