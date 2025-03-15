package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	mcp := server.New("Chatbot", "1.0.0")

	mcp.Tool("greeting", "Get random greeting").
		WithStringParam("name", "User name", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			name := args["name"].(string)

			greetings := []string{
				"Hello, %s! Nice to meet you!",
				"Hi, %s! How's your day going?",
				"Great to see you, %s!",
				"Welcome, %s!",
				"Hey hey, %s is here!",
			}

			greeting := greetings[rand.Intn(len(greetings))]
			formattedGreeting := fmt.Sprintf(greeting, name)
			return protocol.NewToolResultText(formattedGreeting), nil
		})

	mcp.Tool("weather", "Get weather for specified city").
		WithStringParam("city", "City name", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			city := args["city"].(string)

			weatherTypes := []string{"Sunny", "Cloudy", "Light Rain", "Heavy Rain", "Thunderstorm", "Foggy", "Light Snow", "Heavy Snow"}
			temperatures := []int{-5, 0, 5, 10, 15, 20, 25, 30, 35}

			weather := weatherTypes[rand.Intn(len(weatherTypes))]
			temp := temperatures[rand.Intn(len(temperatures))]

			formattedGreeting := fmt.Sprintf("The weather in %s today is %s, temperature %d°C", city, weather, temp)
			return protocol.NewToolResultText(formattedGreeting), nil
		})

	mcp.Tool("translate", "Simple Chinese-English translation").
		WithStringParam("text", "Text to translate", true).
		WithStringParam("target_lang", "Target language (en or zh)", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			text := args["text"].(string)
			targetLang := args["target_lang"].(string)

			enToZh := map[string]string{
				"hello":   "你好",
				"world":   "世界",
				"thanks":  "谢谢",
				"goodbye": "再见",
				"book":    "书",
				"apple":   "苹果",
			}

			zhToEn := map[string]string{
				"你好": "hello",
				"世界": "world",
				"谢谢": "thanks",
				"再见": "goodbye",
				"书":  "book",
				"苹果": "apple",
			}

			if targetLang == "zh" {
				words := strings.Fields(strings.ToLower(text))
				for i, word := range words {
					if translation, ok := enToZh[word]; ok {
						words[i] = translation
					}
				}
				return protocol.NewToolResultText(strings.Join(words, " ")), nil
			} else if targetLang == "en" {
				for zh, en := range zhToEn {
					text = strings.ReplaceAll(text, zh, en)
				}
				return protocol.NewToolResultText(text), nil
			}

			return nil, fmt.Errorf("Unsupported target language: %s", targetLang)
		})

	mcp.Prompt("chat_template", "Chat template").
		WithArgument("username", "User name", true).
		Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
			username := args["username"]

			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(
					"You are a friendly chatbot assistant who can provide weather information, translation services, and friendly greetings.")),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					fmt.Sprintf("Hello, I'm %s", username))),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					fmt.Sprintf("Hello, %s! Nice to meet you. I can help you check the weather, translate simple Chinese-English texts, or just chat. How can I assist you today?", username))),
			}

			return protocol.NewGetPromptResult("chat_template", messages), nil
		})

	log.Println("Starting WebSocket server on :8082...")
	if err := mcp.ServeWebSocket(context.Background(), ":8082"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
