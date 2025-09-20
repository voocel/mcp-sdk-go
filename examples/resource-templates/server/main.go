package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalCh
		cancel()
	}()

	mcp := server.NewFastMCP("资源模板演示", "1.0.0")

	// 注册资源模板，告知客户端可访问的日志形态
	if err := mcp.ResourceTemplate("log://app/{date}", "应用日志", "获取指定日期的应用日志").
		WithMimeType("text/plain").
		Register(); err != nil {
		log.Fatalf("注册资源模板失败: %v", err)
	}

	logs := map[string]string{
		"log://app/2025-02-01": "[2025-02-01 10:00] 应用启动\n[2025-02-01 10:05] 处理请求 /health",
		"log://app/2025-02-02": "[2025-02-02 09:12] 定时任务执行\n[2025-02-02 11:43] 生成报表",
		"log://app/latest":     "[" + time.Now().Format(time.RFC3339) + "] 系统运行正常",
	}

	for uri, content := range logs {
		resourceURI := uri
		logContent := content
		if err := mcp.Resource(resourceURI, "日志"+resourceURI, "示例日志数据").
			Handle(func(ctx context.Context) (*protocol.ReadResourceResult, error) {
				return protocol.NewReadResourceResult(protocol.NewTextResourceContents(resourceURI, logContent)), nil
			}); err != nil {
			log.Fatalf("注册资源失败: %v", err)
		}
	}

	sseServer := sse.NewServer(":8080", mcp)
	log.Println("资源模板演示服务器已启动 http://localhost:8080")
	if err := sseServer.Serve(ctx); err != nil && err != context.Canceled {
		log.Fatalf("服务器错误: %v", err)
	}
}
