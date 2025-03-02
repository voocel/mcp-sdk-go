package protocol

type PromptMessage struct {
	Role    Role        `json:"role"`
	Content interface{} `json:"content"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptList struct {
	Prompts []Prompt `json:"prompts"`
}

type GetPromptParams struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args,omitempty"`
}

type GetPromptResult struct {
	Name     string          `json:"name"`
	Messages []PromptMessage `json:"messages"`
}

func NewPromptMessage(role Role, content interface{}) PromptMessage {
	return PromptMessage{
		Role:    role,
		Content: content,
	}
}
func NewGetPromptResult(name string, messages []PromptMessage) *GetPromptResult {
	return &GetPromptResult{
		Name:     name,
		Messages: messages,
	}
}
