package protocol

// SamplingMessage 采样消息
type SamplingMessage struct {
	Role    Role    `json:"role"`
	Content Content `json:"content"`
}

// ModelHint 模型提示
type ModelHint struct {
	Name string `json:"name,omitempty"`
}

// ModelPreferences 模型偏好设置
type ModelPreferences struct {
	Hints                []ModelHint `json:"hints,omitempty"`
	CostPriority         *float64    `json:"costPriority,omitempty"`         // 0-1, 成本优先级
	SpeedPriority        *float64    `json:"speedPriority,omitempty"`        // 0-1, 速度优先级
	IntelligencePriority *float64    `json:"intelligencePriority,omitempty"` // 0-1, 智能优先级
}

// IncludeContext 上下文包含选项
type IncludeContext string

const (
	IncludeContextNone       IncludeContext = "none"
	IncludeContextThisServer IncludeContext = "thisServer"
	IncludeContextAllServers IncludeContext = "allServers"
)

// CreateMessageRequest 创建消息请求 (服务器发起的LLM采样)
type CreateMessageRequest struct {
	Meta             map[string]any         `json:"_meta,omitempty"`
	Messages         []SamplingMessage      `json:"messages"`
	ModelPreferences *ModelPreferences      `json:"modelPreferences,omitempty"`
	SystemPrompt     string                 `json:"systemPrompt,omitempty"`
	IncludeContext   IncludeContext         `json:"includeContext,omitempty"`
	Temperature      *float64               `json:"temperature,omitempty"` // 0.0-1.0
	MaxTokens        int                    `json:"maxTokens"`             // 必需
	StopSequences    []string               `json:"stopSequences,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// CreateMessageParams 是 CreateMessageRequest 的别名,用于保持一致性
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

// WithHints 设置模型提示
func (mp *ModelPreferences) WithHints(hints ...ModelHint) *ModelPreferences {
	mp.Hints = hints
	return mp
}

// WithCostPriority 设置成本优先级 (0-1)
func (mp *ModelPreferences) WithCostPriority(priority float64) *ModelPreferences {
	mp.CostPriority = &priority
	return mp
}

// WithSpeedPriority 设置速度优先级 (0-1)
func (mp *ModelPreferences) WithSpeedPriority(priority float64) *ModelPreferences {
	mp.SpeedPriority = &priority
	return mp
}

// WithIntelligencePriority 设置智能优先级 (0-1)
func (mp *ModelPreferences) WithIntelligencePriority(priority float64) *ModelPreferences {
	mp.IntelligencePriority = &priority
	return mp
}

// NewCreateMessageRequest 创建消息请求
func NewCreateMessageRequest(messages []SamplingMessage, maxTokens int) *CreateMessageRequest {
	return &CreateMessageRequest{
		Messages:  messages,
		MaxTokens: maxTokens,
	}
}

// WithModelPreferences 设置模型偏好
func (cmr *CreateMessageRequest) WithModelPreferences(prefs *ModelPreferences) *CreateMessageRequest {
	cmr.ModelPreferences = prefs
	return cmr
}

// WithSystemPrompt 设置系统提示
func (cmr *CreateMessageRequest) WithSystemPrompt(prompt string) *CreateMessageRequest {
	cmr.SystemPrompt = prompt
	return cmr
}

// WithIncludeContext 设置上下文包含选项
func (cmr *CreateMessageRequest) WithIncludeContext(context IncludeContext) *CreateMessageRequest {
	cmr.IncludeContext = context
	return cmr
}

// WithTemperature 设置温度 (0.0-1.0)
func (cmr *CreateMessageRequest) WithTemperature(temp float64) *CreateMessageRequest {
	cmr.Temperature = &temp
	return cmr
}

// WithStopSequences 设置停止序列
func (cmr *CreateMessageRequest) WithStopSequences(sequences ...string) *CreateMessageRequest {
	cmr.StopSequences = sequences
	return cmr
}

// WithMetadata 设置元数据
func (cmr *CreateMessageRequest) WithMetadata(metadata map[string]interface{}) *CreateMessageRequest {
	cmr.Metadata = metadata
	return cmr
}

// NewCreateMessageResult 创建消息结果
func NewCreateMessageResult(role Role, content Content, model string, stopReason StopReason) *CreateMessageResult {
	return &CreateMessageResult{
		Role:       role,
		Content:    content,
		Model:      model,
		StopReason: stopReason,
	}
}

// 验证方法

// Validate 验证创建消息请求
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

// Validate 验证模型偏好设置
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
