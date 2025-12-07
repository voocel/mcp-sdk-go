package protocol

import "encoding/json"

// SamplingMessage sampling message
type SamplingMessage struct {
	Role    Role    `json:"role"`
	Content Content `json:"content"`
}

// ModelHint model hint
type ModelHint struct {
	Name string `json:"name,omitempty"`
}

// ModelPreferences model preference settings
type ModelPreferences struct {
	Hints                []ModelHint `json:"hints,omitempty"`
	CostPriority         *float64    `json:"costPriority,omitempty"`         // 0-1, cost priority
	SpeedPriority        *float64    `json:"speedPriority,omitempty"`        // 0-1, speed priority
	IntelligencePriority *float64    `json:"intelligencePriority,omitempty"` // 0-1, intelligence priority
}

// IncludeContext context inclusion options
type IncludeContext string

const (
	IncludeContextNone       IncludeContext = "none"
	IncludeContextThisServer IncludeContext = "thisServer"
	IncludeContextAllServers IncludeContext = "allServers"
)

// ToolChoiceMode represents the tool selection mode (MCP 2025-11-25)
type ToolChoiceMode string

const (
	// ToolChoiceModeAuto allows the model to decide whether to use tools (default)
	ToolChoiceModeAuto ToolChoiceMode = "auto"
	// ToolChoiceModeRequired forces the model to use at least one tool
	ToolChoiceModeRequired ToolChoiceMode = "required"
	// ToolChoiceModeNone prevents the model from using any tools
	ToolChoiceModeNone ToolChoiceMode = "none"
)

// ToolChoice controls tool selection behavior in sampling requests (MCP 2025-11-25)
type ToolChoice struct {
	Mode ToolChoiceMode `json:"mode,omitempty"`
}

// SamplingTool represents a tool available for use in sampling (MCP 2025-11-25)
type SamplingTool struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	InputSchema JSONSchema `json:"inputSchema"`
}

// CreateMessageRequest create message request (server-initiated LLM sampling)
type CreateMessageRequest struct {
	Meta             map[string]any         `json:"_meta,omitempty"`
	Messages         []SamplingMessage      `json:"messages"`
	ModelPreferences *ModelPreferences      `json:"modelPreferences,omitempty"`
	SystemPrompt     string                 `json:"systemPrompt,omitempty"`
	IncludeContext   IncludeContext         `json:"includeContext,omitempty"`
	Temperature      *float64               `json:"temperature,omitempty"` // 0.0-1.0
	MaxTokens        int                    `json:"maxTokens"`             // Required
	StopSequences    []string               `json:"stopSequences,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	// Tools available for the LLM to use (MCP 2025-11-25)
	Tools []SamplingTool `json:"tools,omitempty"`
	// ToolChoice controls tool selection behavior (MCP 2025-11-25)
	ToolChoice *ToolChoice `json:"toolChoice,omitempty"`
	// Task metadata for task-augmented requests (MCP 2025-11-25)
	Task *TaskMetadata `json:"task,omitempty"`
}

// CreateMessageParams is an alias for CreateMessageRequest for consistency
type CreateMessageParams = CreateMessageRequest

type StopReason string

const (
	StopReasonEndTurn      StopReason = "endTurn"
	StopReasonMaxTokens    StopReason = "maxTokens"
	StopReasonStopSequence StopReason = "stopSequence"
	StopReasonToolUse      StopReason = "toolUse"
)

type CreateMessageResult struct {
	Role       Role       `json:"role"`
	Content    Content    `json:"content"`
	Model      string     `json:"model"`
	StopReason StopReason `json:"stopReason"`
}

// UnmarshalJSON implements custom unmarshaling for SamplingMessage
func (sm *SamplingMessage) UnmarshalJSON(data []byte) error {
	var temp struct {
		Role    Role            `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	sm.Role = temp.Role
	if len(temp.Content) > 0 {
		content, err := UnmarshalContent(temp.Content)
		if err != nil {
			return err
		}
		sm.Content = content
	}
	return nil
}

// UnmarshalJSON implements custom unmarshaling for CreateMessageResult
func (cmr *CreateMessageResult) UnmarshalJSON(data []byte) error {
	var temp struct {
		Role       Role            `json:"role"`
		Content    json.RawMessage `json:"content"`
		Model      string          `json:"model"`
		StopReason StopReason      `json:"stopReason"`
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	cmr.Role = temp.Role
	cmr.Model = temp.Model
	cmr.StopReason = temp.StopReason
	if len(temp.Content) > 0 {
		content, err := UnmarshalContent(temp.Content)
		if err != nil {
			return err
		}
		cmr.Content = content
	}
	return nil
}

// NewCreateMessageResult creates a message result
func NewCreateMessageResult(role Role, content Content, model string, stopReason StopReason) *CreateMessageResult {
	return &CreateMessageResult{
		Role:       role,
		Content:    content,
		Model:      model,
		StopReason: stopReason,
	}
}

// Validate validates the create message request
func (cmr *CreateMessageRequest) Validate() error {
	if len(cmr.Messages) == 0 {
		return NewMCPError(ErrorCodeInvalidParams, "messages cannot be empty", nil)
	}

	if cmr.MaxTokens <= 0 {
		return NewMCPError(ErrorCodeInvalidParams, "maxTokens must be positive", nil)
	}

	if cmr.Temperature != nil && (*cmr.Temperature < 0.0 || *cmr.Temperature > 1.0) {
		return NewMCPError(ErrorCodeInvalidParams, "temperature must be between 0.0 and 1.0", nil)
	}

	if cmr.ModelPreferences != nil {
		if err := cmr.ModelPreferences.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates model preference settings
func (mp *ModelPreferences) Validate() error {
	if mp.CostPriority != nil && (*mp.CostPriority < 0.0 || *mp.CostPriority > 1.0) {
		return NewMCPError(ErrorCodeInvalidParams, "costPriority must be between 0.0 and 1.0", nil)
	}

	if mp.SpeedPriority != nil && (*mp.SpeedPriority < 0.0 || *mp.SpeedPriority > 1.0) {
		return NewMCPError(ErrorCodeInvalidParams, "speedPriority must be between 0.0 and 1.0", nil)
	}

	if mp.IntelligencePriority != nil && (*mp.IntelligencePriority < 0.0 || *mp.IntelligencePriority > 1.0) {
		return NewMCPError(ErrorCodeInvalidParams, "intelligencePriority must be between 0.0 and 1.0", nil)
	}

	return nil
}
