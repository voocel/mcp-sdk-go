package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/voocel/mcp-sdk-go/protocol"
)

const serverURL = "http://localhost:8081/mcp"

var sessionID string

func main() {
	ctx := context.Background()

	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("Streamable HTTP 客户端测试")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println()

	// ========== Initialize ==========
	log.Println("初始化连接")
	initResult, sid, err := sendRequestWithSession(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "HTTP测试客户端",
			"version": "1.0.0",
		},
	})
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}
	sessionID = sid
	log.Printf("初始化成功 (Session ID: %s)\n", sessionID)
	log.Printf("响应: %s\n\n", prettyJSON(initResult))

	// ========== 列出工具 ==========
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("列出所有工具")
	toolsResult, err := sendRequest(ctx, "tools/list", map[string]interface{}{})
	if err != nil {
		log.Fatalf("列出工具失败: %v", err)
	}

	var toolsList struct {
		Tools []protocol.Tool `json:"tools"`
	}
	if err := json.Unmarshal(toolsResult, &toolsList); err != nil {
		log.Fatalf("解析工具列表失败: %v", err)
	}

	log.Printf("找到 %d 个工具:\n", len(toolsList.Tools))
	for i, tool := range toolsList.Tools {
		log.Printf("  %d. %s - %s", i+1, tool.Name, tool.Description)
		if tool.OutputSchema != nil {
			log.Printf("     支持结构化输出")
		}
	}
	log.Println()

	// ========== 调用普通文本工具 ==========
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("调用 greet 工具 (文本输出)")
	greetResult, err := sendRequest(ctx, "tools/call", map[string]interface{}{
		"name": "greet",
		"arguments": map[string]interface{}{
			"name":     "测试用户",
			"language": "zh",
		},
	})
	if err != nil {
		log.Printf("调用失败: %v\n", err)
	} else {
		log.Printf("调用成功\n")
		log.Printf("响应: %s\n", prettyJSON(greetResult))
	}
	log.Println()

	// ========== 调用结构化输出工具 - 天气 ==========
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("调用 get_weather 工具 (结构化输出)")
	weatherResult, err := sendRequest(ctx, "tools/call", map[string]interface{}{
		"name": "get_weather",
		"arguments": map[string]interface{}{
			"city": "北京",
		},
	})
	if err != nil {
		log.Printf("调用失败: %v\n", err)
	} else {
		log.Printf("调用成功\n")
		log.Printf("原始响应:\n%s\n", prettyJSON(weatherResult))

		// 解析结构化数据
		var toolResult protocol.CallToolResult
		if err := json.Unmarshal(weatherResult, &toolResult); err == nil {
			if toolResult.StructuredContent != nil {
				log.Println("\n解析结构化数据:")
				data := toolResult.StructuredContent.(map[string]interface{})
				log.Printf("  城市: %v", data["city"])
				log.Printf("  温度: %.1f°C", data["temperature"])
				log.Printf("  湿度: %v%%", data["humidity"])
				log.Printf("  天气: %v", data["condition"])
				log.Printf("  风速: %.1f km/h", data["wind_speed"])
				log.Printf("  时间: %v", data["timestamp"])
			}
		}
	}
	log.Println()

	// ========== 调用嵌套结构化输出工具 ==========
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("调用 get_user_info 工具 (嵌套结构化输出)")
	userResult, err := sendRequest(ctx, "tools/call", map[string]interface{}{
		"name": "get_user_info",
		"arguments": map[string]interface{}{
			"user_id": "12345",
		},
	})
	if err != nil {
		log.Printf("调用失败: %v\n", err)
	} else {
		log.Printf("调用成功\n")
		log.Printf("原始响应:\n%s\n", prettyJSON(userResult))

		// 解析嵌套结构化数据
		var toolResult protocol.CallToolResult
		if err := json.Unmarshal(userResult, &toolResult); err == nil {
			if toolResult.StructuredContent != nil {
				log.Println("\n解析嵌套结构化数据:")
				data := toolResult.StructuredContent.(map[string]interface{})

				log.Printf("  用户ID: %v", data["user_id"])
				log.Printf("  姓名: %v", data["name"])
				log.Printf("  年龄: %v", data["age"])
				log.Printf("  邮箱: %v", data["email"])

				if address, ok := data["address"].(map[string]interface{}); ok {
					log.Printf("  地址:")
					log.Printf("     城市: %v", address["city"])
					log.Printf("     国家: %v", address["country"])
					log.Printf("     邮编: %v", address["zipcode"])
				}

				if skills, ok := data["skills"].([]interface{}); ok {
					log.Printf("  技能: %v", skills)
				}

				if metadata, ok := data["metadata"].(map[string]interface{}); ok {
					log.Printf("  元数据:")
					log.Printf("     创建时间: %v", metadata["created_at"])
					log.Printf("     最后登录: %v", metadata["last_login"])
					log.Printf("     浏览次数: %v", metadata["profile_views"])
					log.Printf("     已验证: %v", metadata["is_verified"])
				}
			}
		}
	}
	log.Println()

	// ========== 批量调用 ==========
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("批量调用 get_weather (多个城市)")
	cities := []string{"上海", "深圳", "杭州", "广州", "成都"}
	for i, city := range cities {
		result, err := sendRequest(ctx, "tools/call", map[string]interface{}{
			"name": "get_weather",
			"arguments": map[string]interface{}{
				"city": city,
			},
		})
		if err != nil {
			log.Printf("  [%d] %s: 失败 - %v\n", i+1, city, err)
			continue
		}

		var toolResult protocol.CallToolResult
		if err := json.Unmarshal(result, &toolResult); err == nil {
			if data, ok := toolResult.StructuredContent.(map[string]interface{}); ok {
				log.Printf("  [%d] %s: %.1f°C, %v%% 湿度, %v",
					i+1, data["city"], data["temperature"], data["humidity"], data["condition"])
			}
		}
	}

	log.Println()
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("END")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

var requestID = 1

func sendRequest(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	result, _, err := sendRequestWithSession(ctx, method, params)
	return result, err
}

func sendRequestWithSession(ctx context.Context, method string, params interface{}) (json.RawMessage, string, error) {
	req := protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf(`%d`, requestID)),
		Method:  method,
	}
	requestID++

	if params != nil {
		paramsBytes, err := json.Marshal(params)
		if err != nil {
			return nil, "", fmt.Errorf("marshal params: %w", err)
		}
		req.Params = paramsBytes
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", serverURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("MCP-Protocol-Version", "2025-06-18")

	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	newSessionID := resp.Header.Get("Mcp-Session-Id")

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp protocol.JSONRPCMessage
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, "", fmt.Errorf("unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, "", fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, newSessionID, nil
}

func prettyJSON(data json.RawMessage) string {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(data)
	}
	return string(pretty)
}
