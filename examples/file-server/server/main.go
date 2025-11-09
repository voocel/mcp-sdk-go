package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

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
		log.Println("接收到关闭信号")
		cancel()
	}()

	mcpServer := server.NewServer(&protocol.ServerInfo{
		Name:    "文件服务器",
		Version: "1.0.0",
	}, nil)

	// 注册文件列表工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "list_directory",
			Description: "列出指定目录中的文件",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "目录路径",
					},
				},
				"required": []string{"path"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			path, ok := req.Params.Arguments["path"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'path' 必须是字符串"), nil
			}

			// 安全检查：防止路径遍历攻击
			if strings.Contains(path, "..") {
				return protocol.NewToolResultError("不允许访问上级目录"), nil
			}

			files, err := ioutil.ReadDir(path)
			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("无法读取目录: %v", err)), nil
			}

			var fileList []string
			for _, file := range files {
				fileType := "文件"
				if file.IsDir() {
					fileType = "目录"
				}
				fileList = append(fileList, fmt.Sprintf("%s (%s, %d 字节)", file.Name(), fileType, file.Size()))
			}

			if len(fileList) == 0 {
				return protocol.NewToolResultText("目录为空"), nil
			}

			result := strings.Join(fileList, "\n")
			return protocol.NewToolResultText(result), nil
		},
	)

	// 注册当前目录资源
	currentDir, _ := os.Getwd()
	mcpServer.AddResource(
		&protocol.Resource{
			URI:         "file://current",
			Name:        "当前工作目录",
			Description: "当前工作目录的路径",
			MimeType:    "text/plain",
		},
		func(ctx context.Context, req *server.ReadResourceRequest) (*protocol.ReadResourceResult, error) {
			contents := protocol.NewTextResourceContents("file://current", currentDir)
			return protocol.NewReadResourceResult(contents), nil
		},
	)

	// 注册文件读取工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "read_file",
			Description: "读取文件内容",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径",
					},
				},
				"required": []string{"path"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			path, ok := req.Params.Arguments["path"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'path' 必须是字符串"), nil
			}

			// 安全检查：防止路径遍历攻击
			if strings.Contains(path, "..") {
				return protocol.NewToolResultError("不允许访问上级目录"), nil
			}

			// 检查文件大小（限制为 1MB）
			if fileInfo, err := os.Stat(path); err == nil {
				if fileInfo.Size() > 1024*1024 {
					return protocol.NewToolResultError("文件太大（超过 1MB 限制）"), nil
				}
			}

			content, err := ioutil.ReadFile(path)
			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("无法读取文件: %v", err)), nil
			}

			return protocol.NewToolResultText(string(content)), nil
		},
	)

	// 注册文件搜索工具
	mcpServer.AddTool(
		&protocol.Tool{
			Name:        "search_files",
			Description: "搜索包含特定内容的文件",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"directory": map[string]interface{}{
						"type":        "string",
						"description": "搜索目录",
					},
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "搜索内容",
					},
				},
				"required": []string{"directory", "pattern"},
			},
		},
		func(ctx context.Context, req *server.CallToolRequest) (*protocol.CallToolResult, error) {
			directory, ok := req.Params.Arguments["directory"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'directory' 必须是字符串"), nil
			}
			pattern, ok := req.Params.Arguments["pattern"].(string)
			if !ok {
				return protocol.NewToolResultError("参数 'pattern' 必须是字符串"), nil
			}

			// 安全检查：防止路径遍历攻击
			if strings.Contains(directory, "..") {
				return protocol.NewToolResultError("不允许访问上级目录"), nil
			}

			var results []string

			err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil // 忽略无法访问的文件
				}

				if info.IsDir() {
					return nil
				}

				// 只搜索小于 10MB 的文本文件
				if info.Size() < 10*1024*1024 && isTextFile(path) {
					content, err := ioutil.ReadFile(path)
					if err != nil {
						return nil // 忽略无法读取的文件
					}

					if strings.Contains(string(content), pattern) {
						results = append(results, path)
					}
				}

				return nil
			})

			if err != nil {
				return protocol.NewToolResultError(fmt.Sprintf("搜索文件时出错: %v", err)), nil
			}

			if len(results) == 0 {
				return protocol.NewToolResultText("未找到匹配的文件"), nil
			}

			result := fmt.Sprintf("找到 %d 个匹配的文件:\n%s", len(results), strings.Join(results, "\n"))
			return protocol.NewToolResultText(result), nil
		},
	)

	// 注册文件帮助提示模板
	mcpServer.AddPrompt(
		&protocol.Prompt{
			Name:        "file_help",
			Description: "文件操作帮助",
		},
		func(ctx context.Context, req *server.GetPromptRequest) (*protocol.GetPromptResult, error) {
			messages := []protocol.PromptMessage{
				protocol.NewPromptMessage(protocol.RoleSystem, protocol.NewTextContent(
					"这是一个文件服务器，提供文件和目录操作功能。支持列出目录内容、读取文件和搜索文件。")),
				protocol.NewPromptMessage(protocol.RoleUser, protocol.NewTextContent(
					"我该如何使用文件服务器？")),
				protocol.NewPromptMessage(protocol.RoleAssistant, protocol.NewTextContent(
					"你可以使用以下功能：\n1. list_directory - 列出目录中的文件\n2. read_file - 读取文件内容\n3. search_files - 在目录中搜索包含特定内容的文件\n4. 访问 file://current 资源获取当前目录路径")),
			}
			return protocol.NewGetPromptResult("文件服务器操作指南", messages...), nil
		},
	)

	handler := sse.NewHTTPHandler(func(r *http.Request) *server.Server {
		return mcpServer
	})
	httpServer := &http.Server{
		Addr:    ":8081",
		Handler: handler,
	}

	log.Println("启动文件服务器 MCP 服务 (SSE) 在端口 :8081...")

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器错误: %v", err)
		}
	}()

	<-ctx.Done()

	log.Println("服务器已关闭")
}

func isTextFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	textExtensions := []string{
		".txt", ".md", ".go", ".js", ".ts", ".html", ".css", ".json",
		".xml", ".csv", ".log", ".yaml", ".yml", ".toml", ".ini",
		".py", ".java", ".c", ".cpp", ".h", ".hpp", ".rs", ".php",
		".rb", ".sh", ".bat", ".ps1", ".dockerfile", ".makefile",
	}

	for _, ext := range textExtensions {
		if extension == ext {
			return true
		}
	}

	// 检查没有扩展名的常见文本文件
	filename := strings.ToLower(filepath.Base(path))
	textFiles := []string{"readme", "license", "changelog", "makefile", "dockerfile"}
	for _, textFile := range textFiles {
		if filename == textFile {
			return true
		}
	}

	return false
}
