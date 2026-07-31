package protocol

import (
	"encoding/json"
	"testing"
)

// Interim result fixture verbatim from server/tools.md (2026-07-28).
const specInterimResult = `{
	"resultType": "input_required",
	"inputRequests": {
		"github_login": {
			"method": "elicitation/create",
			"params": {
				"mode": "form",
				"message": "Please provide your GitHub username",
				"requestedSchema": {
					"type": "object",
					"properties": { "name": { "type": "string" } },
					"required": ["name"]
				}
			}
		}
	},
	"requestState": "eyJsb2NhdGlvbiI6Ik5ldyBZb3JrIn0..."
}`

func TestInputRequiredMatchesSpecFixture(t *testing.T) {
	res := RequireInput(InputRequests{
		"github_login": NewElicitFormRequest("Please provide your GitHub username", JSONSchema{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
			"required":   []any{"name"},
		}),
	}, "eyJsb2NhdGlvbiI6Ik5ldyBZb3JrIn0...")
	if err := res.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := MarshalResult(res)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(specInterimResult))
}

func TestInputRequiredDecode(t *testing.T) {
	rt, err := PeekResultType([]byte(specInterimResult))
	if err != nil || rt != ResultTypeInputRequired {
		t.Fatalf("PeekResultType = %q, %v", rt, err)
	}
	var res InputRequired
	if err := json.Unmarshal([]byte(specInterimResult), &res); err != nil {
		t.Fatal(err)
	}
	req, ok := res.Requests["github_login"]
	if !ok {
		t.Fatal("missing github_login request")
	}
	p, err := req.Elicit()
	if err != nil {
		t.Fatal(err)
	}
	if p.EffectiveMode() != ElicitModeForm || p.Message != "Please provide your GitHub username" {
		t.Fatalf("elicit params mismatch: %+v", p)
	}
	if res.State != "eyJsb2NhdGlvbiI6Ik5ldyBZb3JrIn0..." {
		t.Fatalf("state = %q", res.State)
	}
}

// Retry fixture verbatim from server/tools.md: inputResponses and requestState
// are siblings of name/arguments.
func TestRetryParamsMatchSpecFixture(t *testing.T) {
	responses := InputResponses{}
	if err := responses.Set("github_login", ElicitResult{
		Action:  ElicitActionAccept,
		Content: map[string]any{"name": "octocat"},
	}); err != nil {
		t.Fatal(err)
	}
	p := CallToolParams{
		Name:           "get_weather",
		Arguments:      map[string]any{"location": "New York"},
		InputResponses: responses,
		RequestState:   "eyJsb2NhdGlvbiI6Ik5ldyBZb3JrIn0...",
	}
	type noMeta CallToolParams // fixture omits _meta; compare payload fields only
	raw, err := json.Marshal(struct {
		noMeta
		Meta any `json:"_meta,omitempty"`
	}{noMeta: noMeta(p)})
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{
		"name": "get_weather",
		"arguments": { "location": "New York" },
		"inputResponses": {
			"github_login": { "action": "accept", "content": { "name": "octocat" } }
		},
		"requestState": "eyJsb2NhdGlvbiI6Ik5ldyBZb3JrIn0..."
	}`))
}

func TestInputRequiredValidate(t *testing.T) {
	if err := RequireInput(nil, "").Validate(); err == nil {
		t.Fatal("expected validation error for empty InputRequired")
	}
	if err := RequireInput(nil, "state-only").Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestInputResponsesElicit(t *testing.T) {
	r := InputResponses{"k": json.RawMessage(`{"action":"decline"}`)}
	res, err := r.Elicit("k")
	if err != nil || res.Action != ElicitActionDecline {
		t.Fatalf("Elicit = %+v, %v", res, err)
	}
	if _, err := r.Elicit("missing"); err == nil {
		t.Fatal("expected error for missing key")
	}
}

// The sum types are closed: a CallToolResult and an InputRequired are the only
// ToolResponse implementations. This is a compile-time assertion.
var (
	_ ToolResponse     = (*CallToolResult)(nil)
	_ ToolResponse     = (*InputRequired)(nil)
	_ PromptResponse   = (*GetPromptResult)(nil)
	_ PromptResponse   = (*InputRequired)(nil)
	_ ResourceResponse = (*ReadResourceResult)(nil)
	_ ResourceResponse = (*InputRequired)(nil)
)
