package protocol

import "encoding/json"

type Resource struct {
	URI         string                     `json:"uri"`
	Name        string                     `json:"name"`
	Title       string                     `json:"title,omitempty"`
	Description string                     `json:"description,omitempty"`
	MimeType    string                     `json:"mimeType,omitempty"`
	Size        int64                      `json:"size,omitempty"`
	Annotations *Annotations               `json:"annotations,omitempty"`
	Icons       []Icon                     `json:"icons,omitempty"`
	Meta        map[string]json.RawMessage `json:"_meta,omitempty"`
}

type ResourceTemplate struct {
	URITemplate string                     `json:"uriTemplate"`
	Name        string                     `json:"name"`
	Title       string                     `json:"title,omitempty"`
	Description string                     `json:"description,omitempty"`
	MimeType    string                     `json:"mimeType,omitempty"`
	Annotations *Annotations               `json:"annotations,omitempty"`
	Icons       []Icon                     `json:"icons,omitempty"`
	Meta        map[string]json.RawMessage `json:"_meta,omitempty"`
}

// ResourceContents is one content item of a resources/read result: text or
// base64 blob.
type ResourceContents struct {
	URI      string                     `json:"uri"`
	MimeType string                     `json:"mimeType,omitempty"`
	Text     string                     `json:"text,omitempty"`
	Blob     string                     `json:"blob,omitempty"`
	Meta     map[string]json.RawMessage `json:"_meta,omitempty"`
}

func NewTextResourceContents(uri, text string) ResourceContents {
	return ResourceContents{URI: uri, Text: text}
}

func NewBlobResourceContents(uri, mimeType, blob string) ResourceContents {
	return ResourceContents{URI: uri, MimeType: mimeType, Blob: blob}
}

type ListResourcesParams struct {
	Meta   RequestMeta `json:"_meta"`
	Cursor string      `json:"cursor,omitempty"`
}

type ListResourcesResult struct {
	WithMeta
	CacheControl
	Resources  []*Resource `json:"resources"`
	NextCursor *string     `json:"nextCursor,omitempty"`
}

func (*ListResourcesResult) ResultType() string { return ResultTypeComplete }

type ListResourceTemplatesParams struct {
	Meta   RequestMeta `json:"_meta"`
	Cursor string      `json:"cursor,omitempty"`
}

type ListResourceTemplatesResult struct {
	WithMeta
	CacheControl
	ResourceTemplates []*ResourceTemplate `json:"resourceTemplates"`
	NextCursor        *string             `json:"nextCursor,omitempty"`
}

func (*ListResourceTemplatesResult) ResultType() string { return ResultTypeComplete }

type ReadResourceParams struct {
	Meta RequestMeta `json:"_meta"`
	URI  string      `json:"uri"`
	// InputResponses and RequestState are set only on an MRTR retry.
	InputResponses InputResponses `json:"inputResponses,omitempty"`
	RequestState   string         `json:"requestState,omitempty"`
}

type ReadResourceResult struct {
	WithMeta
	CacheControl
	Contents []ResourceContents `json:"contents"`
}

func (*ReadResourceResult) ResultType() string { return ResultTypeComplete }
func (*ReadResourceResult) resourceResponse()  {}

func NewReadResourceResult(contents ...ResourceContents) *ReadResourceResult {
	return &ReadResourceResult{Contents: contents}
}

// ResourceUpdatedParams is the payload of notifications/resources/updated,
// delivered on subscriptions/listen streams for subscribed URIs.
type ResourceUpdatedParams struct {
	Meta NotificationMeta `json:"_meta,omitzero"`
	URI  string           `json:"uri"`
}
