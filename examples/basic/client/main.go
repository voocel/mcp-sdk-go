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
)

func main() {
	ctx := context.Background()
	mcpClient := client.NewClient(&client.ClientInfo{
		Name:    "BasicMCPClient",
		Version: "1.0.0",
	}, &client.ClientOptions{
		ElicitationHandler:   handleElicitation,
		CreateMessageHandler: handleSampling,
	})

	transport := client.NewCommandTransport("go", "run", "../main.go")

	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer session.Close()

	initResult := session.InitializeResult()

	log.Println("========================================")
	log.Println("MCP Basic Client Connected")
	log.Println("========================================")
	log.Printf("Server: %s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	log.Println("")

	demonstrateAllFeatures(ctx, session)
}

func demonstrateAllFeatures(ctx context.Context, session *client.ClientSession) {
	fmt.Println("\n========== List All Tools ==========")
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Printf("Failed to list tools: %v", err)
	} else {
		for _, tool := range tools.Tools {
			fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
			if tool.Title != "" {
				fmt.Printf("    Title: %s\n", tool.Title)
			}
			if len(tool.Meta) > 0 {
				fmt.Printf("    Metadata: %v\n", tool.Meta)
			}
		}
	}

	fmt.Println("\n========== Call Basic Tool (greet) ==========")
	result, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "greet",
		Arguments: map[string]interface{}{
			"name": "Alice",
		},
	})
	if err != nil {
		log.Printf("Failed to call tool: %v", err)
	} else {
		printToolResult(result)
	}

	fmt.Println("\n========== Call Tool with Metadata (calculate) ==========")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "calculate",
		Arguments: map[string]interface{}{
			"operation": "add",
			"a":         10,
			"b":         20,
		},
	})
	if err != nil {
		log.Printf("Failed to call tool: %v", err)
	} else {
		printToolResult(result)
	}

	fmt.Println("\n========== Call Tool with Output Schema (get_time) ==========")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name:      "get_time",
		Arguments: map[string]interface{}{},
	})
	if err != nil {
		log.Printf("Failed to call tool: %v", err)
	} else {
		printToolResult(result)
	}

	fmt.Println("\n========== List All Resources ==========")
	resources, err := session.ListResources(ctx, nil)
	if err != nil {
		log.Printf("Failed to list resources: %v", err)
	} else {
		for _, resource := range resources.Resources {
			fmt.Printf("  - %s (%s): %s\n", resource.Name, resource.URI, resource.Description)
			if len(resource.Meta) > 0 {
				fmt.Printf("    Metadata: %v\n", resource.Meta)
			}
		}
	}

	fmt.Println("\n========== Read Resource (info://server) ==========")
	resourceResult, err := session.ReadResource(ctx, &protocol.ReadResourceParams{
		URI: "info://server",
	})
	if err != nil {
		log.Printf("Failed to read resource: %v", err)
	} else {
		for _, content := range resourceResult.Contents {
			fmt.Printf("Content: %s\n", content.Text)
		}
	}

	fmt.Println("\n========== List Resource Templates ==========")
	templates, err := session.ListResourceTemplates(ctx, nil)
	if err != nil {
		log.Printf("Failed to list resource templates: %v", err)
	} else {
		for _, template := range templates.ResourceTemplates {
			fmt.Printf("  - %s (%s): %s\n", template.Name, template.URITemplate, template.Description)
			if len(template.Meta) > 0 {
				fmt.Printf("    Metadata: %v\n", template.Meta)
			}
		}
	}

	fmt.Println("\n========== List Prompt Templates ==========")
	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		log.Printf("Failed to list prompts: %v", err)
	} else {
		for _, prompt := range prompts.Prompts {
			fmt.Printf("  - %s: %s\n", prompt.Name, prompt.Description)
			if prompt.Title != "" {
				fmt.Printf("    Title: %s\n", prompt.Title)
			}
			if len(prompt.Meta) > 0 {
				fmt.Printf("    Metadata: %v\n", prompt.Meta)
			}
		}
	}

	fmt.Println("\n========== Get Prompt (code_review) ==========")
	promptResult, err := session.GetPrompt(ctx, &protocol.GetPromptParams{
		Name: "code_review",
		Arguments: map[string]string{
			"language": "go",
			"code":     "func main() { fmt.Println(\"hello\") }",
		},
	})
	if err != nil {
		log.Printf("Failed to get prompt: %v", err)
	} else {
		fmt.Printf("Description: %s\n", promptResult.Description)
		for i, msg := range promptResult.Messages {
			fmt.Printf("Message %d (%s): %v\n", i+1, msg.Role, msg.Content)
		}
		if len(promptResult.Meta) > 0 {
			fmt.Printf("Metadata: %v\n", promptResult.Meta)
		}
	}

	fmt.Println("\n========== Call Interactive Tool (interactive_greet) ==========")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name:      "interactive_greet",
		Arguments: map[string]interface{}{},
	})
	if err != nil {
		log.Printf("Failed to call tool: %v", err)
	} else {
		printToolResult(result)
	}

	fmt.Println("\n========== Call AI Tool (ai_assistant) ==========")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "ai_assistant",
		Arguments: map[string]interface{}{
			"question": "What is the MCP protocol?",
		},
	})
	if err != nil {
		log.Printf("Failed to call tool: %v", err)
	} else {
		printToolResult(result)
	}

	fmt.Println("\n========== Call Resource Link Tool (find_file) ==========")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "find_file",
		Arguments: map[string]interface{}{
			"filename": "main.go",
		},
	})
	if err != nil {
		log.Printf("Failed to call tool: %v", err)
	} else {
		printToolResult(result)
		for i, content := range result.Content {
			if rlc, ok := content.(protocol.ResourceLinkContent); ok {
				fmt.Printf("\n  Resource Link %d:\n", i+1)
				fmt.Printf("    URI: %s\n", rlc.URI)
				fmt.Printf("    Name: %s\n", rlc.Name)
				fmt.Printf("    Description: %s\n", rlc.Description)
				fmt.Printf("    MIME Type: %s\n", rlc.MimeType)
				if rlc.Annotations != nil {
					fmt.Printf("    Annotations:\n")
					if len(rlc.Annotations.Audience) > 0 {
						fmt.Printf("      Audience: %v\n", rlc.Annotations.Audience)
					}
					if rlc.Annotations.Priority > 0 {
						fmt.Printf("      Priority: %.1f\n", rlc.Annotations.Priority)
					}
				}
			}
		}
	}

	fmt.Println("\n=================== END =====================")
}

