package protocol

// Resource 资源定义
type Resource struct {
	URI         string         `json:"uri"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

// ResourceContents 资源内容
type ResourceContents struct {
	URI         string      `json:"uri"`
	Title       string      `json:"title,omitempty"`
	MimeType    string      `json:"mimeType,omitempty"`
	Text        string      `json:"text,omitempty"`
	Blob        string      `json:"blob,omitempty"`
	Annotations *Annotation `json:"annotations,omitempty"`
}

// ListResourcesRequest resources/list 请求和响应
type ListResourcesRequest struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListResourcesParams 列表资源的参数类型
type ListResourcesParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type ListResourcesResult struct {
	Resources []Resource `json:"resources"`
	PaginatedResult
}

// ReadResourceRequest resources/read 请求和响应
type ReadResourceRequest struct {
	URI string `json:"uri"`
}

// ReadResourceParams 读取资源的参数类型
type ReadResourceParams struct {
	URI string `json:"uri"`
}

type ReadResourceResult struct {
	Contents []ResourceContents `json:"contents"`
}

// ListResourceTemplatesRequest resources/templates/list 请求和响应
type ListResourceTemplatesRequest struct {
	Cursor string `json:"cursor,omitempty"`
}

type ResourceTemplate struct {
	URITemplate string         `json:"uriTemplate"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	MimeType    string         `json:"mimeType,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

type ListResourceTemplatesResult struct {
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates"`
	PaginatedResult
}

// ResourcesListChangedNotification 资源变更通知
type ResourcesListChangedNotification struct{}

type ResourceTemplatesListChangedNotification struct{}

type ResourcesUpdatedNotification struct {
	URI   string `json:"uri"`
	Title string `json:"title,omitempty"`
}

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

// NewResourcesUpdatedNotification 创建资源更新通知
func NewResourcesUpdatedNotification(uri string, title ...string) ResourcesUpdatedNotification {
	notification := ResourcesUpdatedNotification{URI: uri}
	if len(title) > 0 && title[0] != "" {
		notification.Title = title[0]
	}
	return notification
}
