package client

import (
	"fmt"

	"github.com/voocel/mcp-sdk-go/internal/headerbind"
	"github.com/voocel/mcp-sdk-go/protocol"
)

// Transport metadata header names (Streamable HTTP; stdio ignores them).
const (
	headerProtocolVersion = "MCP-Protocol-Version"
	headerMethod          = "Mcp-Method"
	headerName            = "Mcp-Name"
)

// headers assembles the standard request headers plus any Mcp-Param-* headers
// derived from cached x-mcp-header bindings.
func (c *Client) headers(method string, pm map[string]any) (map[string]string, error) {
	h := map[string]string{
		headerProtocolVersion: protocol.Version,
		headerMethod:          method,
	}
	switch method {
	case protocol.MethodToolsCall:
		name, _ := pm["name"].(string)
		h[headerName] = headerbind.Encode(name)
		args, _ := pm["arguments"].(map[string]any)
		if err := c.paramHeaders(h, name, args); err != nil {
			return nil, err
		}
	case protocol.MethodPromptsGet:
		name, _ := pm["name"].(string)
		h[headerName] = headerbind.Encode(name)
	case protocol.MethodResourcesRead:
		uri, _ := pm["uri"].(string)
		h[headerName] = headerbind.Encode(uri)
	default:
		// Extension methods with a registered routing param (Options.RouteNames)
		// send it as Mcp-Name, as e.g. the tasks draft requires for taskId.
		if key, ok := c.opts.RouteNames[method]; ok {
			if v, ok := pm[key].(string); ok && v != "" {
				h[headerName] = headerbind.Encode(v)
			}
		}
	}
	return h, nil
}

func (c *Client) paramHeaders(h map[string]string, toolName string, args map[string]any) error {
	c.mu.RLock()
	bindings := c.bindings[toolName]
	c.mu.RUnlock()
	for _, b := range bindings {
		v, present := headerbind.Lookup(args, b.Path)
		if !present {
			continue // null/absent value: the header must be omitted
		}
		s, err := headerbind.Format(v)
		if err != nil {
			return fmt.Errorf("client: tool %q argument %v: %w", toolName, b.Path, err)
		}
		h[b.HeaderName()] = headerbind.Encode(s)
	}
	return nil
}

// registerTools validates x-mcp-header bindings on listed tools. Invalid
// tools are excluded from the result with a logged warning, as the spec
// requires of Streamable HTTP clients.
func (c *Client) registerTools(res *protocol.ListToolsResult) {
	valid := res.Tools[:0]
	for _, t := range res.Tools {
		bindings, err := headerbind.Extract(t.InputSchema)
		if err != nil {
			c.opts.Logger.Warn("mcp: excluding tool with invalid x-mcp-header binding",
				"tool", t.Name, "error", err)
			continue
		}
		valid = append(valid, t)
		c.mu.Lock()
		if len(bindings) > 0 {
			c.bindings[t.Name] = bindings
		} else {
			delete(c.bindings, t.Name)
		}
		c.mu.Unlock()
	}
	res.Tools = valid
}
