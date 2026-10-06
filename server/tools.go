package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/voocel/mcp-sdk-go/internal/headerbind"
	"github.com/voocel/mcp-sdk-go/protocol"
)

// ToolHandler is the low-level tool handler: raw params in, response sum out.
// Return *protocol.CallToolResult for a final answer or *protocol.InputRequired
// (via protocol.RequireInput) for an MRTR interim result.
type ToolHandler func(ctx context.Context, req *CallRequest) (protocol.ToolResponse, error)

type serverTool struct {
	tool     *protocol.Tool
	handler  ToolHandler
	bindings []headerbind.Binding
	output   *compiledSchema // non-nil iff the tool declares an outputSchema
}

// ToolHeaderBindings exposes a tool's x-mcp-header bindings. It is the seam
// the streamhttp transport uses for header/body validation; applications
// normally have no reason to call it.
func (s *Server) ToolHeaderBindings(name string) []headerbind.Binding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.tools.get(name); ok {
		return st.bindings
	}
	return nil
}

// AddTool registers a tool. It panics on invalid registration (empty name,
// invalid x-mcp-header bindings): configuration errors must fail loudly, not
// surface as silently missing tools.
func (s *Server) AddTool(t *protocol.Tool, h ToolHandler) {
	if t.Name == "" {
		panic("server: AddTool requires a tool name")
	}
	if h == nil {
		panic(fmt.Sprintf("server: AddTool %q requires a handler", t.Name))
	}
	tool := *t
	if tool.InputSchema == nil {
		tool.InputSchema = protocol.JSONSchema{"type": "object"}
	}
	bindings, err := headerbind.Extract(tool.InputSchema)
	if err != nil {
		panic(fmt.Sprintf("server: AddTool %q: invalid %s binding: %v", tool.Name, headerbind.AnnotationKey, err))
	}
	var output *compiledSchema
	if tool.OutputSchema != nil {
		if output, err = compileRawSchema(tool.OutputSchema); err != nil {
			panic(fmt.Sprintf("server: AddTool %q: invalid output schema: %v", tool.Name, err))
		}
	}
	s.mu.Lock()
	s.tools.add(&serverTool{tool: &tool, handler: h, bindings: bindings, output: output})
	s.toolsDeclared = true
	s.mu.Unlock()
	s.hub.publish(topicTools, protocol.NotificationToolsListChanged, nil)
}

// RemoveTools removes tools by name.
func (s *Server) RemoveTools(names ...string) {
	s.mu.Lock()
	changed := s.tools.remove(names...)
	s.mu.Unlock()
	if changed {
		s.hub.publish(topicTools, protocol.NotificationToolsListChanged, nil)
	}
}

func (s *Server) handleListTools(ctx context.Context, req *Request) (protocol.Result, error) {
	var p protocol.ListToolsParams
	if err := unmarshalParams(req.rawParams, &p); err != nil {
		return nil, err
	}
	s.mu.RLock()
	items, next, err := paginateList(s.tools, s.opts.PageSize, p.Cursor)
	s.mu.RUnlock()
	if err != nil {
		return nil, protocol.Errorf(protocol.CodeInvalidParams, "invalid cursor")
	}
	res := &protocol.ListToolsResult{Tools: make([]*protocol.Tool, 0, len(items)), NextCursor: next}
	for _, st := range items {
		res.Tools = append(res.Tools, st.tool)
	}
	return res, nil
}

