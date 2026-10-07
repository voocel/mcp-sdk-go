# MCP Go SDK

<div align="center">

<strong>优雅的无状态 MCP (Model Context Protocol) Go 实现</strong>

[![English](https://img.shields.io/badge/lang-English-blue.svg)](./README.md)
[![中文](https://img.shields.io/badge/lang-中文-red.svg)](./README_CN.md)

![License](https://img.shields.io/badge/license-MIT-blue.svg)
[![Go Reference](https://pkg.go.dev/badge/github.com/voocel/mcp-sdk-go.svg)](https://pkg.go.dev/github.com/voocel/mcp-sdk-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/voocel/mcp-sdk-go)](https://goreportcard.com/report/github.com/voocel/mcp-sdk-go)
[![Build Status](https://github.com/voocel/mcp-sdk-go/workflows/go/badge.svg)](https://github.com/voocel/mcp-sdk-go/actions)

</div>

## 简介

面向 **MCP 2026-07-28**（协议的无状态修订版）从零构建的 Go SDK。本 SDK 只实现 2026-07-28 —— 没有 `initialize` 握手、没有会话、没有过时传输 —— API 因此小而明确，与线上传输的内容一一对应。

同时提供官方 **tasks 扩展**（`io.modelcontextprotocol/tasks`）的首个 Go 实现：可持久化、可轮询、可取消的长时工具调用，含 `input_required` 输入会合机制。

## 特性

- **彻底无状态** —— 服务端是纯函数：一条消息进，一串消息出。所有请求级上下文经 `_meta` 传递，两侧都没有 Session 对象。
- **类型化工具** —— 输入 schema 从 Go 结构体自动推导，参数在 handler 执行前完成校验，结构化输出由返回类型生成。
- **MRTR（多轮往返请求）** —— 2026-07-28 中服务端发起请求的替代机制。服务端以编译期安全的和类型返回 `input_required` 中间结果；客户端自动补全 elicitation 并重试。
- **订阅** —— 带过滤器的 `subscriptions/listen` 通知流，由非阻塞 hub 支撑。
- **Tasks 扩展** —— `tasks/get`、`tasks/update`、`tasks/cancel`、`notifications/tasks`，按每请求能力门控并支持同步回退，Store 可插拔。
- **两种传输** —— STDIO（换行分帧、逐请求 goroutine、`notifications/cancelled`）与 Streamable HTTP（单 POST 端点、JSON/SSE 响应协商、断流即取消、完整 `Mcp-*` 头校验）。
- **OAuth 授权** —— MCP 授权的客户端实现：Protected Resource Metadata 与授权服务器发现、Client ID Metadata Documents 与预注册客户端、回环重定向上带 PKCE 的授权码流程、RFC 8707 `resource`、RFC 9207 `iss` 校验、step-up 权限升级，以及负责携带和刷新 token 的 `http.RoundTripper`。
- **安全默认值** —— Origin 校验默认开启、消息体大小限制、路径穿越防护（`os.OpenRoot`）、MRTR `requestState` HMAC 助手、panic 不向 wire 泄露堆栈。
- **一致性测试** —— 规范 wire 示例作为 fixture 进 CI：每个结果的 `resultType`、`_meta` 保留键规则、HTTP 状态映射、头/体校验矩阵、哨兵编码。

## 环境要求

- Go 1.25+

```bash
go get github.com/voocel/mcp-sdk-go
```

## 快速开始

### 服务端（STDIO）

```go
package main

import (
	"context"
	"log"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
)

type greetIn struct {
	Name string `json:"name" jsonschema:"required"`
}

func main() {
	srv := server.New(&server.Options{
		Impl: protocol.Implementation{Name: "my-server", Version: "1.0.0"},
	})

	server.AddTool(srv, &protocol.Tool{Name: "greet", Description: "打个招呼"},
		func(ctx context.Context, req *server.CallRequest, in greetIn) (protocol.ToolResponse, string, error) {
			return nil, "你好，" + in.Name + "！", nil
		})

	if err := stdio.Serve(context.Background(), srv, nil); err != nil {
		log.Fatal(err)
	}
}
```

同一个 `srv` 挂到 HTTP 只需一行 —— 服务端核心与传输无关：

```go
http.Handle("/mcp", streamhttp.NewHandler(srv, nil))
```

### 客户端

```go
c := client.New(streamhttp.New("http://localhost:8080/mcp", nil), &client.Options{
	Info: &protocol.Implementation{Name: "my-client", Version: "1.0.0"},
})
defer c.Close()

res, err := c.CallTool(ctx, &protocol.CallToolParams{
	Name:      "greet",
	Arguments: map[string]any{"name": "Ada"},
})
```

客户端在每个请求上自动盖 `_meta`（协议版本、推导能力、clientInfo）与必需的 `Mcp-*` HTTP 头。连接子进程服务端时，用 `stdio.NewCommand(exec.Command(...), nil)` 作为传输。

### 授权

```go
authz := auth.New(auth.Config{
	Server:            endpoint,
	Store:             store,
	ClientMetadataURL: "https://example.com/oauth/client.json",
})
c := client.New(streamhttp.New(endpoint, &streamhttp.TransportOptions{
	HTTPClient: &http.Client{Transport: authz.Transport(http.DefaultTransport)},
}), nil)

_, err := c.Discover(ctx)
var se *streamhttp.StatusError
if errors.As(err, &se) && se.StatusCode == http.StatusUnauthorized {
	l, _ := authz.Login(ctx) // 发现授权服务器、在回环重定向 URI 上监听
	openBrowser(l.URL)
	err = l.Wait(ctx) // 保存 token，之后的请求都会带上它
}
```

客户端按 2026-07-28 规范规定的顺序表明身份：优先用事先在授权服务器注册的客户端（`ClientID`，有密钥再加 `ClientSecret`；重定向 URI 为 `http://127.0.0.1/callback`，端口任意），否则用它的 Client ID Metadata Document（`ClientMetadataURL`），由授权服务器去拉取。文档里列出固定端口的回环重定向 URI（多列几个，登录时监听第一个空闲的），并声明 `"token_endpoint_auth_method": "none"`。两者都不接受的授权服务器，`Login` 返回 `auth.ErrClientRequired`。规范已弃用的动态客户端注册不支持。

`Store` 按服务器 URL 持久化 token；token 是机密，要存在只有用户能读的地方。传输层在 token 过期前刷新，服务器拒绝时再刷新一次；仍然解决不了的 401 或 403 以 `*streamhttp.StatusError` 返回，由调用方重新登录。

### Elicitation（MRTR）

工具通过返回中间结果向用户请求输入；客户端自动补全并重试：

```go
// 服务端：handler 可重入 —— 先查答案，没有则发问。
srv.AddTool(&protocol.Tool{Name: "signup"}, func(ctx context.Context, req *server.CallRequest) (protocol.ToolResponse, error) {
	if er, err := req.Params.InputResponses.Elicit("email"); err == nil {
		return protocol.NewToolResultText("已注册 " + er.Content["email"].(string)), nil
	}
	return protocol.RequireInput(protocol.InputRequests{
		"email": protocol.NewElicitFormRequest("请输入邮箱", schema),
	}, ""), nil
})

// 客户端：Elicitor 同时声明能力并在自动循环中作答。
c := client.New(tr, &client.Options{
	Elicitor: func(ctx context.Context, p *protocol.ElicitParams) (*protocol.ElicitResult, error) {
		return &protocol.ElicitResult{Action: protocol.ElicitActionAccept,
			Content: map[string]any{"email": "ada@example.com"}}, nil
	},
})
```

### Tasks（官方扩展）

```go
// 服务端
tk := tasks.Install(srv, tasks.NewMemStore(), nil)
tasks.AddTool(tk, &protocol.Tool{Name: "research"},
	func(ctx context.Context, tc *tasks.Context, in researchIn) (*protocol.CallToolResult, error) {
		// 长时工作；tc.RequireInput 可让任务进入 input_required 等待客户端作答
		return protocol.NewToolResultText("完成"), nil
	})

// 客户端
_, err := c.CallTool(ctx, &protocol.CallToolParams{Name: "research"})
if task, ok := tasks.AsTask(err); ok {
	final, _ := tasks.Await(ctx, c, task, nil) // 轮询直到终态
	res, _ := final.ToolResult()
}
```

未声明该能力的客户端将得到同步执行的结果（或按 `tasks.Options.Reject` 收到 `-32021` 拒绝）。

## 包结构

| 包 | 职责 |
|---|---|
| `protocol` | 2026-07-28 wire 类型，零第三方依赖 |
| `server` | 无状态服务端核心：方法表、类型化注册、hub、中间件 |
| `client` | 类型化客户端：`_meta`/头盖章、MRTR 循环、翻页迭代器、订阅 |
| `transport` | 客户端传输抽象（`Do(ctx, req) → Stream`） |
| `transport/stdio` | `Serve`（服务端）与 `Command`（子进程客户端） |
| `transport/streamhttp` | Streamable HTTP 的 `Handler`（服务端）与 `Transport`（客户端） |
| `transport/mem` | 进程内传输，用于测试与嵌入 |
| `ext/tasks` | 官方 tasks 扩展：服务端、客户端与存储 |
| `auth` | MCP 授权的客户端：发现、登录、token 刷新 |

## 示例

| 示例 | 演示内容 |
|---|---|
| [basic](./examples/basic) | STDIO 上的工具 + 资源 + 提示词 |
| [calculator](./examples/calculator) | 类型化工具的 schema 推导与校验 |
| [file-server](./examples/file-server) | 带路径穿越防护的文件资源 |
| [streamhttp](./examples/streamhttp) | HTTP 服务端 + 客户端：SSE 进度、订阅 |
| [elicitation](./examples/elicitation) | 单进程内完整 MRTR 循环 |
| [tasks](./examples/tasks) | 任务全生命周期与 `input_required` 会合 |

## 范围说明

本 SDK 只实现 MCP 2026-07-28。该修订版移除或废弃的特性刻意不做：`initialize`/会话、Roots、Sampling、Logging、`resources/subscribe`、ping、SSE 断流恢复（`Last-Event-ID`）。

对其他修订版保留两处让步，因为规范把它们都定为 MUST：`server/discover` 对任何协议版本的调用方都作答，使其能从中读到 `supportedVersions`；客户端把缺少 `resultType` 的结果视为 `complete`。未知的 `InputRequest` 方法（如旧服务端的 `roots/list`）原样透传并交由调用方处理，不会被静默丢弃。

## 许可证

MIT
