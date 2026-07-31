package protocol

const (
	RefPrompt   = "ref/prompt"
	RefResource = "ref/resource"
)

// CompletionRef identifies what is being completed: a prompt (by name) or a
// resource template (by uri).
type CompletionRef struct {
	Type string `json:"type"` // RefPrompt | RefResource
	Name string `json:"name,omitempty"`
	URI  string `json:"uri,omitempty"`
}

type CompleteArgument struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type CompleteContext struct {
	Arguments map[string]string `json:"arguments,omitempty"`
}

type CompleteParams struct {
	Meta     RequestMeta      `json:"_meta"`
	Ref      CompletionRef    `json:"ref"`
	Argument CompleteArgument `json:"argument"`
	Context  *CompleteContext `json:"context,omitempty"`
}

const MaxCompletionValues = 100

type CompletionValues struct {
	Values  []string `json:"values"`
	Total   int      `json:"total,omitempty"`
	HasMore bool     `json:"hasMore,omitempty"`
}

type CompleteResult struct {
	WithMeta
	Completion CompletionValues `json:"completion"`
}

func (*CompleteResult) ResultType() string { return ResultTypeComplete }

// NewCompleteResult caps values at MaxCompletionValues and sets hasMore
// accordingly.
func NewCompleteResult(values []string) *CompleteResult {
	total := len(values)
	hasMore := false
	if len(values) > MaxCompletionValues {
		values = values[:MaxCompletionValues]
		hasMore = true
	}
	return &CompleteResult{Completion: CompletionValues{Values: values, Total: total, HasMore: hasMore}}
}
