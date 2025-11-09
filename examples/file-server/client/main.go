package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport/sse"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mcpClient := client.NewClient(&client.ClientInfo{
		Name:    "file-client",
		Version: "1.0.0",
	}, nil)

	transport, err := sse.NewSSETransport("http://localhost:8081")
	if err != nil {
		log.Fatalf("创建 Transport 失败: %v", err)
	}

	fmt.Println("连接到文件服务器...")
	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer session.Close()

	initResult := session.InitializeResult()
	fmt.Printf("连接成功！服务器: %s v%s\n",
		initResult.ServerInfo.Name, initResult.ServerInfo.Version)

	// 获取当前目录资源
	fmt.Println("\n获取当前工作目录...")
	resourceResult, err := session.ReadResource(ctx, &protocol.ReadResourceParams{
		URI: "file://current",
	})
	if err != nil {
		log.Fatalf("读取资源失败: %v", err)
	}

	var currentDir string
	if len(resourceResult.Contents) > 0 && resourceResult.Contents[0].Text != "" {
		currentDir = resourceResult.Contents[0].Text
		fmt.Printf("当前工作目录: %s\n", currentDir)
	} else {
		// 如果资源读取失败，使用当前目录作为后备
		currentDir, _ = os.Getwd()
		fmt.Printf("当前工作目录: %s\n", currentDir)
	}

	// 列出当前目录内容
	fmt.Println("\n当前目录内容:")
	result, err := session.CallTool(ctx, &protocol.CallToolParams{
		Name: "list_directory",
		Arguments: map[string]any{
			"path": currentDir,
		},
	})
	if err != nil {
		log.Fatalf("调用 list_directory 工具失败: %v", err)
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("%s\n", textContent.Text)
		}
	}

	// 读取当前文件内容的前100个字符
	fmt.Println("\n读取当前文件内容预览:")
	_, currentFilePath, _, _ := runtime.Caller(0)
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "read_file",
		Arguments: map[string]any{
			"path": currentFilePath,
		},
	})
	if err != nil {
		fmt.Printf("调用 read_file 工具失败: %v\n", err)
	} else if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			content := textContent.Text
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			fmt.Printf("文件 %s 的前200个字符:\n%s\n", filepath.Base(currentFilePath), content)
		}
	}

	// 搜索包含 "MCP" 的文件
	fmt.Println("\n搜索包含 'MCP' 的文件:")
	searchDir := filepath.Dir(currentDir)
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "search_files",
		Arguments: map[string]any{
			"directory": searchDir,
			"pattern":   "MCP",
		},
	})
	if err != nil {
		fmt.Printf("调用 search_files 工具失败: %v\n", err)
	} else if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("%s\n", textContent.Text)
		}
	}

	// 获取文件操作帮助
	fmt.Println("\n获取文件操作帮助:")
	promptResult, err := session.GetPrompt(ctx, &protocol.GetPromptParams{
		Name:      "file_help",
		Arguments: map[string]string{},
	})
	if err != nil {
		fmt.Printf("获取帮助提示失败: %v\n", err)
	} else {
		fmt.Printf("描述: %s\n", promptResult.Description)
		fmt.Println("帮助信息:")
		for i, message := range promptResult.Messages {
			if textContent, ok := message.Content.(protocol.TextContent); ok {
				fmt.Printf("  %d. [%s]: %s\n", i+1, message.Role, textContent.Text)
			}
		}
	}

	fmt.Println("\n演示错误处理 - 尝试访问不存在的目录:")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "list_directory",
		Arguments: map[string]any{
			"path": "/nonexistent/directory",
		},
	})
	if err != nil {
		fmt.Printf("预期错误: %v\n", err)
	} else if result.IsError && len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("服务器返回错误: %s\n", textContent.Text)
		}
	}

	fmt.Println("\n演示安全检查 - 尝试路径遍历:")
	result, err = session.CallTool(ctx, &protocol.CallToolParams{
		Name: "read_file",
		Arguments: map[string]any{
			"path": "../../../etc/passwd",
		},
	})
	if err != nil {
		fmt.Printf("安全检查生效: %v\n", err)
	} else if result.IsError && len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(protocol.TextContent); ok {
			fmt.Printf("安全检查生效: %s\n", textContent.Text)
		}
	}

	fmt.Println("\n end!")
}
