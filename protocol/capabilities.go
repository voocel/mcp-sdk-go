package protocol

import "encoding/json"

// ClientCapabilities travels in every request's _meta. The zero value is a
// valid, empty declaration. Roots and sampling are deprecated in 2026-07-28
// and intentionally not modeled; unknown keys are ignored on decode.
type ClientCapabilities struct {
	Elicitation  *ElicitationCapability     `json:"elicitation,omitempty"`
	Extensions   map[string]json.RawMessage `json:"extensions,omitempty"`
	Experimental map[string]json.RawMessage `json:"experimental,omitempty"`
}

// ElicitationCapability declares supported elicitation modes. An empty object
// is equivalent to declaring form mode only.
type ElicitationCapability struct {
	Form *struct{} `json:"form,omitempty"`
	URL  *struct{} `json:"url,omitempty"`
}

// SupportsForm reports whether form-mode elicitation is declared. Per spec an
// empty elicitation object means form-only support.
func (c *ElicitationCapability) SupportsForm() bool {
	return c != nil && (c.Form != nil || c.URL == nil)
}

func (c *ElicitationCapability) SupportsURL() bool {
	return c != nil && c.URL != nil
}

// ServerCapabilities is advertised via server/discover. Servers derive it from
// what is actually registered; it is never hand-negotiated.
type ServerCapabilities struct {
	Completions  *struct{}                  `json:"completions,omitempty"`
	Prompts      *PromptsCapability         `json:"prompts,omitempty"`
	Resources    *ResourcesCapability       `json:"resources,omitempty"`
	Tools        *ToolsCapability           `json:"tools,omitempty"`
	Extensions   map[string]json.RawMessage `json:"extensions,omitempty"`
	Experimental map[string]json.RawMessage `json:"experimental,omitempty"`
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// DiscoverParams carries only _meta. Servers must implement server/discover;
// clients may call it before any other request.
type DiscoverParams struct {
	Meta RequestMeta `json:"_meta"`
}

// DiscoverResult advertises supported versions, capabilities and identity
// (serverInfo travels in _meta). It is cacheable.
type DiscoverResult struct {
	WithMeta
	CacheControl
	SupportedVersions []string           `json:"supportedVersions"`
	Capabilities      ServerCapabilities `json:"capabilities"`
	Instructions      string             `json:"instructions,omitempty"`
}

func (*DiscoverResult) ResultType() string { return ResultTypeComplete }
