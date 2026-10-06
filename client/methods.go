package client

import (
	"context"
	"encoding/json"
	"errors"
	"iter"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// Discover fetches the server's supported versions, capabilities and identity.
func (c *Client) Discover(ctx context.Context) (*protocol.DiscoverResult, error) {
	var res protocol.DiscoverResult
	if err := c.Call(ctx, protocol.MethodDiscover, protocol.DiscoverParams{}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListTools fetches one page of tools; pass nil for the first page and the
// previous result's NextCursor for the next. Tools with invalid x-mcp-header
// bindings are excluded per spec; their bindings are cached for header
// generation on later CallTool requests.
func (c *Client) ListTools(ctx context.Context, cursor *string) (*protocol.ListToolsResult, error) {
	var res protocol.ListToolsResult
	if err := c.Call(ctx, protocol.MethodToolsList, protocol.ListToolsParams{Cursor: cursor}, &res); err != nil {
		return nil, err
	}
	c.registerTools(&res)
	return &res, nil
}

// Tools iterates all tools across pages.
func (c *Client) Tools(ctx context.Context) iter.Seq2[*protocol.Tool, error] {
	return paged(ctx, c.ListTools,
		func(r *protocol.ListToolsResult) ([]*protocol.Tool, *string) { return r.Tools, r.NextCursor })
}

func (c *Client) ListPrompts(ctx context.Context, cursor *string) (*protocol.ListPromptsResult, error) {
	var res protocol.ListPromptsResult
	if err := c.Call(ctx, protocol.MethodPromptsList, protocol.ListPromptsParams{Cursor: cursor}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Prompts iterates all prompts across pages.
func (c *Client) Prompts(ctx context.Context) iter.Seq2[*protocol.Prompt, error] {
	return paged(ctx, c.ListPrompts,
		func(r *protocol.ListPromptsResult) ([]*protocol.Prompt, *string) { return r.Prompts, r.NextCursor })
}

func (c *Client) ListResources(ctx context.Context, cursor *string) (*protocol.ListResourcesResult, error) {
	var res protocol.ListResourcesResult
	if err := c.Call(ctx, protocol.MethodResourcesList, protocol.ListResourcesParams{Cursor: cursor}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Resources iterates all resources across pages.
func (c *Client) Resources(ctx context.Context) iter.Seq2[*protocol.Resource, error] {
	return paged(ctx, c.ListResources,
		func(r *protocol.ListResourcesResult) ([]*protocol.Resource, *string) {
			return r.Resources, r.NextCursor
		})
}

func (c *Client) ListResourceTemplates(ctx context.Context, cursor *string) (*protocol.ListResourceTemplatesResult, error) {
	var res protocol.ListResourceTemplatesResult
	if err := c.Call(ctx, protocol.MethodResourcesTemplatesList, protocol.ListResourceTemplatesParams{Cursor: cursor}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) Complete(ctx context.Context, p *protocol.CompleteParams) (*protocol.CompleteResult, error) {
	var res protocol.CompleteResult
	if err := c.Call(ctx, protocol.MethodCompletionComplete, p, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// CallTool invokes a tool, transparently running the MRTR fulfillment loop
// (see Options.Elicitor / NoAutoInput). If the server rejects the call with a
// HeaderMismatch, the cached x-mcp-header bindings are missing or stale (a
// tool never listed, or a changed schema): they are refreshed via tools/list
// and the call is retried once, as the spec advises.
func (c *Client) CallTool(ctx context.Context, p *protocol.CallToolParams) (*protocol.CallToolResult, error) {
	res, err := c.callTool(ctx, p)
	var perr *protocol.Error
	if errors.As(err, &perr) && perr.Code == protocol.CodeHeaderMismatch {
		for _, lerr := range c.Tools(ctx) {
			if lerr != nil {
				return nil, err // the refresh failed; the original error is the one to report
			}
		}
		return c.callTool(ctx, p)
	}
	return res, err
}

func (c *Client) callTool(ctx context.Context, p *protocol.CallToolParams) (*protocol.CallToolResult, error) {
	var res protocol.CallToolResult
	if err := c.mrtrCall(ctx, protocol.MethodToolsCall, p, mrtrFields{
		responses: func(r protocol.InputResponses, s string) { p.InputResponses = r; p.RequestState = s },
	}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetPrompt resolves a prompt, transparently running the MRTR loop.
func (c *Client) GetPrompt(ctx context.Context, p *protocol.GetPromptParams) (*protocol.GetPromptResult, error) {
	var res protocol.GetPromptResult
	if err := c.mrtrCall(ctx, protocol.MethodPromptsGet, p, mrtrFields{
		responses: func(r protocol.InputResponses, s string) { p.InputResponses = r; p.RequestState = s },
	}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ReadResource reads a resource, transparently running the MRTR loop.
func (c *Client) ReadResource(ctx context.Context, p *protocol.ReadResourceParams) (*protocol.ReadResourceResult, error) {
	var res protocol.ReadResourceResult
	if err := c.mrtrCall(ctx, protocol.MethodResourcesRead, p, mrtrFields{
		responses: func(r protocol.InputResponses, s string) { p.InputResponses = r; p.RequestState = s },
	}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// paged adapts a page-fetching method into an iterator. Only an absent cursor
// ends the walk (see protocol.ListToolsResult.NextCursor).
func paged[R any, T any](ctx context.Context, fetch func(context.Context, *string) (*R, error),
	split func(*R) ([]T, *string)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var cursor *string
		for {
			res, err := fetch(ctx, cursor)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			items, next := split(res)
			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}
			if next == nil {
				return
			}
			cursor = next
		}
	}
}

// decodeInto re-decodes a raw result body into v.
func decodeInto(raw json.RawMessage, v any) error {
	return json.Unmarshal(raw, v)
}
