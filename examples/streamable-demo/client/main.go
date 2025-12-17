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
	log.Println("Streamable HTTP Client Test")
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println()

	// ========== Initialize ==========
	log.Println("Initializing connection")
	initResult, sid, err := sendRequestWithSession(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "HTTP Test Client",
			"version": "1.0.0",
		},
	})
	if err != nil {
		log.Fatalf("Initialization failed: %v", err)
	}
	sessionID = sid
	log.Printf("Initialization successful (Session ID: %s)\n", sessionID)
	log.Printf("Response: %s\n\n", prettyJSON(initResult))

	// ========== List Tools ==========
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("Listing all tools")
	toolsResult, err := sendRequest(ctx, "tools/list", map[string]interface{}{})
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}

	var toolsList struct {
		Tools []protocol.Tool `json:"tools"`
	}
	if err := json.Unmarshal(toolsResult, &toolsList); err != nil {
		log.Fatalf("Failed to parse tools list: %v", err)
	}

	log.Printf("Found %d tools:\n", len(toolsList.Tools))
	for i, tool := range toolsList.Tools {
		log.Printf("  %d. %s - %s", i+1, tool.Name, tool.Description)
		if tool.OutputSchema != nil {
			log.Printf("     Supports structured output")
		}
	}
	log.Println()

	// ========== Call Text Output Tool ==========
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("Calling greet tool (text output)")
	greetResult, err := sendRequest(ctx, "tools/call", map[string]interface{}{
		"name": "greet",
		"arguments": map[string]interface{}{
			"name":     "Test User",
			"language": "en",
		},
	})
	if err != nil {
		log.Printf("Call failed: %v\n", err)
	} else {
		log.Printf("Call successful\n")
		log.Printf("Response: %s\n", prettyJSON(greetResult))
	}
	log.Println()

	// ========== Call Structured Output Tool - Weather ==========
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("Calling get_weather tool (structured output)")
	weatherResult, err := sendRequest(ctx, "tools/call", map[string]interface{}{
		"name": "get_weather",
		"arguments": map[string]interface{}{
			"city": "Beijing",
		},
	})
	if err != nil {
		log.Printf("Call failed: %v\n", err)
	} else {
		log.Printf("Call successful\n")
		log.Printf("Raw response:\n%s\n", prettyJSON(weatherResult))

		// Parse structured data
		var toolResult protocol.CallToolResult
		if err := json.Unmarshal(weatherResult, &toolResult); err == nil {
			if toolResult.StructuredContent != nil {
				log.Println("\nParsed structured data:")
				data := toolResult.StructuredContent.(map[string]interface{})
				log.Printf("  City: %v", data["city"])
				log.Printf("  Temperature: %.1f°C", data["temperature"])
				log.Printf("  Humidity: %v%%", data["humidity"])
				log.Printf("  Condition: %v", data["condition"])
				log.Printf("  Wind Speed: %.1f km/h", data["wind_speed"])
				log.Printf("  Timestamp: %v", data["timestamp"])
			}
		}
	}
	log.Println()

	// ========== Call Nested Structured Output Tool ==========
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("Calling get_user_info tool (nested structured output)")
	userResult, err := sendRequest(ctx, "tools/call", map[string]interface{}{
		"name": "get_user_info",
		"arguments": map[string]interface{}{
			"user_id": "12345",
		},
	})
	if err != nil {
		log.Printf("Call failed: %v\n", err)
	} else {
		log.Printf("Call successful\n")
		log.Printf("Raw response:\n%s\n", prettyJSON(userResult))

		// Parse nested structured data
		var toolResult protocol.CallToolResult
		if err := json.Unmarshal(userResult, &toolResult); err == nil {
			if toolResult.StructuredContent != nil {
				log.Println("\nParsed nested structured data:")
				data := toolResult.StructuredContent.(map[string]interface{})

				log.Printf("  User ID: %v", data["user_id"])
				log.Printf("  Name: %v", data["name"])
				log.Printf("  Age: %v", data["age"])
				log.Printf("  Email: %v", data["email"])

				if address, ok := data["address"].(map[string]interface{}); ok {
					log.Printf("  Address:")
					log.Printf("     City: %v", address["city"])
					log.Printf("     Country: %v", address["country"])
					log.Printf("     Zipcode: %v", address["zipcode"])
				}

				if skills, ok := data["skills"].([]interface{}); ok {
					log.Printf("  Skills: %v", skills)
				}

				if metadata, ok := data["metadata"].(map[string]interface{}); ok {
					log.Printf("  Metadata:")
					log.Printf("     Created At: %v", metadata["created_at"])
					log.Printf("     Last Login: %v", metadata["last_login"])
					log.Printf("     Profile Views: %v", metadata["profile_views"])
					log.Printf("     Verified: %v", metadata["is_verified"])
				}
			}
		}
	}
	log.Println()

	// ========== Batch Calls ==========
	log.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Println("Batch calling get_weather (multiple cities)")
	cities := []string{"Shanghai", "Shenzhen", "Hangzhou", "Guangzhou", "Chengdu"}
	for i, city := range cities {
		result, err := sendRequest(ctx, "tools/call", map[string]interface{}{
			"name": "get_weather",
			"arguments": map[string]interface{}{
				"city": city,
			},
		})
		if err != nil {
			log.Printf("  [%d] %s: Failed - %v\n", i+1, city, err)
			continue
		}

		var toolResult protocol.CallToolResult
		if err := json.Unmarshal(result, &toolResult); err == nil {
			if data, ok := toolResult.StructuredContent.(map[string]interface{}); ok {
				log.Printf("  [%d] %s: %.1f°C, %v%% humidity, %v",
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
	httpReq.Header.Set("MCP-Protocol-Version", "2025-11-25")

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
