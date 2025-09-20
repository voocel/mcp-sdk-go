# MCP Go SDK

<div align="center">

<strong>一个优雅、高效的模型上下文协议 (Model Context Protocol, MCP) Go 实现</strong>

[![English](https://img.shields.io/badge/lang-English-blue.svg)](./README_EN.md)
[![中文](https://img.shields.io/badge/lang-中文-red.svg)](./README.md)

![License](https://img.shields.io/badge/license-MIT-blue.svg)
[![Go Reference](https://pkg.go.dev/badge/github.com/voocel/mcp-sdk-go.svg)](https://pkg.go.dev/github.com/voocel/mcp-sdk-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/voocel/mcp-sdk-go)](https://goreportcard.com/report/github.com/voocel/mcp-sdk-go)
[![Build Status](https://github.com/voocel/mcp-sdk-go/workflows/go/badge.svg)](https://github.com/voocel/mcp-sdk-go/actions)

</div>

<div align="center">

**构建更智能的应用，连接更强大的模型**
*使用 MCP Go SDK，轻松集成大语言模型能力*

</div>

## 介绍

MCP Go SDK 是模型上下文协议（Model Context Protocol）的 Go 语言实现，完全支持最新的 **MCP 2025-06-18** 规范，同时向后兼容 **MCP 2025-03-26** 和 **MCP 2024-11-05**。

## 核心特性

- **完全符合 MCP 标准** - 支持最新 MCP 2025-06-18 规范，向后兼容 2025-03-26, 2024-11-05
- **服务器 SDK** - 快速构建 MCP 服务器，支持工具、资源、提示模板
- **客户端 SDK** - 连接任何 MCP 兼容服务器的客户端实现
- **多种传输协议** - STDIO、SSE、Streamable HTTP (官方标准)
- **Elicitation 支持** - 交互式用户输入，支持字符串、数字、布尔值、枚举选择
- **Sampling 支持** - 服务器发起的LLM推理请求，支持递归AI交互
- **类型安全** - 完整的类型定义和参数验证
- **高性能** - 并发安全，优化的消息处理
- **安全防护** - 内置输入验证、路径遍历保护、资源限制

## MCP协议版本支持

本SDK跟踪并支持MCP协议的最新发展，确保与生态系统的兼容性：

### 支持的版本

| 版本 | 发布时间 | 主要特性 | 支持状态 |
|------|----------|----------|----------|
| **2025-06-18** | 2025年6月 | 结构化工具输出、工具注解、**Elicitation 用户交互**、**Sampling LLM推理** | **完全支持** |
| **2025-03-26** | 2025年3月 | OAuth 2.1授权、Streamable HTTP、JSON-RPC批处理 | **完全支持** |
| **2024-11-05** | 2024年11月 | HTTP+SSE传输、基础工具和资源 | **完全支持** |

### 最新特性 (2025-06-18)

- **结构化工具输出**：工具可返回类型化JSON数据，便于程序化处理
- **工具注解**：描述工具行为特征（只读、破坏性、缓存策略等）
- **用户交互请求**：工具可主动请求用户输入或确认
- **资源链接**：支持资源间的关联和引用
- **协议版本头**：HTTP传输需要`MCP-Protocol-Version`头

### 主要变更历史

**2025-03-26 → 2025-06-18**：

- 新增结构化工具输出支持
- 增强工具注解系统
- 添加用户交互请求机制
- 支持资源链接功能

**2024-11-05 → 2025-03-26**：

- 引入OAuth 2.1授权框架
- 用Streamable HTTP替代HTTP+SSE
- 添加JSON-RPC批处理支持
- 增加音频内容类型支持

## 安装

```bash
go get github.com/voocel/mcp-sdk-go
```

## 快速开始

### 服务器端 (主要功能)

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/voocel/mcp-sdk-go/protocol"
    "github.com/voocel/mcp-sdk-go/server"
    "github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // 处理优雅关闭
    signalCh := make(chan os.Signal, 1)
    signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-signalCh
        cancel()
    }()

    // 创建 FastMCP 服务器
    mcp := server.NewFastMCP("快速入门服务器", "1.0.0")

    // 注册一个简单的问候工具
    mcp.Tool("greet", "问候用户").
        WithStringParam("name", "用户名称", true).
        Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
            name, ok := args["name"].(string)
            if !ok {
                return protocol.NewToolResultError("参数 'name' 必须是字符串"), nil
            }
            
            greeting := fmt.Sprintf("你好，%s！欢迎使用 MCP Go SDK！", name)
            return protocol.NewToolResultText(greeting), nil
        })

    // 注册一个资源
    mcp.Resource("info://server", "服务器信息", "获取服务器基本信息").
        Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
            info := "MCP Go SDK 服务器正在运行..."
            contents := protocol.NewTextResourceContents("info://server", info)
            return protocol.NewReadResourceResult(contents), nil
        })

    // 创建 SSE 传输服务器 (也可以使用 Streamable HTTP)
    sseServer := sse.NewServer(":8080", mcp)
    
    log.Println("服务器启动在 http://localhost:8080")
    if err := sseServer.Serve(ctx); err != nil && err != context.Canceled {
        log.Fatalf("服务器错误: %v", err)
    }
}
```

### 客户端 (连接 MCP 服务器)

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/voocel/mcp-sdk-go/client"
    "github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // 创建 SSE 客户端
    mcpClient, err := client.New(
        client.WithSSETransport("http://localhost:8080"),
        client.WithClientInfo("demo-client", "1.0.0"),
    )
    if err != nil {
        log.Fatalf("创建客户端失败: %v", err)
    }
    defer mcpClient.Close()

    // 初始化连接
    initResult, err := mcpClient.Initialize(ctx, protocol.ClientInfo{
        Name:    "演示客户端",
        Version: "1.0.0",
    })
    if err != nil {
        log.Fatalf("初始化失败: %v", err)
    }

    fmt.Printf("连接成功！服务器: %s v%s\n",
        initResult.ServerInfo.Name, initResult.ServerInfo.Version)

    // 发送初始化完成通知
    if err := mcpClient.SendInitialized(ctx); err != nil {
        log.Printf("发送初始化完成通知失败: %v", err)
    }

    // 调用工具
    result, err := mcpClient.CallTool(ctx, "greet", map[string]interface{}{
        "name": "Go 开发者",
    })
    if err != nil {
        log.Fatalf("调用工具失败: %v", err)
    }
    
    if len(result.Content) > 0 {
        if textContent, ok := result.Content[0].(protocol.TextContent); ok {
            fmt.Printf("结果: %s\n", textContent.Text)
        }
    }

    // 读取资源
    resource, err := mcpClient.ReadResource(ctx, "info://server")
    if err != nil {
        log.Fatalf("读取资源失败: %v", err)
    }
    
    if len(resource.Contents) > 0 {
        fmt.Printf("服务器信息: %s\n", resource.Contents[0].Text)
    }
}
```

## 示例项目

| 示例 | 描述 | 传输协议 | 运行方式 |
|------|------|----------|----------|
| [Calculator](./examples/calculator/) | 数学计算器服务 | STDIO | `cd examples/calculator/server && go run main.go` |
| [SSE Demo](./examples/sse-demo/) | SSE 传输演示 | SSE | `cd examples/sse-demo/server && go run main.go` |
| [Chatbot](./examples/chatbot/) | 聊天机器人服务 | SSE | `cd examples/chatbot/server && go run main.go` |
| [File Server](./examples/file-server/) | 文件操作服务 | SSE | `cd examples/file-server/server && go run main.go` |
| [Streamable Demo](./examples/streamable-demo/) | Streamable HTTP 演示 (MCP 2025-06-18) | Streamable HTTP | `cd examples/streamable-demo/server && go run main.go` |
| [Resource Templates](./examples/resource-templates/) | 资源模板注册与使用 | SSE | `cd examples/resource-templates/server && go run main.go` |

**运行示例**: 每个示例都包含服务器和客户端，需要在不同终端中分别运行。

## 核心架构

### 服务器端(主要功能)

```go
// 创建FastMCP服务器
mcp := server.NewFastMCP("服务名称", "1.0.0")

// 注册工具 - 链式 API
mcp.Tool("tool_name", "工具描述").
    WithStringParam("param1", "参数1描述", true).
    WithIntParam("param2", "参数2描述", false).
    Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
        // 工具逻辑实现
        return protocol.NewToolResultText("结果"), nil
    })

// 注册资源
mcp.Resource("resource://uri", "资源名称", "资源描述").
    Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
        contents := protocol.NewTextResourceContents("resource://uri", "内容")
        return protocol.NewReadResourceResult(contents), nil
    })

// 注册资源模板，向客户端声明可用的动态资源模式
mcp.ResourceTemplate("log://app/{date}", "应用日志", "获取指定日期的应用日志").
    WithMimeType("text/plain").
    Register()

// 注册提示模板
mcp.Prompt("prompt_name", "提示描述").
    WithArgument("arg1", "参数描述", true).
    Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
        messages := []protocol.PromptMessage{
            protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent("内容")),
        }
        return protocol.NewGetPromptResult("描述", messages...), nil
    })

// 注册支持 Elicitation 的交互式工具
mcp.Tool("user_profile", "创建用户档案").
    HandleWithElicitation(func(ctx *server.MCPContext, args map[string]interface{}) (*protocol.CallToolResult, error) {
        // 请求用户输入姓名
        name, err := ctx.ElicitString("请输入你的姓名", "name", "你的全名", true)
        if err != nil {
            return protocol.NewToolResultError(err.Error()), nil
        }

        // 请求用户选择颜色
        color, err := ctx.ElicitChoice("请选择你喜欢的颜色", "color", "你最喜欢的颜色",
            []string{"red", "green", "blue"}, []string{"红色", "绿色", "蓝色"}, true)
        if err != nil {
            return protocol.NewToolResultError(err.Error()), nil
        }

        return protocol.NewToolResultText(fmt.Sprintf("用户档案: %s 喜欢 %s", name, color)), nil
    })

// 启动服务器 (SSE 传输)
sseTransport := sse.NewServer(":8080", mcp)
sseTransport.Serve(ctx)

// 或者使用 Streamable HTTP 传输 (推荐用于新项目)
// streamableTransport := streamable.NewServer(":8080", mcp)
// streamableTransport.Serve(ctx)
```

### 客户端(连接 MCP 服务器)

```go
// Elicitation 处理器
func handleElicitation(ctx context.Context, params *protocol.ElicitationCreateParams) (*protocol.ElicitationResult, error) {
    fmt.Println(params.Message) // 显示服务器请求
    // 获取用户输入并返回结果
    return protocol.NewElicitationAccept(map[string]interface{}{
        "name": "用户输入的姓名",
    }), nil
}

// 创建客户端
client, err := client.New(
    client.WithSSETransport("http://localhost:8080"),
    client.WithClientInfo("client-name", "1.0.0"),
    client.WithElicitationHandler(handleElicitation), // 设置 elicitation 处理器
)

// Sampling 处理器
func handleSampling(ctx context.Context, request *protocol.CreateMessageRequest) (*protocol.CreateMessageResult, error) {
    fmt.Printf("收到AI推理请求: %+v\n", request)
    // 调用实际的LLM API并返回结果
    return protocol.NewCreateMessageResult(
        protocol.RoleAssistant,
        protocol.NewTextContent("AI生成的回复"),
        "gpt-4",
        protocol.StopReasonEndTurn,
    ), nil
}

// 创建客户端
client, err := client.New(
    client.WithSSETransport("http://localhost:8080"),
    client.WithClientInfo("client-name", "1.0.0"),
    client.WithElicitationHandler(handleElicitation), // 设置 elicitation 处理器
    client.WithSamplingHandler(handleSampling),       // 设置 sampling 处理器
)

// 初始化并调用工具
initResult, err := client.Initialize(ctx, protocol.ClientInfo{...})
client.SendInitialized(ctx)
result, err := client.CallTool(ctx, "tool_name", map[string]interface{}{"param": "value"})
```

### 资源模板

```go
// 服务器端注册资源模板，向客户端声明动态资源形态
mcp.ResourceTemplate("log://app/{date}", "应用日志", "获取指定日期的应用日志").
    WithMimeType("text/plain").
    Register()

// 客户端检索资源模板并读取具体资源
templates, err := client.ListResourceTemplates(ctx, "")
if err != nil {
    log.Fatalf("列出资源模板失败: %v", err)
}

for _, tpl := range templates.ResourceTemplates {
    fmt.Printf("模板: %s\n", tpl.URITemplate)
}

resource, err := client.ReadResource(ctx, "log://app/latest")
```

### Sampling (LLM推理) 示例

```go
// 服务器端：使用Sampling的AI工具
mcp.Tool("ai_calculator", "使用AI进行数学计算").
    WithStringParam("expression", "数学表达式", true).
    HandleWithElicitation(func(ctx *server.MCPContext, args map[string]interface{}) (*protocol.CallToolResult, error) {
        expression := args["expression"].(string)

        // 发起LLM推理请求
        result, err := ctx.CreateTextMessageWithSystem(
            "你是一个数学计算助手，只返回计算结果",
            fmt.Sprintf("计算: %s", expression),
            100,
        )
        if err != nil {
            return protocol.NewToolResultError(fmt.Sprintf("AI计算失败: %v", err)), nil
        }

        // 提取AI响应
        if textContent, ok := result.Content.(protocol.TextContent); ok {
            return protocol.NewToolResultText(fmt.Sprintf("计算结果: %s", textContent.Text)), nil
        }

        return protocol.NewToolResultError("无法解析AI响应"), nil
    })
```

## 协议支持

### MCP 标准合规性

**完全符合 MCP 2025-06-18 规范**，向后兼容 MCP 2025-03-26, 2024-11-05

### 传输协议

| 协议 | 使用场景 | 官方支持 | 协议版本 |
|------|----------|------|----------|
| **STDIO** | 子进程通信 | 官方标准 | 2024-11-05+ |
| **SSE** | Web 应用 | 官方标准 | 2024-11-05+ |
| **Streamable HTTP** | 现代 Web 应用 | 官方标准 | 2025-06-18 |
| ~~**WebSocket**~~ | ~~实时应用~~ | 非官方标准 | - |
| ~~**gRPC**~~ | ~~微服务~~ | 非官方标准 | - |

**支持的协议版本**: 2025-06-18, 2025-03-26, 2024-11-05

## 开发指南

### 错误处理

```go
// 服务器端
return protocol.NewToolResultError("参数错误"), nil  // 业务错误
return nil, fmt.Errorf("系统错误")                    // 系统错误

// 客户端
if result.IsError {
    // 处理业务错误
}
```

### 学习路径

1. 快速开始示例 → 基本概念
2. [Calculator](./examples/calculator/) → 工具注册和调用
3. [SSE Demo](./examples/sse-demo/) → SSE 传输
4. [Streamable Demo](./examples/streamable-demo/) → 最新传输协议

## 贡献

我们欢迎各种形式的贡献！

1. **报告 Bug** - 提交 Issue 描述问题
2. **功能建议** - 提出新功能想法
3. **改进文档** - 完善文档和示例
4. **代码贡献** - 提交 Pull Request

请查看 [贡献指南](CONTRIBUTING.md) 了解详细信息。

## 许可证

MIT License - 详见 [LICENSE](LICENSE) 文件

## Roadmap

- [x] **结构化工具输出** - 支持类型化、验证的工具结果 (MCP 2025-06-18)
- [x] **用户交互请求 (Elicitation)** - 服务器可在交互过程中请求用户输入 (MCP 2025-06-18)
- [x] **LLM采样支持 (Sampling)** - 服务器发起的LLM推理请求，支持递归AI交互
- [x] **资源模板 (Resource Templates)** - 支持动态资源模板和URI模板 (如 `file:///{path}`)
- [ ] **进度跟踪 (Progress Tracking)** - 长时间运行操作的实时进度反馈和取消机制
- [ ] **参数自动补全 (Completion)** - 工具和提示参数的智能补全建议
- [ ] **根目录管理 (Roots)** - 客户端文件系统根目录管理和变更通知

- [ ] **结构化日志 (Logging)** - 服务器向客户端发送结构化日志消息
- [ ] **资源订阅 (Resource Subscription)** - 实时资源变更通知和订阅机制
- [ ] **请求取消 (Cancellation)** - 支持取消长时间运行的操作

- [ ] **基础会话管理** - 支持每客户端独立状态管理
- [ ] **简单中间件系统** - 提供基本的请求/响应拦截能力
- [ ] **CLI工具** - 开发、测试和调试MCP服务器的命令行工具
- [ ] **OAuth 2.1授权支持** - 企业级安全认证机制
- [ ] **高级工具过滤** - 基于用户角色的工具访问控制

## 相关项目

- [MCP 官方规范](https://github.com/anthropics/model-context-protocol) - 协议规范定义
- [MCP Python SDK](https://github.com/anthropics/model-context-protocol/tree/main/src/mcp) - Python 实现
- [MCP TypeScript SDK](https://github.com/anthropics/model-context-protocol/tree/main/src/mcp) - TypeScript 实现

---
