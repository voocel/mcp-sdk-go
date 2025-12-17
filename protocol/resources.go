package protocol

type Resource struct {
	URI         string         `json:"uri"`
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`       // MCP 2025-11-25: Human-readable display name
	Description string         `json:"description,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	Icons       []Icon         `json:"icons,omitempty"`       // MCP 2025-11-25: Icons for UI display
	Size        *int64         `json:"size,omitempty"`        // MCP 2025-11-25: Size in bytes
	Annotations *Annotation    `json:"annotations,omitempty"` // MCP 2025-06-18: Resource annotations
	Meta        map[string]any `json:"_meta,omitempty"`
}

type ResourceContents struct {
	URI         string      `json:"uri"`
	Title       string      `json:"title,omitempty"`
	MimeType    string      `json:"mimeType,omitempty"`
	Text        string      `json:"text,omitempty"`
	Blob        string      `json:"blob,omitempty"`
	Annotations *Annotation `json:"annotations,omitempty"`
}

// ListResourcesRequest resources/list request and response
type ListResourcesRequest struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListResourcesParams parameter type for listing resources
type ListResourcesParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type ListResourcesResult struct {
	Resources []Resource `json:"resources"`
	PaginatedResult
}

// ReadResourceRequest resources/read request and response
type ReadResourceRequest struct {
	URI string `json:"uri"`
}

// ReadResourceParams parameter type for reading resources
type ReadResourceParams struct {
	URI string `json:"uri"`
}

type ReadResourceResult struct {
	Contents []ResourceContents `json:"contents"`
}

// ListResourceTemplatesRequest resources/templates/list request and response
type ListResourceTemplatesRequest struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListResourceTemplatesParams is an alias for ListResourceTemplatesRequest
type ListResourceTemplatesParams = ListResourceTemplatesRequest

type ResourceTemplate struct {
	URITemplate string         `json:"uriTemplate"`
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`       // MCP 2025-11-25: Human-readable display name
	Description string         `json:"description,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	Icons       []Icon         `json:"icons,omitempty"`       // MCP 2025-11-25: Icons for UI display
	Meta        map[string]any `json:"_meta,omitempty"`
}

type ListResourceTemplatesResult struct {
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates"`
	PaginatedResult
}

// SubscribeParams resources/subscribe request parameters
type SubscribeParams struct {
	URI string `json:"uri"`
}

// UnsubscribeParams resources/unsubscribe request parameters
type UnsubscribeParams struct {
	URI string `json:"uri"`
}

// ResourcesListChangedNotification resource change notification
type ResourcesListChangedNotification struct{}

type ResourceTemplatesListChangedNotification struct{}

func NewResource(uri, name, description, mimeType string) Resource {
	return Resource{
		URI:         uri,
		Name:        name,
		Description: description,
		MimeType:    mimeType,
	}
}

func NewTextResourceContents(uri, text string) ResourceContents {
	return ResourceContents{
		URI:      uri,
		MimeType: "text/plain",
		Text:     text,
	}
}

func NewBlobResourceContents(uri, blob, mimeType string) ResourceContents {
	return ResourceContents{
		URI:      uri,
		MimeType: mimeType,
		Blob:     blob,
	}
}

func NewReadResourceResult(contents ...ResourceContents) *ReadResourceResult {
	return &ReadResourceResult{
		Contents: contents,
	}
}

func NewResourceTemplate(uriTemplate, name, description, mimeType string) ResourceTemplate {
	return ResourceTemplate{
		URITemplate: uriTemplate,
		Name:        name,
		Description: description,
		MimeType:    mimeType,
	}
}
