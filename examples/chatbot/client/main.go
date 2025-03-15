package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/voocel/mcp-sdk-go/client"
)

func main() {
	ctx := context.Background()

	c, err := client.New(client.WithWebSocketTransport("ws://localhost:8082"))
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	if err := c.Initialize(ctx); err != nil {
		log.Fatalf("Initialization failed: %v", err)
	}

	fmt.Println("Successfully connected to chatbot server!")

	fmt.Print("Please enter your name: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	username := scanner.Text()

	result, err := c.CallTool(ctx, "greeting", map[string]interface{}{
		"name": username,
	})
	if err != nil {
		log.Fatalf("Failed to call greeting tool: %v", err)
	}
	fmt.Println("\n" + result.Content[0].(map[string]interface{})["text"].(string))

	prompt, err := c.GetPrompt(ctx, "chat_template", map[string]string{
		"username": username,
	})
	if err != nil {
		log.Fatalf("Failed to get chat template: %v", err)
	}

	assistantMessage := prompt.Messages[len(prompt.Messages)-1]
	assistantContent := assistantMessage.Content.(map[string]interface{})
	fmt.Println(assistantContent["text"].(string))

	fmt.Println("\nYou can enter the following commands:")
	fmt.Println("- 'weather [city]' to check weather")
	fmt.Println("- 'translate [text] to [zh/en]' to translate text")
	fmt.Println("- 'exit' to quit chat")
	fmt.Println("")

	for {
		fmt.Print("> ")
		scanner.Scan()
		input := scanner.Text()

		if input == "exit" {
			break
		}

		if strings.HasPrefix(input, "weather ") {
			city := strings.TrimPrefix(input, "weather ")
			result, err := c.CallTool(ctx, "weather", map[string]interface{}{
				"city": city,
			})
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			fmt.Println(result.Content[0].(map[string]interface{})["text"].(string))
		} else if strings.Contains(input, " to ") {
			parts := strings.Split(input, " to ")
			if len(parts) != 2 || !strings.HasPrefix(parts[0], "translate ") {
				fmt.Println("Format error. Please use: translate [text] to [zh/en]")
				continue
			}

			text := strings.TrimPrefix(parts[0], "translate ")
			targetLang := parts[1]

			result, err := c.CallTool(ctx, "translate", map[string]interface{}{
				"text":        text,
				"target_lang": targetLang,
			})
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			fmt.Println(result.Content[0].(map[string]interface{})["text"].(string))
		} else {
			fmt.Println("I don't understand this command. Please try 'weather [city]', 'translate [text] to [zh/en]', or 'exit'.")
		}
	}

	fmt.Println("Goodbye!")
}
