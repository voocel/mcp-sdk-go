package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	rand.Seed(time.Now().UnixNano())
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
		Name:    "Chatbot",
		Version: "1.0.0",
	}, nil)

	// Register greeting tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "greeting",
			Description: "Get a random greeting",
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
			name, ok := req.Params.Arguments["name"].(string)
			if !ok {
				return protocol.NewToolResultError("Parameter 'name' must be a string"), nil
			}

			greetings := []string{
				"Hello, %s! Nice to meet you!",
				"Hi, %s! How are you today?",
				"Great to see you, %s!",
				"Welcome, %s!",
				"Hey, %s is here!",
			}

			greeting := greetings[rand.Intn(len(greetings))]
			formattedGreeting := fmt.Sprintf(greeting, name)
			return protocol.NewToolResultText(formattedGreeting), nil
		},
	)

	// Register weather tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "weather",
			Description: "Get weather for a specified city",
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
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			city, ok := req.Params.Arguments["city"].(string)
			if !ok {
				return protocol.NewToolResultError("Parameter 'city' must be a string"), nil
			}

			weatherTypes := []string{"Sunny", "Cloudy", "Light Rain", "Heavy Rain", "Thunderstorm", "Foggy", "Light Snow", "Heavy Snow"}
			temperatures := []int{-5, 0, 5, 10, 15, 20, 25, 30, 35}

			weather := weatherTypes[rand.Intn(len(weatherTypes))]
			temp := temperatures[rand.Intn(len(temperatures))]

			formattedWeather := fmt.Sprintf("Today's weather in %s is %s, temperature %d°C", city, weather, temp)
			return protocol.NewToolResultText(formattedWeather), nil
		},
	)

	// Register translation tool
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "translate",
			Description: "Simple Chinese-English translation",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to translate",
					},
					"target_lang": map[string]interface{}{
						"type":        "string",
						"description": "Target language (en or zh)",
					},
				},
				"required": []string{"text", "target_lang"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			text, ok := req.Params.Arguments["text"].(string)
			if !ok {
				return protocol.NewToolResultError("Parameter 'text' must be a string"), nil
			}
			targetLang, ok := req.Params.Arguments["target_lang"].(string)
			if !ok {
				return protocol.NewToolResultError("Parameter 'target_lang' must be a string"), nil
			}

			// Simple dictionary mapping
			enToZh := map[string]string{
				"hello":   "ni hao",
				"world":   "shi jie",
				"thanks":  "xie xie",
				"goodbye": "zai jian",
				"book":    "shu",
				"apple":   "ping guo",
				"water":   "shui",
				"food":    "shi wu",
				"good":    "hao de",
				"bad":     "huai de",
			}

			zhToEn := map[string]string{
				"ni hao":   "hello",
				"shi jie":  "world",
				"xie xie":  "thanks",
				"zai jian": "goodbye",
				"shu":      "book",
				"ping guo": "apple",
				"shui":     "water",
				"shi wu":   "food",
				"hao de":   "good",
				"huai de":  "bad",
			}

			if targetLang == "zh" {
				// English to Chinese
				words := strings.Fields(strings.ToLower(text))
				for i, word := range words {
					if translation, exists := enToZh[word]; exists {
						words[i] = translation
					}
				}
				return protocol.NewToolResultText(strings.Join(words, " ")), nil
			} else if targetLang == "en" {
				// Chinese to English
				result := text
				for zh, en := range zhToEn {
					result = strings.ReplaceAll(result, zh, en)
				}
				return protocol.NewToolResultText(result), nil
			} else {
				return protocol.NewToolResultError(fmt.Sprintf("Unsupported target language: %s", targetLang)), nil
			}
		},
	)

	// Register chat template prompt
	mcpServer.AddPrompt(
		&protocol.Prompt{
			Name:        "chat_template",
			Description: "Chat template",
			Arguments: []protocol.PromptArgument{
				{
					Name:        "username",
					Description: "Username",
					Required:    true,
				},
			},
		},
		func(ctx context.Context, req *server.GetPromptRequest) (*protocol.GetPromptResult, error) {
			username, _ := req.Params.Arguments["username"]
			if username == "" {
				username = "friend"
			}

			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(
					"You are a friendly chatbot assistant that can provide weather information, translation services, and friendly greetings.")),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					fmt.Sprintf("Hello, I am %s", username))),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					fmt.Sprintf("Hello, %s! Nice to meet you. I can help you check the weather, translate simple Chinese-English text, or just chat. What can I do for you today?", username))),
			}

			return protocol.NewGetPromptResult("Chatbot conversation template", messages...), nil
		},
	)

	handler := sse.NewHTTPHandler(func(r *http.Request) *server.Server {
		return mcpServer
	})
	httpServer := &http.Server{
		Addr:    ":8082",
		Handler: handler,
	}

	log.Println("Starting Chatbot MCP Server (SSE) on port :8082...")

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Server shutdown")
}
