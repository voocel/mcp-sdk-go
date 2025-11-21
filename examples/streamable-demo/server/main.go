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

// ========== 泛型 API 类型定义 ==========

// UserInfoInput 用户信息查询输入（泛型 API）
type UserInfoInput struct {
	UserID string `json:"user_id" jsonschema:"required,description=用户ID"`
}

// Address 地址信息
type Address struct {
	City    string `json:"city" jsonschema:"required,description=城市"`
	Country string `json:"country" jsonschema:"required,description=国家"`
	Zipcode string `json:"zipcode" jsonschema:"required,description=邮编"`
}

// Metadata 用户元数据
type Metadata struct {
	CreatedAt    string `json:"created_at" jsonschema:"required,description=创建时间"`
	LastLogin    string `json:"last_login" jsonschema:"required,description=最后登录时间"`
	ProfileViews int    `json:"profile_views" jsonschema:"required,description=个人资料浏览次数"`
	IsVerified   bool   `json:"is_verified" jsonschema:"required,description=是否已验证"`
}

// UserInfoOutput 用户信息输出（泛型 API）
type UserInfoOutput struct {
	UserID   string   `json:"user_id" jsonschema:"required,description=用户ID"`
	Name     string   `json:"name" jsonschema:"required,description=姓名"`
	Age      int      `json:"age" jsonschema:"required,description=年龄"`
	Email    string   `json:"email" jsonschema:"required,description=邮箱"`
	Address  Address  `json:"address" jsonschema:"required,description=地址信息"`
	Skills   []string `json:"skills" jsonschema:"required,description=技能列表"`
	Metadata Metadata `json:"metadata" jsonschema:"required,description=元数据"`
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

	mcpServer := server.NewServer(&protocol.ServerInfo{
		Name:    "Streamable HTTP 演示服务",
		Version: "1.0.0",
	}, nil)

	// 注册问候工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "greet",
			Description: "问候用户",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "用户名称",
					},
					"language": map[string]interface{}{
						"type":        "string",
						"description": "语言（可选）",
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

			language, _ := req.Params.Arguments["language"].(string)
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
		},
	)

	// 注册计算工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "calculate",
			Description: "执行数学计算",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type":        "string",
						"description": "运算类型 (add, subtract, multiply, divide)",
					},
					"a": map[string]interface{}{
						"type":        "number",
						"description": "第一个数字",
					},
					"b": map[string]interface{}{
						"type":        "number",
						"description": "第二个数字",
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
					return protocol.NewToolResultError("除数不能为零"), nil
				}
				result = a / b
				opSymbol = "/"
			default:
				return protocol.NewToolResultError("不支持的运算类型"), nil
			}

			resultText := fmt.Sprintf("%.2f %s %.2f = %.2f (请求 #%d, 时间: %s)",
				a, opSymbol, b, result, requestCounter, time.Now().Format("15:04:05"))
			return protocol.NewToolResultText(resultText), nil
		},
	)

	// 注册结构化输出工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "get_weather",
			Description: "获取指定城市的天气信息（返回结构化数据）",
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

			OutputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{
						"type":        "string",
						"description": "城市名称",
					},
					"temperature": map[string]interface{}{
						"type":        "number",
						"description": "温度（摄氏度）",
					},
					"humidity": map[string]interface{}{
						"type":        "integer",
						"description": "湿度（百分比）",
					},
					"condition": map[string]interface{}{
						"type":        "string",
						"description": "天气状况",
					},
					"wind_speed": map[string]interface{}{
						"type":        "number",
						"description": "风速（km/h）",
					},
					"timestamp": map[string]interface{}{
						"type":        "string",
						"description": "查询时间",
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
				"temperature": 22.5 + float64(requestCounter%10), // 模拟变化
				"humidity":    65 + requestCounter%20,
				"condition":   []string{"晴朗", "多云", "小雨", "阴天"}[requestCounter%4],
				"wind_speed":  12.3,
				"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
			}

			// 返回结构化内容 (使用 StructuredContent)
			result := &protocol.CallToolResult{
				StructuredContent: weatherData,
				IsError:           false,
			}

			return result, nil
		},
	)

	// 注册用户信息工具（使用泛型 API）
	server.AddTool(mcpServer, &protocol.Tool{
		Name:        "get_user_info",
		Description: "获取用户详细信息（演示泛型 API - 自动生成 Schema）",
	}, func(ctx context.Context, req *server.CallToolRequest, input UserInfoInput) (*protocol.CallToolResult, UserInfoOutput, error) {
		requestCounter++

		output := UserInfoOutput{
			UserID: input.UserID,
			Name:   "张三",
			Age:    28,
			Email:  fmt.Sprintf("user_%s@example.com", input.UserID),
			Address: Address{
				City:    "北京",
				Country: "中国",
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

	// 注册服务器统计资源
	mcpServer.AddResource(
		&protocol.Resource{
			URI:         "stats://server",
			Name:        "服务器统计",
			Description: "获取服务器运行统计信息",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			uptime := time.Since(serverStartTime)
			stats := fmt.Sprintf(`服务器统计信息:
运行时间: %s
请求总数: %d
协议版本: %s
启动时间: %s`,
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
		log.Println("Streamable HTTP MCP 服务器已启动")
		log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		log.Println("端点地址: http://localhost:8081/mcp")
		log.Println("传输协议: Streamable HTTP")
		log.Println("MCP版本: 2025-06-18")
		log.Println()
		log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		log.Println()

		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器错误: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("服务器关闭错误: %v", err)
	}

	log.Println("Closed!")
}
