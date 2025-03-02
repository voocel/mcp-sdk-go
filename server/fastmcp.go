package server

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/transport/grpc"
	"github.com/voocel/mcp-sdk-go/transport/sse"
	"github.com/voocel/mcp-sdk-go/transport/stdio"
	"github.com/voocel/mcp-sdk-go/transport/websocket"
)

type FastMCP struct {
	server *MCPServer
}

func NewFastMCP(name, version string) *FastMCP {
	return &FastMCP{
		server: NewServer(name, version),
	}
}

func (f *FastMCP) AddTool(name string, handler interface{}, description string) {
	params, wrapper := createToolHandler(handler)
	f.server.AddTool(name, description, wrapper, params...)
}

func createToolHandler(handler interface{}) ([]protocol.ToolParameter, ToolHandler) {
	handlerType := reflect.TypeOf(handler)
	handlerValue := reflect.ValueOf(handler)

	if handlerType.Kind() != reflect.Func {
		panic("handler must be a function")
	}

	params := make([]protocol.ToolParameter, 0, handlerType.NumIn())
	for i := 0; i < handlerType.NumIn(); i++ {
		paramType := handlerType.In(i)
		paramName := fmt.Sprintf("arg%d", i)

		var param protocol.ToolParameter
		switch paramType.Kind() {
		case reflect.String:
			param = protocol.StringParameter(paramName, "", true)
		case reflect.Int, reflect.Int64, reflect.Float64:
			param = protocol.NumberParameter(paramName, "", true)
		case reflect.Bool:
			param = protocol.BooleanParameter(paramName, "", true)
		default:
			param = protocol.ObjectParameter(paramName, "", true, protocol.JSONSchema{}, []string{})
		}

		params = append(params, param)
	}

	wrapper := func(ctx context.Context, args map[string]interface{}) (*protocol.CallToolResult, error) {
		callArgs := make([]reflect.Value, handlerType.NumIn())
		for i := 0; i < handlerType.NumIn(); i++ {
			paramName := fmt.Sprintf("arg%d", i)
			paramType := handlerType.In(i)

			argValue, ok := args[paramName]
			if !ok {
				return nil, fmt.Errorf("missing parameter: %s", paramName)
			}

			var value reflect.Value
			switch paramType.Kind() {
			case reflect.String:
				value = reflect.ValueOf(fmt.Sprintf("%v", argValue))
			case reflect.Int:
				floatVal, ok := argValue.(float64)
				if !ok {
					return nil, fmt.Errorf("parameter %s must be a number", paramName)
				}
				value = reflect.ValueOf(int(floatVal))
			case reflect.Int64:
				floatVal, ok := argValue.(float64)
				if !ok {
					return nil, fmt.Errorf("parameter %s must be a number", paramName)
				}
				value = reflect.ValueOf(int64(floatVal))
			case reflect.Float64:
				floatVal, ok := argValue.(float64)
				if !ok {
					return nil, fmt.Errorf("parameter %s must be a number", paramName)
				}
				value = reflect.ValueOf(floatVal)
			case reflect.Bool:
				boolVal, ok := argValue.(bool)
				if !ok {
					return nil, fmt.Errorf("parameter %s must be a boolean", paramName)
				}
				value = reflect.ValueOf(boolVal)
			default:
				value = reflect.ValueOf(argValue)
			}

			callArgs[i] = value
		}

		results := handlerValue.Call(callArgs)
		if len(results) == 0 {
			return protocol.NewToolResultText(""), nil
		}

		var err error
		if len(results) > 1 && !results[len(results)-1].IsNil() && results[len(results)-1].Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			err = results[len(results)-1].Interface().(error)
			results = results[:len(results)-1]
		}

		if err != nil {
			return nil, err
		}

		if len(results) > 0 {
			return protocol.NewToolResultText(fmt.Sprintf("%v", results[0].Interface())), nil
		}

		return protocol.NewToolResultText(""), nil
	}

	return params, wrapper
}

type ToolBuilder struct {
	fastmcp     *FastMCP
	name        string
	description string
	parameters  []protocol.ToolParameter
}

func (f *FastMCP) Tool(name, description string) *ToolBuilder {
	return &ToolBuilder{
		fastmcp:     f,
		name:        name,
		description: description,
		parameters:  []protocol.ToolParameter{},
	}
}

func (tb *ToolBuilder) WithStringParam(name, description string, required bool) *ToolBuilder {
	tb.parameters = append(tb.parameters, protocol.StringParameter(name, description, required))
	return tb
}

func (tb *ToolBuilder) WithNumberParam(name, description string, required bool) *ToolBuilder {
	tb.parameters = append(tb.parameters, protocol.NumberParameter(name, description, required))
	return tb
}

func (tb *ToolBuilder) WithBooleanParam(name, description string, required bool) *ToolBuilder {
	tb.parameters = append(tb.parameters, protocol.BooleanParameter(name, description, required))
	return tb
}

func (tb *ToolBuilder) WithObjectParam(name, description string, required bool, properties protocol.JSONSchema, requiredProps []string) *ToolBuilder {
	tb.parameters = append(tb.parameters, protocol.ObjectParameter(name, description, required, properties, requiredProps))
	return tb
}

func (tb *ToolBuilder) Handle(handler ToolHandler) *FastMCP {
	tb.fastmcp.server.AddTool(tb.name, tb.description, handler, tb.parameters...)
	return tb.fastmcp
}

type PromptBuilder struct {
	fastmcp     *FastMCP
	name        string
	description string
	arguments   []protocol.PromptArgument
}

func (f *FastMCP) Prompt(name, description string) *PromptBuilder {
	return &PromptBuilder{
		fastmcp:     f,
		name:        name,
		description: description,
		arguments:   []protocol.PromptArgument{},
	}
}

func (pb *PromptBuilder) WithArgument(name, description string, required bool) *PromptBuilder {
	pb.arguments = append(pb.arguments, protocol.PromptArgument{
		Name:        name,
		Description: description,
		Required:    required,
	})
	return pb
}

func (pb *PromptBuilder) Handle(handler PromptHandler) *FastMCP {
	pb.fastmcp.server.AddPrompt(pb.name, pb.description, handler, pb.arguments...)
	return pb.fastmcp
}

func (f *FastMCP) Resource(uri, name string, handler func() []string) *FastMCP {
	resourceHandler := func(ctx context.Context) (*protocol.ResourceContent, error) {
		content := strings.Join(handler(), "\n")
		return protocol.NewResourceResultText(uri, content), nil
	}

	f.server.AddResource(uri, name, resourceHandler)
	return f
}

func (f *FastMCP) ServeStdio(ctx context.Context) error {
	handler := NewHandler(f.server)
	stdioServer := stdio.NewServer(handler)
	return stdioServer.Serve(ctx)
}

func (f *FastMCP) ServeSSE(ctx context.Context, addr string) error {
	handler := NewHandler(f.server)
	sseServer := sse.NewServer(addr, handler)
	return sseServer.Serve(ctx)
}

func (f *FastMCP) ServeWebSocket(ctx context.Context, addr string) error {
	handler := NewHandler(f.server)
	wsServer := websocket.NewServer(addr, handler)
	return wsServer.Serve(ctx)
}

func (f *FastMCP) ServeGRPC(ctx context.Context, addr string) error {
	handler := NewHandler(f.server)
	grpcServer := grpc.NewServer(addr, handler)
	return grpcServer.Serve(ctx)
}
