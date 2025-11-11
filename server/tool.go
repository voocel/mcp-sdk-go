package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/invopop/jsonschema"
	"github.com/voocel/mcp-sdk-go/protocol"
)

// ToolHandlerFor In:  输入参数类型（将自动 unmarshal 和验证）
// Out: 输出类型（将自动 marshal 到 result）
type ToolHandlerFor[In, Out any] func(
	ctx context.Context,
	req *CallToolRequest,
	input In,
) (result *protocol.CallToolResult, output Out, err error)

// AddTool 如果 tool.InputSchema 为 nil，将从 In 类型自动生成
// 如果 tool.OutputSchema 为 nil 且 Out != any，将从 Out 类型自动生成
func AddTool[In, Out any](s *Server, tool *protocol.Tool, handler ToolHandlerFor[In, Out]) {
	wrappedTool, wrappedHandler, err := wrapToolHandler(tool, handler)
	if err != nil {
		panic(fmt.Sprintf("AddTool %q: %v", tool.Name, err))
	}

	s.AddTool(wrappedTool, wrappedHandler)
}

// wrapToolHandler 包装类型安全的 handler 为低层 handler
func wrapToolHandler[In, Out any](tool *protocol.Tool, handler ToolHandlerFor[In, Out]) (*protocol.Tool, ToolHandler, error) {
	toolCopy := *tool

	inputSchema, err := setupInputSchema[In](&toolCopy)
	if err != nil {
		return nil, nil, fmt.Errorf("input schema: %w", err)
	}

	outputSchema, err := setupOutputSchema[Out](&toolCopy)
	if err != nil {
		return nil, nil, fmt.Errorf("output schema: %w", err)
	}

	// 获取零值（用于处理 typed nil）
	var outputZero interface{}
	if outputSchema != nil {
		outputZero = getZeroValue[Out]()
	}

	// 创建包装的 handler
	wrappedHandler := func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
		inputData := req.Params.Arguments
		if inputData == nil {
			inputData = make(map[string]any)
		}

		input, err := unmarshalAndValidate[In](inputData, inputSchema)
		if err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		// 调用用户 handler
		result, output, err := handler(ctx, req, input)

		if err != nil {
			// 如果已经有 result，说明是工具级错误（包装在 result 中）
			if result != nil {
				return result, nil
			}
			// 否则是协议级错误，直接返回
			return nil, err
		}

		// 如果 result 为 nil，创建一个
		if result == nil {
			result = &protocol.CallToolResult{}
		}

		// 处理输出
		if outputSchema != nil {
			// 检查 typed nil
			var zeroOut Out
			if outputZero != nil && any(output) == any(zeroOut) {
				// 使用零值替代 typed nil
				output = outputZero.(Out)
			}

			outputData, err := json.Marshal(output)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal output: %w", err)
			}

			// 添加到 result（作为 Embedded Resource）
			result.Content = append(result.Content, protocol.EmbeddedResourceContent{
				Type: protocol.ContentTypeResource,
				Resource: protocol.ResourceContents{
					URI:      fmt.Sprintf("output://%s", tool.Name),
					MimeType: "application/json",
					Text:     string(outputData),
				},
			})
		}

		return result, nil
	}

	return &toolCopy, wrappedHandler, nil
}

// setupInputSchema 设置输入 schema
func setupInputSchema[In any](tool *protocol.Tool) (*jsonschema.Schema, error) {
	// 如果用户已提供 schema, 直接使用
	if tool.InputSchema != nil {
		schemaBytes, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal input schema: %w", err)
		}

		var schema jsonschema.Schema
		if err := json.Unmarshal(schemaBytes, &schema); err != nil {
			return nil, fmt.Errorf("invalid input schema: %w", err)
		}

		if schema.Type != "object" {
			return nil, fmt.Errorf("input schema must have type 'object', got %q", schema.Type)
		}

		return &schema, nil
	}

	// 自动生成 schema
	schema, err := inferSchema[In]()
	if err != nil {
		// 自动生成失败，使用基本 object schema
		// 注意：这意味着不会有参数验证，但工具仍能运行
		return &jsonschema.Schema{
			Type: "object",
		}, nil
	}

	// 转换为 protocol.JSONSchema（map[string]interface{}）
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	var schemaMap map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	tool.InputSchema = schemaMap
	return schema, nil
}

// setupOutputSchema 设置输出 schema
func setupOutputSchema[Out any](tool *protocol.Tool) (*jsonschema.Schema, error) {
	// 如果是 any 类型，不生成 output schema
	if reflect.TypeFor[Out]() == reflect.TypeFor[any]() {
		return nil, nil
	}

	// 如果用户已提供 schema, 直接使用
	if tool.OutputSchema != nil {
		schemaBytes, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal output schema: %w", err)
		}

		var schema jsonschema.Schema
		if err := json.Unmarshal(schemaBytes, &schema); err != nil {
			return nil, fmt.Errorf("invalid output schema: %w", err)
		}

		if schema.Type != "object" {
			return nil, fmt.Errorf("output schema must have type 'object', got %q", schema.Type)
		}

		return &schema, nil
	}

	// 尝试自动生成 schema
	schema, err := inferSchema[Out]()
	if err != nil {
		// 自动生成失败，返回 nil（不使用 output schema）
		return nil, nil
	}

	// 转换为 protocol.JSONSchema（map[string]interface{}）
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	var schemaMap map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	tool.OutputSchema = schemaMap
	return schema, nil
}
