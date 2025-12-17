package protocol

import (
	"encoding/json"
	"fmt"
)

type ElicitationAction string

const (
	ElicitationActionAccept  ElicitationAction = "accept"
	ElicitationActionDecline ElicitationAction = "decline"
	ElicitationActionCancel  ElicitationAction = "cancel"
)

// ElicitationMode represents the mode of elicitation (MCP 2025-11-25)
type ElicitationMode string

const (
	// ElicitationModeForm is for in-band structured data collection
	ElicitationModeForm ElicitationMode = "form"
	// ElicitationModeURL is for out-of-band interaction via URL navigation
	ElicitationModeURL ElicitationMode = "url"
)

// ElicitationCreateParams represents the parameters for elicitation/create request
// Supports both form mode and URL mode (MCP 2025-11-25)
type ElicitationCreateParams struct {
	// Mode specifies the elicitation mode: "form" or "url" (MCP 2025-11-25)
	// Defaults to "form" for backward compatibility
	Mode ElicitationMode `json:"mode,omitempty"`
	// Message is the human-readable message to display to the user
	Message string `json:"message"`
	// RequestedSchema is the JSON schema for form mode validation (form mode only)
	RequestedSchema JSONSchema `json:"requestedSchema,omitempty"`
	// ElicitationID is a unique identifier for URL mode elicitation (url mode only)
	ElicitationID string `json:"elicitationId,omitempty"`
	// URL is the URL to navigate to for out-of-band interaction (url mode only)
	URL string `json:"url,omitempty"`
}

// ElicitationResult represents the result of an elicitation request
type ElicitationResult struct {
	Action  ElicitationAction `json:"action"`
	Content interface{}       `json:"content,omitempty"`
}

// ElicitationCompleteNotificationParams represents the parameters for
// notifications/elicitation/complete notification (MCP 2025-11-25)
// Sent when URL mode elicitation is completed
type ElicitationCompleteNotificationParams struct {
	ElicitationID string `json:"elicitationId"`
}

func NewElicitationCreateParams(message string, schema JSONSchema) *ElicitationCreateParams {
	return &ElicitationCreateParams{
		Message:         message,
		RequestedSchema: schema,
	}
}

func NewElicitationResult(action ElicitationAction, content interface{}) *ElicitationResult {
	return &ElicitationResult{
		Action:  action,
		Content: content,
	}
}

func NewElicitationAccept(content interface{}) *ElicitationResult {
	return NewElicitationResult(ElicitationActionAccept, content)
}

func NewElicitationDecline() *ElicitationResult {
	return NewElicitationResult(ElicitationActionDecline, nil)
}

func NewElicitationCancel() *ElicitationResult {
	return NewElicitationResult(ElicitationActionCancel, nil)
}

func (r *ElicitationResult) IsAccepted() bool {
	return r.Action == ElicitationActionAccept
}

func (r *ElicitationResult) IsDeclined() bool {
	return r.Action == ElicitationActionDecline
}

func (r *ElicitationResult) IsCancelled() bool {
	return r.Action == ElicitationActionCancel
}

func (r *ElicitationResult) Validate() error {
	switch r.Action {
	case ElicitationActionAccept:
		if r.Content == nil {
			return fmt.Errorf("elicitation accept action must have content")
		}
	case ElicitationActionDecline, ElicitationActionCancel:
		// decline and cancel should not have content
		if r.Content != nil {
			return fmt.Errorf("elicitation %s action should not have content", r.Action)
		}
	default:
		return fmt.Errorf("invalid elicitation action: %s", r.Action)
	}
	return nil
}

func (r *ElicitationResult) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}

	type Alias ElicitationResult
	return json.Marshal((*Alias)(r))
}

func (r *ElicitationResult) UnmarshalJSON(data []byte) error {
	type Alias ElicitationResult
	aux := (*Alias)(r)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	return r.Validate()
}

// ValidateElicitationAction validates whether the elicitation action is valid
func ValidateElicitationAction(action string) bool {
	switch ElicitationAction(action) {
	case ElicitationActionAccept, ElicitationActionDecline, ElicitationActionCancel:
		return true
	default:
		return false
	}
}

// CreateElicitationSchema creates a common elicitation schema
func CreateElicitationSchema() JSONSchema {
	return JSONSchema{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type": "string",
				"enum": []string{
					string(ElicitationActionAccept),
					string(ElicitationActionDecline),
					string(ElicitationActionCancel),
				},
			},
			"content": map[string]interface{}{
				"type": "object",
			},
		},
		"required": []string{"action"},
	}
}

// CreateStringElicitationSchema creates a schema for requesting string input
func CreateStringElicitationSchema(propertyName, description string, required bool) JSONSchema {
	schema := JSONSchema{
		"type": "object",
		"properties": map[string]interface{}{
			propertyName: map[string]interface{}{
				"type":        "string",
				"description": description,
			},
		},
	}

	if required {
		schema["required"] = []string{propertyName}
	}

	return schema
}

// CreateNumberElicitationSchema creates a schema for requesting number input
func CreateNumberElicitationSchema(propertyName, description string, min, max *float64, required bool) JSONSchema {
	prop := map[string]interface{}{
		"type":        "number",
		"description": description,
	}

	if min != nil {
		prop["minimum"] = *min
	}
	if max != nil {
		prop["maximum"] = *max
	}

	schema := JSONSchema{
		"type": "object",
		"properties": map[string]interface{}{
			propertyName: prop,
		},
	}

	if required {
		schema["required"] = []string{propertyName}
	}

	return schema
}

// CreateBooleanElicitationSchema creates a schema for requesting boolean input
func CreateBooleanElicitationSchema(propertyName, description string, defaultValue *bool, required bool) JSONSchema {
	prop := map[string]interface{}{
		"type":        "boolean",
		"description": description,
	}

	if defaultValue != nil {
		prop["default"] = *defaultValue
	}

	schema := JSONSchema{
		"type": "object",
		"properties": map[string]interface{}{
			propertyName: prop,
		},
	}

	if required {
		schema["required"] = []string{propertyName}
	}

	return schema
}

// CreateEnumElicitationSchema creates a schema for requesting enum selection
func CreateEnumElicitationSchema(propertyName, description string, options []string, optionNames []string, required bool) JSONSchema {
	prop := map[string]interface{}{
		"type":        "string",
		"description": description,
		"enum":        options,
	}

	if len(optionNames) > 0 && len(optionNames) == len(options) {
		prop["enumNames"] = optionNames
	}

	schema := JSONSchema{
		"type": "object",
		"properties": map[string]interface{}{
			propertyName: prop,
		},
	}

	if required {
		schema["required"] = []string{propertyName}
	}

	return schema
}