func (s *Server) handleCallTool(ctx context.Context, req *Request) (protocol.Result, error) {
	var p protocol.CallToolParams
	if err := unmarshalParams(req.rawParams, &p); err != nil {
		return nil, err
	}
	s.mu.RLock()
	st, ok := s.tools.get(p.Name)
	s.mu.RUnlock()
	if !ok {
		return nil, protocol.Errorf(protocol.CodeInvalidParams, "Unknown tool: %s", p.Name)
	}
	res, err := st.handler(ctx, &CallRequest{Request: req, Params: &p})
	if err != nil || st.output == nil {
		return res, err
	}
	// With an outputSchema declared, the spec makes conforming structured
	// results a server MUST; a non-conforming result is a server bug, not
	// something to ship to the client. Execution errors (isError) carry no
	// structured result and are exempt.
	if ctr, ok := res.(*protocol.CallToolResult); ok && !ctr.IsError {
		if verr := validateToolOutput(st.output, ctr.StructuredContent); verr != nil {
			return nil, protocol.Errorf(protocol.CodeInternal,
				"server bug: tool %q output does not conform to its outputSchema: %v", p.Name, verr)
		}
	}
	return res, nil
}

// ValidateToolOutput checks a final tool result against a declared
// outputSchema, sharing the compiled-validator cache tools/call uses. It is
// the seam for extensions that execute tools out of band (e.g. tasks):
// capture the schema at registration and pass it here, so in-flight work is
// validated against the schema it was created under — immune to the tool
// being removed or re-registered meanwhile. A nil schema and execution
// errors (isError) pass; a nil result counts as an empty result and fails
// when a schema is declared.
func ValidateToolOutput(schema protocol.JSONSchema, res *protocol.CallToolResult) error {
	if schema == nil || (res != nil && res.IsError) {
		return nil
	}
	compiled, err := compileRawSchema(schema)
	if err != nil {
		return err
	}
	var sc any
	if res != nil {
		sc = res.StructuredContent
	}
	return validateToolOutput(compiled, sc)
}

func validateToolOutput(compiled *compiledSchema, sc any) error {
	if sc == nil {
		return errors.New("tool declares an outputSchema but returned no structuredContent")
	}
	// Normalize through JSON so structs, maps and json.RawMessage all reach
	// the validator in plain decoded form.
	raw, err := json.Marshal(sc)
	if err != nil {
		return err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	return compiled.Validate(v)
}

// ToolHandlerFor is the type-safe tool handler. The SDK infers and validates
// the input schema from In, deserializes arguments into In, and populates
// structuredContent from Out (mirrored as JSON text per spec recommendation).
//
// Return conventions:
//   - return nil, out, nil            → result built from out
//   - return res, _, nil              → res used as-is (*CallToolResult or RequireInput)
//   - return nil, _, err              → tool execution error (isError: true)
//   - return nil, _, *protocol.Error  → JSON-RPC protocol error
type ToolHandlerFor[In, Out any] func(ctx context.Context, req *CallRequest, input In) (protocol.ToolResponse, Out, error)

// AddTool registers a type-safe tool handler. It is a package-level function
// because Go does not support method-level type parameters.
//
// If the tool's input schema is nil it is inferred from In, which must be a
// struct or map so the schema has the "object" root the spec requires (`any`
// infers an empty object schema). If the output schema is nil and Out is not
// `any`, it is inferred from Out — any root type is allowed (SEP-2106).
func AddTool[In, Out any](s *Server, tool *protocol.Tool, handler ToolHandlerFor[In, Out]) {
	wrappedTool, wrappedHandler, err := wrapToolHandler(tool, handler)
	if err != nil {
		panic(fmt.Sprintf("server: AddTool %q: %v", tool.Name, err))
	}
	s.AddTool(wrappedTool, wrappedHandler)
}

func wrapToolHandler[In, Out any](tool *protocol.Tool, handler ToolHandlerFor[In, Out]) (*protocol.Tool, ToolHandler, error) {
	toolCopy := *tool

	inputSchema, err := setupInputSchema[In](&toolCopy)
	if err != nil {
		return nil, nil, fmt.Errorf("input schema: %w", err)
	}
	// Compiled here, not per call: a schema that cannot compile (e.g. an
	// unresolvable $ref) is a registration error, never the caller's fault.
	inputValidator, err := compileRawSchema(toolCopy.InputSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("input schema: %w", err)
	}
	outputSchema, err := setupOutputSchema[Out](&toolCopy)
	if err != nil {
		return nil, nil, fmt.Errorf("output schema: %w", err)
	}

	// Out = any means "no typed output to mirror": marshaling a nil any would
	// overwrite a handler-built structuredContent with "null". The declared
	// schema still applies — handleCallTool validates whatever the handler set.
	mirrorOut := outputSchema != nil && reflect.TypeFor[Out]() != reflect.TypeFor[any]()
	var outputZero any
	if mirrorOut {
		outputZero = getZeroValue[Out]()
	}

	wrapped := func(ctx context.Context, req *CallRequest) (protocol.ToolResponse, error) {
		args := req.Params.Arguments
		if args == nil {
			args = make(map[string]any)
		}
		input, err := unmarshalAndValidate[In](args, inputSchema, inputValidator)
		if err != nil {
			// Input validation failures are tool execution errors: the model
			// can read them and retry with corrected arguments.
			return protocol.NewToolResultError(
				fmt.Sprintf("invalid arguments for tool %q: %v", toolCopy.Name, err)), nil
		}

		res, output, err := handler(ctx, req, input)
		if err != nil {
			var pe *protocol.Error
			if errors.As(err, &pe) {
				return nil, pe
			}
			return protocol.NewToolResultError(err.Error()), nil
		}

		if ir, ok := res.(*protocol.InputRequired); ok && ir != nil {
			return ir, nil
		}
		if er, ok := res.(*protocol.ExtensionResult); ok && er != nil {
			return er, nil
		}
		result, _ := res.(*protocol.CallToolResult)
		if result == nil {
			result = &protocol.CallToolResult{}
		}
		if mirrorOut {
			// Replace a typed nil with the zero value so marshaling stays sane.
			if outputZero != nil && reflect.ValueOf(&output).Elem().IsZero() {
				output = outputZero.(Out)
			}
			raw, err := json.Marshal(output)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal output: %w", err)
			}
			result.StructuredContent = json.RawMessage(raw)
			if len(result.Content) == 0 {
				result.Content = protocol.ContentList{protocol.NewTextContent(string(raw))}
			}
		}
		return result, nil
	}

	return &toolCopy, wrapped, nil
}

