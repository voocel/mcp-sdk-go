package protocol

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

func NewSamplingMessage(role Role, content Content) SamplingMessage {
	return SamplingMessage{
		Role:    role,
		Content: content,
	}
}

func NewModelHint(name string) ModelHint {
	return ModelHint{Name: name}
}

func NewModelPreferences() *ModelPreferences {
	return &ModelPreferences{}
}

// WithHints sets model hints
func (mp *ModelPreferences) WithHints(hints ...ModelHint) *ModelPreferences {
	mp.Hints = hints
	return mp
}

// WithCostPriority sets cost priority (0-1)
func (mp *ModelPreferences) WithCostPriority(priority float64) *ModelPreferences {
	mp.CostPriority = &priority
	return mp
}

// WithSpeedPriority sets speed priority (0-1)
func (mp *ModelPreferences) WithSpeedPriority(priority float64) *ModelPreferences {
	mp.SpeedPriority = &priority
	return mp
}

// WithIntelligencePriority sets intelligence priority (0-1)
func (mp *ModelPreferences) WithIntelligencePriority(priority float64) *ModelPreferences {
	mp.IntelligencePriority = &priority
	return mp
}

// NewCreateMessageRequest creates a message request
func NewCreateMessageRequest(messages []SamplingMessage, maxTokens int) *CreateMessageRequest {
	return &CreateMessageRequest{
		Messages:  messages,
		MaxTokens: maxTokens,
	}
}

// WithModelPreferences sets model preferences
func (cmr *CreateMessageRequest) WithModelPreferences(prefs *ModelPreferences) *CreateMessageRequest {
	cmr.ModelPreferences = prefs
	return cmr
}

// WithSystemPrompt sets system prompt
func (cmr *CreateMessageRequest) WithSystemPrompt(prompt string) *CreateMessageRequest {
	cmr.SystemPrompt = prompt
	return cmr
}

// WithIncludeContext sets context inclusion options
func (cmr *CreateMessageRequest) WithIncludeContext(context IncludeContext) *CreateMessageRequest {
	cmr.IncludeContext = context
	return cmr
}

// WithTemperature sets temperature (0.0-1.0)
func (cmr *CreateMessageRequest) WithTemperature(temp float64) *CreateMessageRequest {
	cmr.Temperature = &temp
	return cmr
}

// WithStopSequences sets stop sequences
func (cmr *CreateMessageRequest) WithStopSequences(sequences ...string) *CreateMessageRequest {
	cmr.StopSequences = sequences
	return cmr
}

// WithMetadata sets metadata
func (cmr *CreateMessageRequest) WithMetadata(metadata map[string]interface{}) *CreateMessageRequest {
	cmr.Metadata = metadata
	return cmr
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
