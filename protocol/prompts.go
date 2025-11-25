package protocol

import "encoding/json"

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type Prompt struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"` // MCP 2025-06-18: Human-friendly title
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
	Meta        map[string]any   `json:"_meta,omitempty"` // MCP 2025-06-18: Extended metadata
}

type PromptMessage struct {
	Role    Role    `json:"role"`
	Content Content `json:"content"`
}

func (pm *PromptMessage) UnmarshalJSON(data []byte) error {
	var temp struct {
		Role    Role            `json:"role"`
		Content json.RawMessage `json:"content"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	pm.Role = temp.Role

	content, err := UnmarshalContent(temp.Content)
	if err != nil {
		return err
	}
	pm.Content = content

	return nil
}

// ListPromptsRequest prompts/list request and response
type ListPromptsRequest struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListPromptsParams parameter type for listing prompt templates
type ListPromptsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type ListPromptsResult struct {
	Prompts []Prompt `json:"prompts"`
	PaginatedResult
}

// GetPromptRequest prompts/get request and response
type GetPromptRequest struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// GetPromptParams parameter type for getting prompt templates
type GetPromptParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

type GetPromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
	Meta        map[string]any  `json:"_meta,omitempty"`
}

// PromptsListChangedNotification prompt template change notification
type PromptsListChangedNotification struct{}

func NewPrompt(name, description string, arguments ...PromptArgument) Prompt {
	return Prompt{
		Name:        name,
		Description: description,
		Arguments:   arguments,
	}
}

func NewPromptArgument(name, description string, required bool) PromptArgument {
	return PromptArgument{
		Name:        name,
		Description: description,
		Required:    required,
	}
}

func NewPromptMessage(role Role, content Content) PromptMessage {
	return PromptMessage{
		Role:    role,
		Content: content,
	}
}

func NewGetPromptResult(description string, messages ...PromptMessage) *GetPromptResult {
	return &GetPromptResult{
		Description: description,
		Messages:    messages,
	}
}
