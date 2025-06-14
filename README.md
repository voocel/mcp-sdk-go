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

MCP Go SDK 是模型上下文协议（Model Context Protocol）的 Go 语言实现，完全符合 MCP 2024-11-05 规范，提供了与大语言模型交互的标准化接口。

## 🌟 功能特点

- ✅ **完全符合 MCP 标准** - 100% 遵循 MCP 2024-11-05 规范
- 🔧 **工具管理** - 注册和调用各种工具
- 📁 **资源访问** - 读取和管理各种资源
- 💬 **提示模板** - 支持参数化提示模板
- 🌐 **多种传输** - STDIO、SSE 传输支持
- 🛡️ **类型安全** - 完整的类型定义和验证
- ⚡ **高性能** - 优化的并发处理
- 🔒 **安全性** - 内置安全检查和防护
- 🎯 **易于使用** - 简洁的链式 API

## 📦 安装

```bash
go get github.com/voocel/mcp-sdk-go
```

## 🚀 快速开始

### 基础服务器示例

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

    // 创建 SSE 传输服务器
    sseServer := sse.NewServer(":8080", mcp)
    
    log.Println("🚀 服务器启动在 http://localhost:8080")
    if err := sseServer.Serve(ctx); err != nil && err != context.Canceled {
        log.Fatalf("服务器错误: %v", err)
    }
}
```

### 基础客户端示例

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

    fmt.Printf("✅ 连接成功！服务器: %s v%s\n", 
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
            fmt.Printf("🎉 %s\n", textContent.Text)
        }
    }

    // 读取资源
    resource, err := mcpClient.ReadResource(ctx, "info://server")
    if err != nil {
        log.Fatalf("读取资源失败: %v", err)
    }
    
    if len(resource.Contents) > 0 {
        fmt.Printf("📋 服务器信息: %s\n", resource.Contents[0].Text)
    }
}
```

## 📁 完整示例项目

### 1. 🧮 [Calculator](./examples/calculator/) - 计算器服务
一个简单的数学计算器服务，支持基本的四则运算。

**功能特性:**
- ✅ 加法、减法、乘法、除法工具
- ✅ 错误处理（除零检查）
- ✅ 参数验证
- ✅ 提示模板支持
- 🔌 **传输**: STDIO

**运行方式:**
```bash
# 服务器
cd examples/calculator/server && go run main.go

# 客户端（需要另一个终端）
cd examples/calculator/client && go run main.go
```

### 2. 💬 [Chatbot](./examples/chatbot/) - 聊天机器人服务
一个友好的聊天机器人，提供问候、天气查询和翻译服务。

**功能特性:**
- 👋 随机问候语生成
- 🌤️ 模拟天气查询
- 🔤 简单中英文翻译
- 💬 交互式聊天界面
- 🔌 **传输**: SSE (Server-Sent Events)

**运行方式:**
```bash
# 服务器
cd examples/chatbot/server && go run main.go

# 客户端（需要另一个终端）
cd examples/chatbot/client && go run main.go
```

### 3. 📁 [File Server](./examples/file-server/) - 文件服务器
一个安全的文件操作服务，支持目录浏览、文件读取和内容搜索。

**功能特性:**
- 📂 目录内容列表
- 📄 文件内容读取
- 🔍 文件内容搜索
- 🛡️ 路径遍历保护
- 📏 文件大小限制
- 🔌 **传输**: SSE (Server-Sent Events)

**运行方式:**
```bash
# 服务器
cd examples/file-server/server && go run main.go

# 客户端
cd examples/file-server/client && go run main.go
```

### 编译所有示例
```bash
# 从项目根目录运行
cd examples
for dir in calculator chatbot file-server; do
  echo "编译 $dir..."
  cd $dir && go mod tidy
  cd server && go build -v && cd ..
  cd client && go build -v && cd ..
  cd ..
done
```

## 🏗️ 核心架构

### 服务器端架构模式

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

// 注册提示模板
mcp.Prompt("prompt_name", "提示描述").
    WithArgument("arg1", "参数描述", true).
    Handle(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
        messages := []protocol.PromptMessage{
            protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent("内容")),
        }
        return protocol.NewGetPromptResult("描述", messages...), nil
    })

// 启动服务器
transport := sse.NewServer(":8080", mcp)
transport.Serve(ctx)
```

### 客户端架构模式

```go
// 创建客户端
client, err := client.New(
    client.WithSSETransport("http://localhost:8080"),
    client.WithClientInfo("client-name", "1.0.0"),
)

// 初始化连接
initResult, err := client.Initialize(ctx, protocol.ClientInfo{
    Name:    "客户端名称",
    Version: "1.0.0",
})

// 发送初始化完成通知
err = client.SendInitialized(ctx)

