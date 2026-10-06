package protocol

import "encoding/json"

type Prompt struct {
	Name        string                     `json:"name"`
	Title       string                     `json:"title,omitempty"`
	Description string                     `json:"description,omitempty"`
	Arguments   []PromptArgument           `json:"arguments,omitempty"`
	Icons       []Icon                     `json:"icons,omitempty"`
	Meta        map[string]json.RawMessage `json:"_meta,omitempty"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type PromptMessage struct {
	Role    Role    `json:"role"`
	Content Content `json:"content"`
}

func NewPromptMessage(role Role, content Content) PromptMessage {
	return PromptMessage{Role: role, Content: content}
}

func (m *PromptMessage) UnmarshalJSON(b []byte) error {
	var raw struct {
		Role    Role            `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	content, err := UnmarshalContent(raw.Content)
	if err != nil {
		return err
	}
	m.Role = raw.Role
	m.Content = content
	return nil
}

type ListPromptsParams struct {
	Meta   RequestMeta `json:"_meta"`
	Cursor *string     `json:"cursor,omitempty"`
}

type ListPromptsResult struct {
	WithMeta
	CacheControl
	Prompts    []*Prompt `json:"prompts"`
	NextCursor *string   `json:"nextCursor,omitempty"`
}

func (*ListPromptsResult) ResultType() string { return ResultTypeComplete }

type GetPromptParams struct {
	Meta      RequestMeta       `json:"_meta"`
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
	// InputResponses and RequestState are set only on an MRTR retry.
	InputResponses InputResponses `json:"inputResponses,omitempty"`
	RequestState   string         `json:"requestState,omitempty"`
}

type GetPromptResult struct {
	WithMeta
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

func (*GetPromptResult) ResultType() string { return ResultTypeComplete }
func (*GetPromptResult) promptResponse()    {}

func NewGetPromptResult(description string, messages ...PromptMessage) *GetPromptResult {
	return &GetPromptResult{Description: description, Messages: messages}
}