// handleElicitation handles user interaction requests from the server
func handleElicitation(ctx context.Context, params *protocol.ElicitationCreateParams) (*protocol.ElicitationResult, error) {
	fmt.Printf("\n[Elicitation] Server request: %s\n", params.Message)

	// Read user input from standard input
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Please enter: ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return protocol.NewElicitationDecline(), nil
	}

	input = strings.TrimSpace(input)

	return protocol.NewElicitationAccept(map[string]interface{}{
		"name": input,
	}), nil
}

// handleSampling handles LLM inference requests from the server
func handleSampling(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
	fmt.Printf("\n[Sampling] Server requests LLM inference\n")
	fmt.Printf("  MaxTokens: %d\n", request.MaxTokens)
	fmt.Printf("  Messages: %d messages\n", len(request.Messages))

	// Extract user message
	var userMessage string
	for _, msg := range request.Messages {
		if msg.Role == protocol.RoleUser {
			if textContent, ok := msg.Content.(protocol.TextContent); ok {
				userMessage = textContent.Text
				fmt.Printf("  User message: %s\n", userMessage)
			}
		}
	}

	// Simulate AI response
	response := fmt.Sprintf("This is a simulated AI response. The question is: %s\n\nMCP (Model Context Protocol) is an open protocol for connecting AI applications with external data sources and tools.", userMessage)

	return protocol.NewCreateMessageResult(
		protocol.RoleAssistant,
		protocol.NewTextContent(response),
		"mock-llm-v1",
		protocol.StopReasonEndTurn,
	), nil
}

// printToolResult prints tool call results
func printToolResult(result *protocol.CallToolResult) {
	for _, content := range result.Content {
		if textContent, ok := content.(protocol.TextContent); ok {
			fmt.Printf("Result: %s\n", textContent.Text)
		}
	}
	if len(result.Meta) > 0 {
		fmt.Printf("Metadata: %v\n", result.Meta)
	}
	if result.StructuredContent != nil {
		fmt.Printf("Structured Content: %v\n", result.StructuredContent)
	}
}
