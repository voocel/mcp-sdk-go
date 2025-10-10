package protocol

import "encoding/json"

// PromptArgument 提示模板参数
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Prompt 提示模板定义
type Prompt struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"` // MCP 2025-06-18: 人类友好的标题
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
	Meta        map[string]any   `json:"_meta,omitempty"` // MCP 2025-06-18: 扩展元数据
}

// PromptMessage 提示消息
type PromptMessage struct {
	Role    Role    `json:"role"`
	Content Content `json:"content"`
}

// UnmarshalJSON 实现自定义JSON反序列化
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

// ListPromptsRequest prompts/list 请求和响应
type ListPromptsRequest struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListPromptsParams 列表提示模板的参数类型
type ListPromptsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type ListPromptsResult struct {
	Prompts []Prompt `json:"prompts"`
	PaginatedResult
}

// GetPromptRequest prompts/get 请求和响应
type GetPromptRequest struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// GetPromptParams 获取提示模板的参数类型
type GetPromptParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

type GetPromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
	Meta        map[string]any  `json:"_meta,omitempty"`
}

// PromptsListChangedNotification 提示模板变更通知
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
