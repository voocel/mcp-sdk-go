package protocol

// Completion parameter auto-completion (MCP 2025-06-18)
// Provides intelligent completion suggestions for prompt and resource URI parameters

// CompletionCapability completion capability declaration
type CompletionCapability struct{}

type ReferenceType string

const (
	ReferenceTypePrompt   ReferenceType = "ref/prompt"   // Prompt reference
	ReferenceTypeResource ReferenceType = "ref/resource" // Resource reference
)

type PromptReference struct {
	Type ReferenceType `json:"type"` // Must be "ref/prompt"
	Name string        `json:"name"` // Prompt name
}

// ResourceReference resource reference
type ResourceReference struct {
	Type ReferenceType `json:"type"` // Must be "ref/resource"
	URI  string        `json:"uri"`  // Resource URI (may contain template variables)
}

// CompletionReference completion reference (PromptReference or ResourceReference)
type CompletionReference interface {
	GetType() ReferenceType
}

func (p PromptReference) GetType() ReferenceType {
	return p.Type
}

func (r ResourceReference) GetType() ReferenceType {
	return r.Type
}

type CompletionArgument struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type CompletionContext struct {
	Arguments map[string]string `json:"arguments,omitempty"`
}

// CompleteRequest represents the completion request (completion/complete)
type CompleteRequest struct {
	Ref      map[string]any     `json:"ref"`               // Reference (PromptReference or ResourceReference)
	Argument CompletionArgument `json:"argument"`          // Argument to complete
	Context  *CompletionContext `json:"context,omitempty"` // Optional context
}

// CompletionResult represents completion result
type CompletionResult struct {
	Values  []string `json:"values"`          // Completion suggestions (max 100)
	Total   *int     `json:"total,omitempty"` // Optional: total matches count
	HasMore bool     `json:"hasMore"`         // Whether there are more results
}

// CompleteResult represents the completion response
type CompleteResult struct {
	Completion CompletionResult `json:"completion"` // Completion result
}

// UnmarshalCompletionReference deserializes completion reference
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

func NewPromptReference(name string) PromptReference {
	return PromptReference{
		Type: ReferenceTypePrompt,
		Name: name,
	}
}

func NewResourceReference(uri string) ResourceReference {
	return ResourceReference{
		Type: ReferenceTypeResource,
		URI:  uri,
	}
}

func NewCompletionResult(values []string, hasMore bool) CompletionResult {
	// Limit to maximum 100 results
	if len(values) > 100 {
		values = values[:100]
		hasMore = true
	}

	return CompletionResult{
		Values:  values,
		HasMore: hasMore,
	}
}

func NewCompletionResultWithTotal(values []string, total int, hasMore bool) CompletionResult {
	// Limit to maximum 100 results
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
