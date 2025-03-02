package protocol

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type ResourceList struct {
	Resources []Resource `json:"resources"`
}

type ResourceContent struct {
	URI     string        `json:"uri"`
	Content []interface{} `json:"content"`
}

type ReadResourceParams struct {
	URI string `json:"uri"`
}

func NewResourceResultText(uri string, text string) *ResourceContent {
	return &ResourceContent{
		URI: uri,
		Content: []interface{}{
			TextContent{
				Type: ContentTypeText,
				Text: text,
			},
		},
	}
}

func NewResourceResultJSON(uri string, data interface{}) (*ResourceContent, error) {
	jsonContent, err := NewJSONContent(data)
	if err != nil {
		return nil, err
	}

	return &ResourceContent{
		URI:     uri,
		Content: []interface{}{jsonContent},
	}, nil
}
