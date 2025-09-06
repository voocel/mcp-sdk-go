package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
)

func main() {
	ctx := context.Background()
	mcpClient, err := client.New(
		client.WithSSETransport("http://localhost:8080/sse"),
		client.WithClientInfo("Elicitation Demo Client", "1.0.0"),
		client.WithElicitationHandler(handleElicitation),
	)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}
	defer mcpClient.Close()

	// 初始化连接
	initResult, err := mcpClient.Initialize(ctx, protocol.ClientInfo{
		Name:    "Elicitation Demo Client",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}

	fmt.Printf("连接成功！\n")
	fmt.Printf("服务器: %s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	fmt.Printf("协议版本: %s\n\n", initResult.ProtocolVersion)

	// 发送初始化完成通知
	if err := mcpClient.SendInitialized(ctx); err != nil {
		log.Fatalf("发送初始化完成通知失败: %v", err)
	}

	// 列出可用工具
	tools, err := mcpClient.ListTools(ctx, "")
	if err != nil {
		log.Fatalf("列出工具失败: %v", err)
	}

	fmt.Println("可用工具:")
	for i, tool := range tools.Tools {
		fmt.Printf("%d. %s - %s\n", i+1, tool.Name, tool.Description)
	}
	fmt.Println()

	// 交互式工具调用
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println("请选择要调用的工具:")
		fmt.Println("1. user_profile - 创建用户档案")
		fmt.Println("2. book_restaurant - 预订餐厅")
		fmt.Println("3. calculator - 计算器")
		fmt.Println("4. 退出")
		fmt.Print("请输入选择 (1-4): ")

		if !scanner.Scan() {
			break
		}

		choice := strings.TrimSpace(scanner.Text())
		switch choice {
		case "1":
			callUserProfile(ctx, mcpClient)
		case "2":
			callBookRestaurant(ctx, mcpClient, scanner)
		case "3":
			callCalculator(ctx, mcpClient, scanner)
		case "4":
			fmt.Println("再见！")
			return
		default:
			fmt.Println("无效选择，请重试")
		}
		fmt.Println()
	}
}

// handleElicitation 处理服务器的 elicitation 请求
func handleElicitation(ctx context.Context, params *protocol.ElicitationCreateParams) (*protocol.ElicitationResult, error) {
	fmt.Printf("\n服务器请求: %s\n", params.Message)

	// 解析 schema 以了解期望的输入
	properties, ok := params.RequestedSchema["properties"].(map[string]interface{})
	if !ok {
		return protocol.NewElicitationCancel(), nil
	}

	result := make(map[string]interface{})
	scanner := bufio.NewScanner(os.Stdin)

	for propName, propDef := range properties {
		propMap, ok := propDef.(map[string]interface{})
		if !ok {
			continue
		}

		propType, _ := propMap["type"].(string)
		description, _ := propMap["description"].(string)

		fmt.Printf("请输入 %s", propName)
		if description != "" {
			fmt.Printf(" (%s)", description)
		}
		fmt.Print(": ")

		if !scanner.Scan() {
			return protocol.NewElicitationCancel(), nil
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Println("输入为空，取消操作")
			return protocol.NewElicitationCancel(), nil
		}

		// 根据类型转换输入
		switch propType {
		case "string":
			// 检查是否是枚举类型
			if enum, ok := propMap["enum"].([]interface{}); ok {
				found := false
				for _, option := range enum {
					if input == option.(string) {
						found = true
						break
					}
				}
				if !found {
					fmt.Printf("无效选择，可选项: %v\n", enum)
					return protocol.NewElicitationCancel(), nil
				}
			}
			result[propName] = input
		case "number", "integer":
			if num, err := strconv.ParseFloat(input, 64); err != nil {
				fmt.Printf("无效数字: %s\n", input)
				return protocol.NewElicitationCancel(), nil
			} else {
				result[propName] = num
			}
		case "boolean":
			switch strings.ToLower(input) {
			case "true", "yes", "y", "1", "是":
				result[propName] = true
			case "false", "no", "n", "0", "否":
				result[propName] = false
			default:
				fmt.Printf("无效布尔值: %s\n", input)
				return protocol.NewElicitationCancel(), nil
			}
		default:
			result[propName] = input
		}
	}

	return protocol.NewElicitationAccept(result), nil
}

// callUserProfile 调用用户档案工具
func callUserProfile(ctx context.Context, mcpClient client.Client) {
	fmt.Println("\n创建用户档案...")

	result, err := mcpClient.CallTool(ctx, "user_profile", map[string]interface{}{})
	if err != nil {
		fmt.Printf("调用失败: %v\n", err)
		return
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(*protocol.TextContent); ok {
			fmt.Printf("结果:\n%s\n", textContent.Text)
		}
	}
}

// callBookRestaurant 调用餐厅预订工具
func callBookRestaurant(ctx context.Context, mcpClient client.Client, scanner *bufio.Scanner) {
	fmt.Println("\n预订餐厅...")

	fmt.Print("请输入预订日期 (YYYY-MM-DD): ")
	if !scanner.Scan() {
		return
	}
	date := strings.TrimSpace(scanner.Text())

	fmt.Print("请输入用餐人数: ")
	if !scanner.Scan() {
		return
	}
	partySizeStr := strings.TrimSpace(scanner.Text())
	partySize, err := strconv.Atoi(partySizeStr)
	if err != nil {
		fmt.Printf("无效人数: %s\n", partySizeStr)
		return
	}

	result, err := mcpClient.CallTool(ctx, "book_restaurant", map[string]interface{}{
		"date":       date,
		"party_size": partySize,
	})
	if err != nil {
		fmt.Printf("调用失败: %v\n", err)
		return
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(*protocol.TextContent); ok {
			fmt.Printf("结果:\n%s\n", textContent.Text)
		}
	}
}

// callCalculator 调用计算器工具
func callCalculator(ctx context.Context, mcpClient client.Client, scanner *bufio.Scanner) {
	fmt.Println("\n计算器...")

	fmt.Print("请输入操作 (add, subtract, multiply, divide): ")
	if !scanner.Scan() {
		return
	}
	operation := strings.TrimSpace(scanner.Text())

	fmt.Print("请输入第一个数字: ")
	if !scanner.Scan() {
		return
	}
	aStr := strings.TrimSpace(scanner.Text())
	a, err := strconv.ParseFloat(aStr, 64)
	if err != nil {
		fmt.Printf("无效数字: %s\n", aStr)
		return
	}

	fmt.Print("请输入第二个数字: ")
	if !scanner.Scan() {
		return
	}
	bStr := strings.TrimSpace(scanner.Text())
	b, err := strconv.ParseFloat(bStr, 64)
	if err != nil {
		fmt.Printf("无效数字: %s\n", bStr)
		return
	}

	result, err := mcpClient.CallTool(ctx, "calculator", map[string]interface{}{
		"operation": operation,
		"a":         a,
		"b":         b,
	})
	if err != nil {
		fmt.Printf("调用失败: %v\n", err)
		return
	}

	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(*protocol.TextContent); ok {
			fmt.Printf("结果: %s\n", textContent.Text)
		}
	}
}
