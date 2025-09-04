package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/streamable"
)

// CalculationResult 计算结果
type CalculationResult struct {
	Expression string  `json:"expression" description:"原始表达式"`
	Result     float64 `json:"result" description:"计算结果"`
	Operation  string  `json:"operation" description:"执行的运算"`
	Timestamp  string  `json:"timestamp" description:"计算时间"`
}

// WeatherInfo 天气信息
type WeatherInfo struct {
	Location    string  `json:"location" description:"地点"`
	Temperature float64 `json:"temperature" description:"温度(摄氏度)"`
	Humidity    int     `json:"humidity" description:"湿度百分比"`
	Condition   string  `json:"condition" description:"天气状况"`
	UpdateTime  string  `json:"update_time" description:"更新时间"`
}

// ServerStats 服务器统计信息
type ServerStats struct {
	Uptime       string `json:"uptime" description:"运行时间"`
	RequestCount int    `json:"request_count" description:"请求总数"`
	ActiveTools  int    `json:"active_tools" description:"活跃工具数"`
	Protocol     string `json:"protocol" description:"协议版本"`
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
		log.Println("接收到关闭信号")
		cancel()
	}()

	mcp := server.NewFastMCP("Streamable HTTP 演示服务", "1.0.0")
	mcp.Tool("greet", "问候用户").
		WithStringParam("name", "用户名称", true).
		WithStringParam("language", "语言（可选）", false).
		Handle(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			name, ok := args["name"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'name' 必须是字符串"), nil
			}

			language, _ := args["language"].(string)
			var greeting string

			switch language {
			case "zh", "中文":
				greeting = fmt.Sprintf("你好，%s！欢迎使用 Streamable HTTP 传输协议！", name)
			case "en", "english":
				greeting = fmt.Sprintf("Hello, %s! Welcome to Streamable HTTP transport!", name)
			default:
				greeting = fmt.Sprintf("Hello, %s! 你好！欢迎使用 Streamable HTTP 传输协议！", name)
			}

			return protocol.NewToolResultText(greeting), nil
		})

	// 注册一个数学计算工具 - 结构化输出 (MCP 2025-06-18)
	err := mcp.Tool("calculate", "执行数学计算，返回结构化结果").
		WithStringParam("operation", "运算类型 (add, subtract, multiply, divide)", true).
		WithNumberParam("a", "第一个数字", true).
		WithNumberParam("b", "第二个数字", true).
		WithStructOutputSchema(CalculationResult{}). // 使用结构体自动生成输出模式
		HandleWithValidation(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			requestCounter++

			operation, _ := args["operation"].(string)
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)

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
				opSymbol = "×"
			case "divide":
				if b == 0 {
					return protocol.NewToolResultError("除数不能为零"), nil
				}
				result = a / b
				opSymbol = "÷"
			default:
				return protocol.NewToolResultError("不支持的运算类型"), nil
			}

			calcResult := CalculationResult{
				Expression: fmt.Sprintf("%.2f %s %.2f", a, opSymbol, b),
				Result:     result,
				Operation:  operation,
				Timestamp:  time.Now().Format(time.RFC3339),
			}

			return protocol.NewToolResultTextWithStructured(
				fmt.Sprintf("计算完成：%.2f %s %.2f = %.2f", a, opSymbol, b, result),
				calcResult,
			), nil
		})
	if err != nil {
		log.Fatal(err)
	}

	err = mcp.SimpleStructuredTool(
		"get_weather",
		"获取指定城市的天气信息",
		protocol.JSONSchema{
			"type": "object",
			"properties": map[string]any{
				"location":    map[string]any{"type": "string", "description": "地点"},
				"temperature": map[string]any{"type": "number", "description": "温度(摄氏度)"},
				"humidity":    map[string]any{"type": "integer", "description": "湿度百分比"},
				"condition":   map[string]any{"type": "string", "description": "天气状况"},
				"update_time": map[string]any{"type": "string", "description": "更新时间"},
			},
			"required": []string{"location", "temperature", "humidity", "condition", "update_time"},
		},
		func(ctx context.Context, args map[string]any) (any, error) {
			requestCounter++

			location, ok := args["location"].(string)
			if !ok {
				return nil, fmt.Errorf("location参数是必需的")
			}

			conditions := []string{"晴天", "多云", "小雨", "阴天"}
			weather := WeatherInfo{
				Location:    location,
				Temperature: 15 + float64(len(location)%20),
				Humidity:    50 + len(location)%40,
				Condition:   conditions[len(location)%len(conditions)],
				UpdateTime:  time.Now().Format("2006-01-02 15:04:05"),
			}

			return weather, nil
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	// 注册服务器状态工具 - 演示动态结构化数据
	err = mcp.Tool("server_stats", "获取服务器运行状态").
		WithStructOutputSchema(ServerStats{}).
		HandleWithValidation(func(ctx context.Context, args map[string]any) (*protocol.CallToolResult, error) {
			requestCounter++

			uptime := time.Since(serverStartTime)
			stats := ServerStats{
				Uptime:       uptime.Round(time.Second).String(),
				RequestCount: requestCounter,
				ActiveTools:  4, // greet, calculate, get_weather, server_stats
				Protocol:     "Streamable HTTP (MCP 2025-06-18)",
			}

			return protocol.NewToolResultTextWithStructured(
				fmt.Sprintf("服务器已运行 %s，处理了 %d 个请求", stats.Uptime, stats.RequestCount),
				stats,
			), nil
		})
	if err != nil {
		log.Fatal(err)
	}

	// 注册一个资源
	mcp.Resource("info://server", "服务器信息", "获取服务器基本信息").
		Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
			info := `# Streamable HTTP MCP 服务器

这是一个演示 Streamable HTTP 传输协议和结构化工具输出的 MCP 服务器。

## 特性

- **单一端点**：所有通信通过一个 HTTP 端点
- **动态升级**：根据需要自动升级到 SSE 流
- **会话管理**：支持有状态的会话
- **可恢复连接**：支持连接中断后的恢复
- **安全防护**：内置 DNS rebinding 攻击防护
- **结构化输出**：支持类型化的工具结果 (MCP 2025-06-18)

## 协议版本

- MCP 版本：2025-06-18
- 传输协议：Streamable HTTP
- 新特性：结构化工具输出

## 可用工具

1. **greet** - 多语言问候工具（传统文本输出）
2. **calculate** - 数学计算工具（结构化输出演示）
3. **get_weather** - 天气查询工具（简化API演示）
4. **server_stats** - 服务器状态工具（动态数据演示）

## 结构化输出示例

工具现在可以返回类型化的JSON数据：

` + "```" + `json
{
  "content": [{"type": "text", "text": "人类可读的描述"}],
  "structuredContent": {
    "result": 42,
    "timestamp": "2025-01-15T10:30:00Z",
    "operation": "multiply"
  }
}
` + "```" + `

## 可用资源

- **info://server** - 服务器信息（本资源）
`
			contents := protocol.NewTextResourceContents("info://server", info)
			return protocol.NewReadResourceResult(contents), nil
		})

	// 注册一个提示模板
	mcp.Prompt("streamable_help", "Streamable HTTP 帮助信息").
		WithArgument("topic", "帮助主题", false).
		Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
			topic := args["topic"]
			if topic == "" {
				topic = "general"
			}

			var content string
			switch topic {
			case "transport":
				content = "Streamable HTTP 是 MCP 2025-06-18 规范中的传输协议，它统一了 HTTP 和 SSE 的优势。"
			case "session":
				content = "会话管理允许服务器在多个请求之间保持状态，提供更好的用户体验。"
			case "security":
				content = "Streamable HTTP 包含多种安全机制，包括 Origin 验证和会话管理。"
			case "structured":
				content = "结构化工具输出是 MCP 2025-06-18 的新特性，允许工具返回类型化的JSON数据，同时保持文本描述的可读性。"
			case "tools":
				content = "本服务器演示了多种工具类型：传统文本输出(greet)、结构化输出(calculate)、简化API(get_weather)和动态数据(server_stats)。"
			default:
				content = "Streamable HTTP 是一个现代化的 MCP 传输协议，现在支持结构化工具输出功能。"
			}

			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(content)),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					fmt.Sprintf("请告诉我更多关于 %s 的信息。", topic))),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					"让我来解释一下 Streamable HTTP 传输协议的相关内容。")),
			}
			return protocol.NewGetPromptResult("Streamable HTTP 帮助提示", messages...), nil
		})

	// 创建Streamable HTTP传输服务器
	streamableServer := streamable.NewServer(":8081", mcp)

	log.Println("启动 Streamable HTTP MCP 服务器")
	log.Println("监听地址: http://localhost:8081")
	log.Println("传输协议: Streamable HTTP")
	log.Println("MCP版本: 2025-06-18 (支持结构化工具输出)")
	log.Println("可用工具: greet, calculate, get_weather, server_stats")

	if err := streamableServer.Serve(ctx); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}

	log.Println("服务器已优雅关闭")
}
