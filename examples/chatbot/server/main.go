package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
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
	// 设置随机种子
	rand.Seed(time.Now().UnixNano())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 处理优雅关闭
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalCh
		log.Println("接收到关闭信号")
		cancel()
	}()

	// 创建 FastMCP 服务器
	mcp := server.NewFastMCP("聊天机器人", "1.0.0")

	// 注册问候工具
	mcp.Tool("greeting", "获取随机问候语").
		WithStringParam("name", "用户名称", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			name, ok := args["name"].(string)
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
		})

	// 注册天气工具
	mcp.Tool("weather", "获取指定城市的天气").
		WithStringParam("city", "城市名称", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			city, ok := args["city"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'city' 必须是字符串"), nil
			}

			weatherTypes := []string{"晴天", "多云", "小雨", "大雨", "雷暴", "雾天", "小雪", "大雪"}
			temperatures := []int{-5, 0, 5, 10, 15, 20, 25, 30, 35}

			weather := weatherTypes[rand.Intn(len(weatherTypes))]
			temp := temperatures[rand.Intn(len(temperatures))]

			formattedWeather := fmt.Sprintf("%s 今天的天气是 %s，温度 %d°C", city, weather, temp)
			return protocol.NewToolResultText(formattedWeather), nil
		})

	// 注册翻译工具
	mcp.Tool("translate", "简单的中英文翻译").
		WithStringParam("text", "要翻译的文本", true).
		WithStringParam("target_lang", "目标语言 (en 或 zh)", true).
		Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
			text, ok := args["text"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'text' 必须是字符串"), nil
			}
			targetLang, ok := args["target_lang"].(string)
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
		})

	// 注册聊天模板提示
	mcp.Prompt("chat_template", "聊天模板").
		WithArgument("username", "用户名", true).
		Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
			username := args["username"]
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
		})

	// 创建SSE传输服务器
	sseServer := sse.NewServer(":8082", mcp)

	log.Println("启动聊天机器人 MCP 服务器 (SSE) 在端口 :8082...")
	if err := sseServer.Serve(ctx); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已关闭")
}