// 列出工具
toolsResult, err := client.ListTools(ctx, "")

// 调用工具
result, err := client.CallTool(ctx, "tool_name", map[string]interface{}{
    "param": "value",
})

// 读取资源
resource, err := client.ReadResource(ctx, "resource://uri")

// 获取提示模板
prompt, err := client.GetPrompt(ctx, "prompt_name", map[string]string{
    "arg": "value",
})
```

## 🔧 技术特性

### MCP 标准合规性
完全符合 MCP 2024-11-05 规范：

- ✅ **JSON-RPC 2.0** 消息格式
- ✅ **标准方法名称** (`tools/list`, `tools/call`, `resources/list`, `resources/read`, `prompts/list`, `prompts/get`)
- ✅ **正确的初始化流程** (initialize → initialized)
- ✅ **Capabilities 协商**
- ✅ **错误处理和超时**
- ✅ **类型安全的参数处理**

### 支持的传输方式
- 📡 **STDIO** - 适合子进程通信，官方标准
- 🌐 **SSE (Server-Sent Events)** - 适合 Web 集成，官方标准
- ❌ ~~WebSocket~~ - 已移除（非官方标准）
- ❌ ~~gRPC~~ - 已移除（非官方标准）

### 安全特性
- 🛡️ **输入验证** - 所有参数都经过类型检查
- 🔒 **路径遍历保护** - 防止 `../` 攻击
- 📏 **资源限制** - 文件大小和搜索范围限制
- ⏱️ **超时控制** - 防止长时间阻塞
- 🔐 **类型安全** - 强类型检查和转换

## 🐛 错误处理

SDK 提供了完善的错误处理机制：

```go
// 服务器端错误处理
mcp.Tool("risky_operation", "可能失败的操作").
    WithStringParam("input", "输入参数", true).
    Handle(func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
        input, ok := args["input"].(string)
        if !ok {
            return protocol.NewToolResultError("参数类型错误"), nil
        }
        
        if input == "" {
            return protocol.NewToolResultError("输入不能为空"), nil
        }
        
        // 业务逻辑...
        return protocol.NewToolResultText("操作成功"), nil
    })

// 客户端错误处理
result, err := client.CallTool(ctx, "risky_operation", map[string]interface{}{
    "input": "test",
})
if err != nil {
    log.Printf("网络或协议错误: %v", err)
    return
}

if result.IsError {
    log.Printf("业务逻辑错误: %s", result.Content[0].(protocol.TextContent).Text)
    return
}

// 处理成功结果
```

## 📚 学习路径

1. **🔰 新手** - 从上面的快速开始示例了解基本概念
2. **📊 初级** - 学习 [Calculator 示例](./examples/calculator/)，掌握工具注册和调用
3. **💬 中级** - 研究 [Chatbot 示例](./examples/chatbot/)，理解交互式应用
4. **📁 高级** - 深入 [File Server 示例](./examples/file-server/)，学习资源管理和安全防护
5. **🌐 专家** - 查看 [Standard Server](./examples/standard-server/)，了解完整的 MCP 实现

## 🔄 传输协议对比

| 传输方式 | 使用场景 | 优点 | 缺点 | 官方支持 |
|---------|---------|------|------|----------|
| **STDIO** | 子进程通信 | 简单、可靠 | 单向通信 | ✅ 官方标准 |
| **SSE** | Web 应用 | 实时推送、HTTP 兼容 | 服务器到客户端单向 | ✅ 官方标准 |
| ~~WebSocket~~ | ~~实时应用~~ | ~~双向通信~~ | ~~非标准~~ | ❌ 已移除 |
| ~~gRPC~~ | ~~微服务~~ | ~~高性能~~ | ~~非标准~~ | ❌ 已移除 |

## 🤝 贡献

我们欢迎各种形式的贡献！

1. 🐛 **报告 Bug** - 提交 Issue 描述问题
2. 💡 **功能建议** - 提出新功能想法
3. 📝 **改进文档** - 完善文档和示例
4. 🔧 **代码贡献** - 提交 Pull Request

请查看 [贡献指南](CONTRIBUTING.md) 了解详细信息。

## 📄 许可证

MIT License - 详见 [LICENSE](LICENSE) 文件

## 🔗 相关项目

- [MCP 官方规范](https://github.com/anthropics/model-context-protocol) - 协议规范定义
- [MCP Python SDK](https://github.com/anthropics/model-context-protocol/tree/main/src/mcp) - Python 实现
- [MCP TypeScript SDK](https://github.com/anthropics/model-context-protocol/tree/main/src/mcp) - TypeScript 实现

---

<div align="center">

**🚀 构建更智能的应用，连接更强大的模型 🚀**

*使用 MCP Go SDK，让您的 Go 应用轻松集成大语言模型能力*

</div>
