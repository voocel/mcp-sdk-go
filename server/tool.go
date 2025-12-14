package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/invopop/jsonschema"
	"github.com/voocel/mcp-sdk-go/protocol"
)

// ToolHandlerFor is a type-safe handler function for tools/call requests.
//
// Unlike [ToolHandler], ToolHandlerFor provides many out-of-the-box features,
// and enforces that tools conform to the MCP specification:
//   - The In type provides a default input schema for the tool (can be overridden in AddTool)
//   - Input values are automatically deserialized from req.Params.Arguments
//   - Input values are automatically validated against their schema, and invalid inputs are rejected before reaching the handler
//   - If the Out type is not [any], it provides a default output schema for the tool (can also be overridden)
//   - The Out value is used to populate result.StructuredContent
//   - If [CallToolResult.Content] is not set, it is populated with the JSON content of the output
//   - Error results are treated as tool errors rather than protocol errors, so they are wrapped in CallToolResult.Content,
//     and the IsError flag is set
//
// Therefore, most users can completely ignore the [CallToolRequest] parameter and [CallToolResult] return value.
// In fact, if you only care about returning an output value or error, returning a nil CallToolResult is allowed.
// Valid results are automatically populated as described above.
//
// Use [AddTool] to add a ToolHandlerFor to a server.
type ToolHandlerFor[In, Out any] func(
	ctx context.Context,
	req *CallToolRequest,
	input In,
) (result *protocol.CallToolResult, output Out, err error)

// AddTool adds a tool and type-safe tool handler to the server.
//
// This is a package-level function rather than a method on Server, because Go does not support method-level type parameters.
// For more information, see the Go generics proposal:
// https://go.googlesource.com/proposal/+/refs/heads/master/design/43651-type-parameters.md#no-parameterized-methods
//
// If the tool's input schema is nil, it is inferred from the In type parameter. Types are inferred from Go types,
// and property descriptions are read from 'jsonschema' struct tags. Internally, the SDK uses the github.com/invopop/jsonschema
// package for inference and validation. The In type parameter must be a map or struct so that its inferred JSON Schema has
// the "object" type required by the specification. As a special case, if the In type is 'any', the tool's input schema
// is set to an empty object schema value.
//
// If the tool's output schema is nil, and the Out type is not 'any', the output schema is inferred from the Out type
// parameter, which must also be a map or struct. If the Out type is 'any', the output schema is omitted.
//
// Unlike [Server.AddTool], AddTool automatically handles many things and enforces that tools conform to the MCP specification.
// For detailed automatic behaviors, see the documentation for [ToolHandlerFor].
//
// Example:
//
//	type Input struct {
//	    Name string `json:"name" jsonschema:"required,description=User name"`
//	}
//	type Output struct {
//	    Greeting string `json:"greeting" jsonschema:"required,description=Greeting message"`
//	}
//
//	server.AddTool[Input, Output](s, &protocol.Tool{
//	    Name:        "greet",
//	    Description: "Greet the user",
//	}, func(ctx context.Context, req *server.CallToolRequest, input Input) (
//	    *protocol.CallToolResult, Output, error,
//	) {
//	    return nil, Output{Greeting: "Hello, " + input.Name}, nil
//	})
func AddTool[In, Out any](s *Server, tool *protocol.Tool, handler ToolHandlerFor[In, Out]) {
	wrappedTool, wrappedHandler, err := wrapToolHandler(tool, handler)
	if err != nil {
		panic(fmt.Sprintf("AddTool %q: %v", tool.Name, err))
	}

	s.AddTool(wrappedTool, wrappedHandler)
}

// wrapToolHandler wraps a type-safe handler into a low-level handler
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

	// Get zero value (for handling typed nil)
	var outputZero interface{}
	if outputSchema != nil {
		outputZero = getZeroValue[Out]()
	}

	// Create wrapped handler
	wrappedHandler := func(ctx context.Context, req *CallToolRequest) (*protocol.CallToolResult, error) {
		inputData := req.Params.Arguments
		if inputData == nil {
			inputData = make(map[string]any)
		}

		input, err := unmarshalAndValidate[In](inputData, inputSchema)
		if err != nil {
			return nil, protocol.NewMCPError(protocol.InvalidParams, "Invalid params", map[string]any{
				"method": protocol.MethodToolsCall,
				"tool":   toolCopy.Name,
			})
		}

		// Call user handler
		result, output, err := handler(ctx, req, input)

		if err != nil {
			// If there's already a result, it's a tool-level error (wrapped in result)
			if result != nil {
				return result, nil
			}
			// Otherwise it's a protocol-level error, return directly
			return nil, err
		}

		// If result is nil, create one
		if result == nil {
			result = &protocol.CallToolResult{}
		}

		// Process output
		if outputSchema != nil {
			// Check for typed nil (use reflection because some types are not comparable)
			if outputZero != nil && reflect.ValueOf(output).IsZero() {
				// Use zero value instead of typed nil
				output = outputZero.(Out)
			}

			outputData, err := json.Marshal(output)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal output: %w", err)
			}

			var outputMap map[string]interface{}
			if err := json.Unmarshal(outputData, &outputMap); err != nil {
				return nil, fmt.Errorf("failed to unmarshal output: %w", err)
			}

			result.StructuredContent = outputMap
		}

		return result, nil
	}

	return &toolCopy, wrappedHandler, nil
}

// setupInputSchema sets up the input schema
func setupInputSchema[In any](tool *protocol.Tool) (*jsonschema.Schema, error) {
	// If user has provided schema, use it directly
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

	// Auto-generate schema
	schema, err := inferSchema[In]()
	if err != nil {
		// Auto-generation failed, use basic object schema
		// Note: This means no parameter validation, but tool can still run
		return &jsonschema.Schema{
			Type: "object",
		}, nil
	}

	// Convert to protocol.JSONSchema (map[string]interface{})
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

// setupOutputSchema sets up the output schema
func setupOutputSchema[Out any](tool *protocol.Tool) (*jsonschema.Schema, error) {
	// If it's 'any' type, don't generate output schema
	if reflect.TypeFor[Out]() == reflect.TypeFor[any]() {
		return nil, nil
	}

	// If user has provided schema, use it directly
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

	// Auto-generate schema
	schema, err := inferSchema[Out]()
	if err != nil {
		// Auto-generation failed, return nil (don't use output schema)
		return nil, nil
	}

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