// setupInputSchema resolves the tool's input schema: user-provided (must be an
// object schema) or inferred from In.
func setupInputSchema[In any](tool *protocol.Tool) (*invopopSchema, error) {
	if tool.InputSchema != nil {
		schema, err := parseSchema(tool.InputSchema)
		if err != nil {
			return nil, err
		}
		if schema.Type != "object" {
			return nil, fmt.Errorf("input schema must have type 'object', got %q", schema.Type)
		}
		return schema, nil
	}
	schema, err := inferSchema[In]()
	if err != nil {
		return nil, err
	}
	if schema.Type != "object" {
		return nil, fmt.Errorf("In type %v must infer an object schema, got %q", reflect.TypeFor[In](), schema.Type)
	}
	m, err := schemaToMap(schema)
	if err != nil {
		return nil, err
	}
	tool.InputSchema = m
	return schema, nil
}

// setupOutputSchema resolves the tool's output schema. `any` means "no
// structured output". Any root type is allowed (SEP-2106).
func setupOutputSchema[Out any](tool *protocol.Tool) (*invopopSchema, error) {
	if reflect.TypeFor[Out]() == reflect.TypeFor[any]() {
		if tool.OutputSchema != nil {
			return parseSchema(tool.OutputSchema)
		}
		return nil, nil
	}
	if tool.OutputSchema != nil {
		return parseSchema(tool.OutputSchema)
	}
	schema, err := inferSchema[Out]()
	if err != nil {
		return nil, err
	}
	m, err := schemaToMap(schema)
	if err != nil {
		return nil, err
	}
	tool.OutputSchema = m
	return schema, nil
}

func parseSchema(m protocol.JSONSchema) (*invopopSchema, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}
	var schema invopopSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}
	return &schema, nil
}
