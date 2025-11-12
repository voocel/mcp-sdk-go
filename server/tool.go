package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/invopop/jsonschema"
	"github.com/voocel/mcp-sdk-go/protocol"
)

// ToolHandlerFor 是一个处理 tools/call 请求的类型安全处理函数。
//
// 与 [ToolHandler] 不同，ToolHandlerFor 提供了大量开箱即用的功能，
// 并强制工具符合 MCP 规范：
//   - In 类型为工具提供默认的输入 schema（可在 AddTool 中覆盖）
//   - 输入值会自动从 req.Params.Arguments 反序列化
//   - 输入值会自动根据其 schema 进行验证，无效输入在到达处理函数前就被拒绝
//   - 如果 Out 类型不是 [any]，它会为工具提供默认的输出 schema（同样可覆盖）
//   - Out 值用于填充 result.StructuredContent
//   - 如果 [CallToolResult.Content] 未设置，它会用输出的 JSON 内容填充
//   - 错误结果被视为工具错误而非协议错误，因此会被打包到 CallToolResult.Content 中，
//     并设置 IsError 标志
//
// 因此，大多数用户可以完全忽略 [CallToolRequest] 参数和 [CallToolResult] 返回值。
// 实际上，如果你只关心返回输出值或错误，返回 nil CallToolResult 也是允许的。
// 有效结果会按上述描述自动填充。
//
// 使用 [AddTool] 将 ToolHandlerFor 添加到服务器。
type ToolHandlerFor[In, Out any] func(
	ctx context.Context,
	req *CallToolRequest,
	input In,
) (result *protocol.CallToolResult, output Out, err error)

// AddTool 添加工具和类型安全的工具处理函数到服务器。
//
// 这是一个包级函数而非 Server 的方法，因为 Go 不支持方法级别的类型参数。
// 有关更多信息，请参阅 Go 泛型提案：
// https://go.googlesource.com/proposal/+/refs/heads/master/design/43651-type-parameters.md#no-parameterized-methods
//
// 如果工具的输入 schema 为 nil，它会从 In 类型参数推断设置。类型从 Go 类型推断，
// 属性描述从 'jsonschema' 结构体标签读取。在内部，SDK 使用 github.com/invopop/jsonschema
// 包进行推断和验证。In 类型参数必须是 map 或 struct，以便其推断的 JSON Schema 具有
// 规范要求的 "object" 类型。作为特例，如果 In 类型是 'any'，工具的输入 schema
// 会设置为空对象 schema 值。
//
// 如果工具的输出 schema 为 nil，且 Out 类型不是 'any'，输出 schema 会从 Out 类型
// 参数推断设置，Out 类型也必须是 map 或 struct。如果 Out 类型是 'any'，则省略输出 schema。
//
// 与 [Server.AddTool] 不同，AddTool 会自动完成许多工作，并强制工具符合 MCP 规范。
// 详细的自动行为请参阅 [ToolHandlerFor] 的文档。
//
// 示例：
//
//	type Input struct {
//	    Name string `json:"name" jsonschema:"required,description=用户名称"`
//	}
//	type Output struct {
//	    Greeting string `json:"greeting" jsonschema:"required,description=问候语"`
//	}
//
//	server.AddTool[Input, Output](s, &protocol.Tool{
//	    Name:        "greet",
//	    Description: "向用户问候",
//	}, func(ctx context.Context, req *server.CallToolRequest, input Input) (
//	    *protocol.CallToolResult, Output, error,
//	) {
//	    return nil, Output{Greeting: "你好，" + input.Name}, nil
//	})
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
