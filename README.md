# MCP Go SDK 🚀

<div align="center">

<strong>一个优雅、高效的模型上下文协议 (Model Context Protocol, MCP) Go 实现</strong>

[![English](https://img.shields.io/badge/lang-English-blue.svg)](./README_EN.md)
[![中文](https://img.shields.io/badge/lang-中文-red.svg)](./README.md)

![License](https://img.shields.io/badge/license-MIT-blue.svg)
[![Go Reference](https://pkg.go.dev/badge/github.com/voocel/mcp-sdk-go.svg)](https://pkg.go.dev/github.com/voocel/mcp-sdk-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/voocel/mcp-sdk-go)](https://goreportcard.com/report/github.com/voocel/mcp-sdk-go)
[![Build Status](https://github.com/voocel/mcp-sdk-go/workflows/go/badge.svg)](https://github.com/voocel/mcp-sdk-go/actions)

</div>

## 介绍

MCP Go SDK 是模型上下文协议（Model Context Protocol）的 Go 语言实现，提供了与大语言模型交互的标准化接口。

## 功能特点

- 支持多种传输方式：标准输入/输出、SSE、WebSocket、gRPC
- 提供简洁的API接口
- 完整的类型定义
- 支持 context 上下文控制

## 安装

```bash
go get github.com/voocel/mcp-sdk-go
```

## 使用示例

### 1. 基础服务端示例

```go
package main

import (
    "context"
    "log"
    "github.com/voocel/mcp-sdk-go/server"
)

func main() {
    // 创建服务器实例
    s := server.New("Calculator", "1.0.0")
    
    // 添加计算工具
    s.AddTool("add", "加法计算", func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
        a := args["a"].(float64)
        b := args["b"].(float64)
        return protocol.NewToolResultText(fmt.Sprintf("%f", a+b)), nil
    })
    
    // 添加文件资源
    s.AddResource("file://config", "配置文件", func() []string {
        return []string{"config.json", "settings.yaml"}
    })
    
    // 启动gRPC服务器
    log.Fatal(s.ServeGRPC(context.Background(), ":50051"))
}
```

### 2. 高级服务端示例（使用FastMCP）

```go
package main

import (
    "context"
    "log"
    "github.com/voocel/mcp-sdk-go/server/fastmcp"
)

func main() {
    mcp := fastmcp.New("AdvancedServer", "1.0.0")
    
    // 链式API添加工具
    mcp.Tool("greet", "问候服务").
        WithStringParam("name", "用户名称", true).
        WithStringParam("language", "语言(en/zh)", false).
        Handle(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
            name := args["name"].(string)
            lang := args["language"].(string)
            
            if lang == "zh" {
                return fmt.Sprintf("你好，%s！", name), nil
            }
            return fmt.Sprintf("Hello, %s!", name), nil
        })
    
    // 添加提示模板
    mcp.Prompt("chat_template", "聊天模板").
        WithArgument("username", "用户名称", true).
        Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
            messages := []protocol.PromptMessage{
                protocol.NewPromptMessage(protocol.RoleSystem, 
                    protocol.NewTextContent("你是一个助手")),
                protocol.NewPromptMessage(protocol.RoleUser, 
                    protocol.NewTextContent(fmt.Sprintf("我是%s", args["username"]))),
            }
            return protocol.NewGetPromptResult("聊天模板", messages), nil
        })
    
    // 启动WebSocket服务器
    log.Fatal(mcp.ServeWebSocket(context.Background(), ":8080"))
}
```

### 3. 客户端示例

```go
package main

import (
    "context"
    "log"
    "github.com/voocel/mcp-sdk-go/client"
)

func main() {
    // 创建gRPC客户端
    c, err := client.New(client.WithGRPCTransport("localhost:50051"))
    if err != nil {
        log.Fatal(err)
    }
    defer c.Close()

    ctx := context.Background()
    
    // 初始化连接
    if err := c.Initialize(ctx); err != nil {
        log.Fatal("初始化失败:", err)
    }

    // 列出所有可用工具
    tools, err := c.ListTools(ctx)
    if err != nil {
        log.Fatal("获取工具列表失败:", err)
    }
    for _, tool := range tools {
        log.Printf("发现工具: %s - %s\n", tool.Name, tool.Description)
    }

    // 调用工具
    result, err := c.CallTool(ctx, "add", map[string]interface{}{
        "a": 5,
        "b": 3,
    })
    if err != nil {
        log.Fatal("调用工具失败:", err)
    }
    log.Println("计算结果:", result.Content[0].(map[string]interface{})["text"])

    // 获取提示模板
    prompt, err := c.GetPrompt(ctx, "chat_template", map[string]string{
        "username": "张三",
    })
    if err != nil {
        log.Fatal("获取提示模板失败:", err)
    }
    log.Printf("提示模板消息数: %d\n", len(prompt.Messages))
}
```

### 4. 不同传输方式的使用

```go
// SSE传输
client.New(client.WithSSETransport("http://localhost:8080"))

// WebSocket传输
client.New(client.WithWebSocketTransport("ws://localhost:8080"))

// 标准输入输出传输
client.New(client.WithStdioTransport("./tool", []string{"--debug"}))

// gRPC传输
client.New(client.WithGRPCTransport("localhost:50051"))
```

## 完整示例项目

- [examples/file-server](./examples/file-server): 文件服务器
  - 文件系统操作
  - 目录列表
  - 文件读取
  - 文件搜索
  - 使用SSE传输


## 贡献

欢迎贡献！请查看[贡献指南](CONTRIBUTING.md)了解如何参与。

## 许可证

MIT License

## 相关项目

- [MCP规范](https://github.com/anthropics/model-context-protocol)

---

<div align="center">
<strong>构建更智能的应用，连接更强大的模型</strong>
</div>
