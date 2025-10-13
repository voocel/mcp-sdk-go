package protocol

// Completion 参数自动补全 (MCP 2025-06-18)
// 为提示和资源 URI 参数提供智能补全建议

// CompletionCapability 补全能力声明
type CompletionCapability struct{}

// ReferenceType 引用类型
type ReferenceType string

const (
	ReferenceTypePrompt   ReferenceType = "ref/prompt"   // 提示引用
	ReferenceTypeResource ReferenceType = "ref/resource" // 资源引用
)

// PromptReference 提示引用
type PromptReference struct {
	Type ReferenceType `json:"type"` // 必须是 "ref/prompt"
	Name string        `json:"name"` // 提示名称
}

// ResourceReference 资源引用
type ResourceReference struct {
	Type ReferenceType `json:"type"` // 必须是 "ref/resource"
	URI  string        `json:"uri"`  // 资源 URI (可能包含模板变量)
}

// CompletionReference 补全引用 (PromptReference 或 ResourceReference)
type CompletionReference interface {
	GetType() ReferenceType
}

func (p PromptReference) GetType() ReferenceType {
	return p.Type
}

func (r ResourceReference) GetType() ReferenceType {
	return r.Type
}

// CompletionArgument 补全参数
type CompletionArgument struct {
	Name  string `json:"name"`  // 参数名称
	Value string `json:"value"` // 当前值
}

// CompletionContext 补全上下文
type CompletionContext struct {
	Arguments map[string]string `json:"arguments,omitempty"` // 已解析的参数映射
}

// CompleteRequest 补全请求 (completion/complete)
type CompleteRequest struct {
	Ref      map[string]any      `json:"ref"`                // 引用 (PromptReference 或 ResourceReference)
	Argument CompletionArgument  `json:"argument"`           // 要补全的参数
	Context  *CompletionContext  `json:"context,omitempty"`  // 可选的上下文
}

// CompletionResult 补全结果
type CompletionResult struct {
	Values  []string `json:"values"`           // 补全建议列表 (最多 100 个)
	Total   *int     `json:"total,omitempty"`  // 可选: 总匹配数
	HasMore bool     `json:"hasMore"`          // 是否有更多结果
}

// CompleteResult 补全响应
type CompleteResult struct {
	Completion CompletionResult `json:"completion"` // 补全结果
}

// UnmarshalCompletionReference 反序列化补全引用
func UnmarshalCompletionReference(data map[string]any) (CompletionReference, error) {
	refType, ok := data["type"].(string)
	if !ok {
		return nil, &MCPError{
			Code:    -32602,
			Message: "Invalid reference: missing or invalid type field",
		}
	}

	switch ReferenceType(refType) {
	case ReferenceTypePrompt:
		name, ok := data["name"].(string)
		if !ok {
			return nil, &MCPError{
				Code:    -32602,
				Message: "Invalid prompt reference: missing or invalid name field",
			}
		}
		return PromptReference{
			Type: ReferenceTypePrompt,
			Name: name,
		}, nil

	case ReferenceTypeResource:
		uri, ok := data["uri"].(string)
		if !ok {
			return nil, &MCPError{
				Code:    -32602,
				Message: "Invalid resource reference: missing or invalid uri field",
			}
		}
		return ResourceReference{
			Type: ReferenceTypeResource,
			URI:  uri,
		}, nil

	default:
		return nil, &MCPError{
			Code:    -32602,
			Message: "Invalid reference type: must be 'ref/prompt' or 'ref/resource'",
		}
	}
}

// NewPromptReference 创建提示引用
func NewPromptReference(name string) PromptReference {
	return PromptReference{
		Type: ReferenceTypePrompt,
		Name: name,
	}
}

// NewResourceReference 创建资源引用
func NewResourceReference(uri string) ResourceReference {
	return ResourceReference{
		Type: ReferenceTypeResource,
		URI:  uri,
	}
}

// NewCompletionResult 创建补全结果
func NewCompletionResult(values []string, hasMore bool) CompletionResult {
	// 限制最多 100 个结果
	if len(values) > 100 {
		values = values[:100]
		hasMore = true
	}

	return CompletionResult{
		Values:  values,
		HasMore: hasMore,
	}
}

// NewCompletionResultWithTotal 创建带总数的补全结果
func NewCompletionResultWithTotal(values []string, total int, hasMore bool) CompletionResult {
	// 限制最多 100 个结果
	if len(values) > 100 {
		values = values[:100]
		hasMore = true
	}

	return CompletionResult{
		Values:  values,
		Total:   &total,
		HasMore: hasMore,
	}
}

