package protocol

import "encoding/json"

// JSONSchema is a JSON Schema document. 2026-07-28 permits any JSON Schema
// 2020-12 keywords; the default dialect when $schema is absent is 2020-12.
type JSONSchema map[string]any

type Icon struct {
	Src      string   `json:"src"`
	MimeType string   `json:"mimeType,omitempty"`
	Sizes    []string `json:"sizes,omitempty"`
}

type Tool struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// InputSchema must be an object schema at the root.
	InputSchema JSONSchema `json:"inputSchema"`
	// OutputSchema has no root type constraint (SEP-2106).
	OutputSchema JSONSchema                 `json:"outputSchema,omitempty"`
	Annotations  *ToolAnnotations           `json:"annotations,omitempty"`
	Icons        []Icon                     `json:"icons,omitempty"`
	Meta         map[string]json.RawMessage `json:"_meta,omitempty"`
}

// ToolAnnotations are untrusted hints. Pointers distinguish "false" from
// "unset".
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

type ListToolsParams struct {
	Meta   RequestMeta `json:"_meta"`
	Cursor string      `json:"cursor,omitempty"`
}

type ListToolsResult struct {
	WithMeta
	CacheControl
	Tools      []*Tool `json:"tools"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

func (*ListToolsResult) ResultType() string { return ResultTypeComplete }

type CallToolParams struct {
	Meta      RequestMeta    `json:"_meta"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	// InputResponses and RequestState are set only on an MRTR retry.
	InputResponses InputResponses `json:"inputResponses,omitempty"`
	RequestState   string         `json:"requestState,omitempty"`
}

type CallToolResult struct {
	WithMeta
	Content ContentList `json:"content"`
	// StructuredContent may be any JSON value (SEP-2106).
	StructuredContent any  `json:"structuredContent,omitempty"`
	IsError           bool `json:"isError,omitempty"`
}

func (*CallToolResult) ResultType() string { return ResultTypeComplete }
func (*CallToolResult) toolResponse()      {}

func NewToolResultText(text string) *CallToolResult {
	return &CallToolResult{Content: ContentList{NewTextContent(text)}}
}

// NewToolResultError reports a tool execution error (isError, not a protocol
// error).
func NewToolResultError(text string) *CallToolResult {
	return &CallToolResult{Content: ContentList{NewTextContent(text)}, IsError: true}
}

// NewToolResultStructured returns structured content mirrored as serialized
// JSON in a text block, as the spec recommends for backwards compatibility.
func NewToolResultStructured(v any) (*CallToolResult, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &CallToolResult{
		Content:           ContentList{NewTextContent(string(raw))},
		StructuredContent: v,
	}, nil
}
