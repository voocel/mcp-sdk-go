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
		log.Println("接收到关闭信号")
		cancel()
	}()

	mcpServer := server.NewServer(&protocol.ServerInfo{
		Name:    "聊天机器人",
		Version: "1.0.0",
	}, nil)

	// 注册问候工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "greeting",
			Description: "获取随机问候语",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "用户名称",
					},
				},
				"required": []string{"name"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			name, ok := req.Params.Arguments["name"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'name' 必须是字符串"), nil
			}

			greetings := []string{
				"你好，%s！很高兴见到你！",
				"嗨，%s！今天过得怎么样？",
				"很高兴看到你，%s！",
				"欢迎，%s！",
				"嘿嘿，%s 来了！",
			}

			greeting := greetings[rand.Intn(len(greetings))]
			formattedGreeting := fmt.Sprintf(greeting, name)
			return protocol.NewToolResultText(formattedGreeting), nil
		},
	)

	// 注册天气工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "weather",
			Description: "获取指定城市的天气",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{
						"type":        "string",
						"description": "城市名称",
					},
				},
				"required": []string{"city"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			city, ok := req.Params.Arguments["city"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'city' 必须是字符串"), nil
			}

			weatherTypes := []string{"晴天", "多云", "小雨", "大雨", "雷暴", "雾天", "小雪", "大雪"}
			temperatures := []int{-5, 0, 5, 10, 15, 20, 25, 30, 35}

			weather := weatherTypes[rand.Intn(len(weatherTypes))]
			temp := temperatures[rand.Intn(len(temperatures))]

			formattedWeather := fmt.Sprintf("%s 今天的天气是 %s，温度 %d°C", city, weather, temp)
			return protocol.NewToolResultText(formattedWeather), nil
		},
	)

	// 注册翻译工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "translate",
			Description: "简单的中英文翻译",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "要翻译的文本",
					},
					"target_lang": map[string]interface{}{
						"type":        "string",
						"description": "目标语言 (en 或 zh)",
					},
				},
				"required": []string{"text", "target_lang"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			text, ok := req.Params.Arguments["text"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'text' 必须是字符串"), nil
			}
			targetLang, ok := req.Params.Arguments["target_lang"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'target_lang' 必须是字符串"), nil
			}

			// 简单的词典映射
			enToZh := map[string]string{
				"hello":   "你好",
				"world":   "世界",
				"thanks":  "谢谢",
				"goodbye": "再见",
				"book":    "书",
				"apple":   "苹果",
				"water":   "水",
				"food":    "食物",
				"good":    "好的",
				"bad":     "坏的",
			}

			zhToEn := map[string]string{
				"你好": "hello",
				"世界": "world",
				"谢谢": "thanks",
				"再见": "goodbye",
				"书":   "book",
				"苹果": "apple",
				"水":   "water",
				"食物": "food",
				"好的": "good",
				"坏的": "bad",
			}

			if targetLang == "zh" {
				// 英译中
				words := strings.Fields(strings.ToLower(text))
				for i, word := range words {
					if translation, exists := enToZh[word]; exists {
						words[i] = translation
					}
				}
				return protocol.NewToolResultText(strings.Join(words, " ")), nil
			} else if targetLang == "en" {
				// 中译英
				result := text
				for zh, en := range zhToEn {
					result = strings.ReplaceAll(result, zh, en)
				}
				return protocol.NewToolResultText(result), nil
			} else {
				return protocol.NewToolResultError(fmt.Sprintf("不支持的目标语言: %s", targetLang)), nil
			}
		},
	)

	// 注册聊天模板提示
	mcpServer.AddPrompt(
		&protocol.Prompt{
			Name:        "chat_template",
			Description: "聊天模板",
			Arguments: []protocol.PromptArgument{
				{
					Name:        "username",
					Description: "用户名",
					Required:    true,
				},
			},
		},
		func(ctx context.Context, req *server.GetPromptRequest) (*protocol.GetPromptResult, error) {
			username, _ := req.Params.Arguments["username"]
			if username == "" {
				username = "朋友"
			}

			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(
					"你是一个友好的聊天机器人助手，可以提供天气信息、翻译服务和友好的问候。")),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					fmt.Sprintf("你好，我是 %s", username))),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					fmt.Sprintf("你好，%s！很高兴认识你。我可以帮你查看天气、翻译简单的中英文，或者只是聊天。今天我可以为你做些什么吗？", username))),
			}

			return protocol.NewGetPromptResult("聊天机器人对话模板", messages...), nil
		},
	)

	handler := sse.NewHTTPHandler(func(r *http.Request) *server.Server {
		return mcpServer
	})
	httpServer := &http.Server{
		Addr:    ":8082",
		Handler: handler,
	}

	log.Println("启动聊天机器人 MCP 服务器 (SSE) 在端口 :8082...")

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器错误: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("服务器已关闭")
}
