package protocol

import (
	"encoding/json"
	"time"
)

const MCPVersion = "0.1.0"

type ContentType string

const (
	ContentTypeText ContentType = "text"
	ContentTypeJSON ContentType = "json"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

type TextContent struct {
	Type ContentType `json:"type"`
	Text string      `json:"text"`
}

type JSONContent struct {
	Type ContentType     `json:"type"`
	JSON json.RawMessage `json:"json"`
}

type Content interface {
	GetType() ContentType
}

func (tc TextContent) GetType() ContentType {
	return tc.Type
}

func (jc JSONContent) GetType() ContentType {
	return jc.Type
}

func NewTextContent(text string) TextContent {
	return TextContent{
		Type: ContentTypeText,
		Text: text,
	}
}

func NewJSONContent(data interface{}) (JSONContent, error) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return JSONContent{}, err
	}

	return JSONContent{
		Type: ContentTypeJSON,
		JSON: jsonBytes,
	}, nil
}

type Message struct {
	ID        string          `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

type Response struct {
	ID        string          `json:"id"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *Error          `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type Capabilities struct {
	Tools     bool `json:"tools"`
	Resources bool `json:"resources"`
	Prompts   bool `json:"prompts"`
}

type ServerInfo struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Capabilities Capabilities `json:"capabilities"`
}

type Subscription struct {
	ID      string   `json:"id"`
	Methods []string `json:"methods"`
}

type Notification struct {
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	Timestamp time.Time       `json:"timestamp"`
}
