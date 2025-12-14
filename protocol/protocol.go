package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
)

const (
	MCPVersion     = "2025-11-25"
	JSONRPCVersion = "2.0"

	// Supported protocol versions list (for backward compatibility check)
	MCPVersion2025_06_18 = "2025-06-18"
	MCPVersion2025_03_26 = "2025-03-26"
	MCPVersionLegacy     = "2024-11-05"
)

// JSON-RPC 2.0 standard error codes
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// MCP specific error codes
const (
	ToolNotFound     = -32000 // Tool not found
	ResourceNotFound = -32002 // Resource not found
	PromptNotFound   = -32001 // Prompt not found
	InvalidTool      = -32003 // Invalid tool
	InvalidResource  = -32004 // Invalid resource
	InvalidPrompt    = -32005 // Invalid prompt

	ErrorCodeInvalidParams = InvalidParams
)

type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPError represents MCP-specific error type
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *MCPError) Error() string {
	return e.Message
}

// NewMCPError creates a new MCP error
func NewMCPError(code int, message string, data interface{}) *MCPError {
	return &MCPError{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

type ContentType string

const (
	ContentTypeText         ContentType = "text"
	ContentTypeImage        ContentType = "image"
	ContentTypeAudio        ContentType = "audio"         // MCP 2025-06-18
	ContentTypeResourceLink ContentType = "resource_link" // MCP 2025-06-18
	ContentTypeResource     ContentType = "resource"      // MCP 2025-06-18: Embedded Resource
	ContentTypeToolUse      ContentType = "tool_use"      // MCP 2025-11-25: Tool use in sampling
	ContentTypeToolResult   ContentType = "tool_result"   // MCP 2025-11-25: Tool result in sampling
)

// Annotation represents content annotation (MCP 2025-06-18)
type Annotation struct {
	Audience     []Role  `json:"audience,omitempty"`     // Target audience (user, assistant)
	Priority     float64 `json:"priority,omitempty"`     // Priority (0.0-1.0)
	LastModified string  `json:"lastModified,omitempty"` // Last modified time (ISO 8601)
}

type TextContent struct {
	Type        ContentType `json:"type"`
	Text        string      `json:"text"`
	Annotations *Annotation `json:"annotations,omitempty"` // MCP 2025-06-18
}

type ImageContent struct {
	Type        ContentType `json:"type"`
	Data        string      `json:"data"`
	MimeType    string      `json:"mimeType"`
	Annotations *Annotation `json:"annotations,omitempty"` // MCP 2025-06-18
}

// AudioContent represents audio content (MCP 2025-06-18)
type AudioContent struct {
	Type        ContentType `json:"type"`
	Data        string      `json:"data"`     // Base64 encoded audio data
	MimeType    string      `json:"mimeType"` // e.g., audio/wav, audio/mp3
	Annotations *Annotation `json:"annotations,omitempty"`
}

// ResourceLinkContent represents resource link (MCP 2025-06-18)
type ResourceLinkContent struct {
	Type        ContentType `json:"type"`
	URI         string      `json:"uri"`
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
	MimeType    string      `json:"mimeType,omitempty"`
	Annotations *Annotation `json:"annotations,omitempty"`
}

// EmbeddedResourceContent represents embedded resource (MCP 2025-06-18)
type EmbeddedResourceContent struct {
	Type     ContentType      `json:"type"`
	Resource ResourceContents `json:"resource"`
}

// ToolUseContent represents a tool invocation request in sampling (MCP 2025-11-25)
type ToolUseContent struct {
	Type  ContentType            `json:"type"`
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
	Meta  map[string]any         `json:"_meta,omitempty"`
}

// ToolResultContent represents the result of a tool execution in sampling (MCP 2025-11-25)
type ToolResultContent struct {
	Type              ContentType            `json:"type"`
	ToolUseID         string                 `json:"toolUseId"`
	Content           []ContentBlock         `json:"content"`
	IsError           bool                   `json:"isError,omitempty"`
	StructuredContent map[string]interface{} `json:"structuredContent,omitempty"`
	Meta              map[string]any         `json:"_meta,omitempty"`
}

// ContentBlock represents a block of content in tool results (MCP 2025-11-25)
type ContentBlock struct {
	Type     ContentType `json:"type"`
	Text     string      `json:"text,omitempty"`
	Data     string      `json:"data,omitempty"`
	MimeType string      `json:"mimeType,omitempty"`
}

type Content interface {
	GetType() ContentType
}

func (tc TextContent) GetType() ContentType              { return tc.Type }
func (ic ImageContent) GetType() ContentType             { return ic.Type }
func (ac AudioContent) GetType() ContentType             { return ac.Type }
func (rlc ResourceLinkContent) GetType() ContentType     { return rlc.Type }
func (erc EmbeddedResourceContent) GetType() ContentType { return erc.Type }
func (tuc ToolUseContent) GetType() ContentType          { return tuc.Type }
func (trc ToolResultContent) GetType() ContentType       { return trc.Type }

func UnmarshalContent(data []byte) (Content, error) {
	var temp struct {
		Type ContentType `json:"type"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return nil, err
	}

	switch temp.Type {
	case ContentTypeText:
		var tc TextContent
		if err := json.Unmarshal(data, &tc); err != nil {
			return nil, err
		}
		return tc, nil
	case ContentTypeImage:
		var ic ImageContent
		if err := json.Unmarshal(data, &ic); err != nil {
			return nil, err
		}
		return ic, nil
	case ContentTypeAudio:
		var ac AudioContent
		if err := json.Unmarshal(data, &ac); err != nil {
			return nil, err
		}
		return ac, nil
	case ContentTypeResourceLink:
		var rlc ResourceLinkContent
		if err := json.Unmarshal(data, &rlc); err != nil {
			return nil, err
		}
		return rlc, nil
	case ContentTypeResource:
		var erc EmbeddedResourceContent
		if err := json.Unmarshal(data, &erc); err != nil {
			return nil, err
		}
		return erc, nil
	case ContentTypeToolUse:
		var tuc ToolUseContent
		if err := json.Unmarshal(data, &tuc); err != nil {
			return nil, err
		}
		return tuc, nil
	case ContentTypeToolResult:
		var trc ToolResultContent
		if err := json.Unmarshal(data, &trc); err != nil {
			return nil, err
		}
		return trc, nil
	default:
		// Default to text content
		var tc TextContent
		if err := json.Unmarshal(data, &tc); err != nil {
			return nil, err
		}
		return tc, nil
	}
}

func NewTextContent(text string) TextContent {
	return TextContent{Type: ContentTypeText, Text: text}
}

func NewImageContent(data, mimeType string) ImageContent {
	return ImageContent{Type: ContentTypeImage, Data: data, MimeType: mimeType}
}

// NewAudioContent creates audio content (MCP 2025-06-18)
func NewAudioContent(data, mimeType string) AudioContent {
	return AudioContent{Type: ContentTypeAudio, Data: data, MimeType: mimeType}
}

// NewResourceLinkContent creates resource link content (MCP 2025-06-18)
func NewResourceLinkContent(uri string) ResourceLinkContent {
	return ResourceLinkContent{Type: ContentTypeResourceLink, URI: uri}
}

// NewResourceLinkContentWithDetails creates resource link with details (MCP 2025-06-18)
func NewResourceLinkContentWithDetails(uri, name, description, mimeType string) ResourceLinkContent {
	return ResourceLinkContent{
		Type:        ContentTypeResourceLink,
		URI:         uri,
		Name:        name,
		Description: description,
		MimeType:    mimeType,
	}
}

// NewEmbeddedResourceContent creates embedded resource content (MCP 2025-06-18)
func NewEmbeddedResourceContent(resource ResourceContents) EmbeddedResourceContent {
	return EmbeddedResourceContent{Type: ContentTypeResource, Resource: resource}
}

// NewToolUseContent creates tool use content for sampling (MCP 2025-11-25)
func NewToolUseContent(id, name string, input map[string]interface{}) ToolUseContent {
	return ToolUseContent{
		Type:  ContentTypeToolUse,
		ID:    id,
		Name:  name,
		Input: input,
	}
}

// NewToolResultContent creates tool result content for sampling (MCP 2025-11-25)
func NewToolResultContent(toolUseID string, content []ContentBlock) ToolResultContent {
	return ToolResultContent{
		Type:      ContentTypeToolResult,
		ToolUseID: toolUseID,
		Content:   content,
	}
}

// NewToolResultContentWithError creates tool result content with error flag (MCP 2025-11-25)
func NewToolResultContentWithError(toolUseID string, content []ContentBlock, isError bool) ToolResultContent {
	return ToolResultContent{
		Type:      ContentTypeToolResult,
		ToolUseID: toolUseID,
		Content:   content,
		IsError:   isError,
	}
}

// NewTextContentBlock creates a text content block (MCP 2025-11-25)
func NewTextContentBlock(text string) ContentBlock {
	return ContentBlock{
		Type: ContentTypeText,
		Text: text,
	}
}

// NewImageContentBlock creates an image content block (MCP 2025-11-25)
func NewImageContentBlock(data, mimeType string) ContentBlock {
	return ContentBlock{
		Type:     ContentTypeImage,
		Data:     data,
		MimeType: mimeType,
	}
}

// WithAnnotations adds annotations to content (MCP 2025-06-18)
func (tc *TextContent) WithAnnotations(annotations *Annotation) *TextContent {
	tc.Annotations = annotations
	return tc
}

func (ic *ImageContent) WithAnnotations(annotations *Annotation) *ImageContent {
	ic.Annotations = annotations
	return ic
}

func (ac *AudioContent) WithAnnotations(annotations *Annotation) *AudioContent {
	ac.Annotations = annotations
	return ac
}

func (rlc *ResourceLinkContent) WithAnnotations(annotations *Annotation) *ResourceLinkContent {
	rlc.Annotations = annotations
	return rlc
}

// NewAnnotation creates annotation (MCP 2025-06-18)
func NewAnnotation() *Annotation {
	return &Annotation{}
}

func (a *Annotation) WithAudience(audience ...Role) *Annotation {
	a.Audience = audience
	return a
}

func (a *Annotation) WithPriority(priority float64) *Annotation {
	a.Priority = priority
	return a
}

func (a *Annotation) WithLastModified(lastModified string) *Annotation {
	a.LastModified = lastModified
	return a
}

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

type ClientCapabilities struct {
	Roots        *RootsCapability       `json:"roots,omitempty"`
	Sampling     *SamplingCapability    `json:"sampling,omitempty"`
	Elicitation  *ElicitationCapability `json:"elicitation,omitempty"`
	Tasks        *ClientTasksCapability `json:"tasks,omitempty"` // MCP 2025-11-25
	Experimental map[string]interface{} `json:"experimental,omitempty"`
}

type ServerCapabilities struct {
	Tools        *ToolsCapability       `json:"tools,omitempty"`
	Resources    *ResourcesCapability   `json:"resources,omitempty"`
	Prompts      *PromptsCapability     `json:"prompts,omitempty"`
	Logging      *LoggingCapability     `json:"logging,omitempty"`
	Completion   *CompletionCapability  `json:"completion,omitempty"` // MCP 2025-06-18: Parameter auto-completion
	Tasks        *TasksCapability       `json:"tasks,omitempty"`      // MCP 2025-11-25
	Experimental map[string]interface{} `json:"experimental,omitempty"`
}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type SamplingCapability struct {
	// Tools indicates client supports tool use in sampling requests (MCP 2025-11-25)
	Tools *struct{} `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type LoggingCapability struct{}

// ElicitationCapability represents elicitation capability declaration
type ElicitationCapability struct{}

// Icon defines icon for visual identification of resources, tools, prompts, and implementations
type Icon struct {
	// Source is the URI pointing to the icon resource (required), can be:
	// - HTTP/HTTPS URL pointing to an image file
	// - data URI containing base64 encoded image data
	Source string `json:"src"`
	// MIMEType is an optional MIME type
	MIMEType string `json:"mimeType,omitempty"`
	// Sizes is an optional size specification (e.g., ["48x48"], ["any"] for scalable formats like SVG)
	Sizes []string `json:"sizes,omitempty"`
	// Theme is an optional theme, such as "light" or "dark"
	Theme string `json:"theme,omitempty"`
}

type ClientInfo struct {
	Name       string `json:"name"`
	Title      string `json:"title,omitempty"`
	Version    string `json:"version"`
	WebsiteURL string `json:"websiteUrl,omitempty"`
	Icons      []Icon `json:"icons,omitempty"`
}

type ServerInfo struct {
	Name       string `json:"name"`
	Title      string `json:"title,omitempty"`
	Version    string `json:"version"`
	WebsiteURL string `json:"websiteUrl,omitempty"`
	Icons      []Icon `json:"icons,omitempty"`
}

// InitializeParams represents initialize request parameters
type InitializeParams struct {
	Meta            map[string]any     `json:"_meta,omitempty"`
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

// InitializeResult represents initialize response
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

type InitializedParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

type EmptyResult struct{}

type ToolListChangedParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

type ResourceListChangedParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

type PromptListChangedParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

type JSONSchema map[string]interface{}

type PaginationParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type PaginatedResult struct {
	NextCursor *string `json:"nextCursor,omitempty"`
}

// IsVersionSupported checks if the protocol version is supported
func IsVersionSupported(version string) bool {
	supportedVersions := []string{
		MCPVersion,           // 2025-11-25
		MCPVersion2025_06_18, // 2025-06-18
		MCPVersion2025_03_26, // 2025-03-26
		MCPVersionLegacy,     // 2024-11-05
	}

	for _, supported := range supportedVersions {
		if version == supported {
			return true
		}
	}
	return false
}

func GetSupportedVersions() []string {
	return []string{
		MCPVersion,           // Latest version first
		MCPVersion2025_06_18, // Previous stable version
		MCPVersion2025_03_26, // Intermediate version
		MCPVersionLegacy,     // Backward compatibility
	}
}

func IDToString(id json.RawMessage) string {
	if len(id) == 0 {
		return ""
	}

	var strID string
	if err := json.Unmarshal(id, &strID); err == nil {
		return strID
	}

	var numID float64
	if err := json.Unmarshal(id, &numID); err == nil {
		if numID == float64(int64(numID)) {
			return fmt.Sprintf("%.0f", numID)
		}
		return fmt.Sprintf("%g", numID)
	}

	return string(id)
}

// StringToID converts string to JSON-RPC ID
func StringToID(id string) json.RawMessage {
	if id == "" {
		return nil
	}

	if num, err := strconv.ParseFloat(id, 64); err == nil {
		if num == float64(int64(num)) {
			return json.RawMessage(fmt.Sprintf("%.0f", num))
		}
		return json.RawMessage(fmt.Sprintf("%g", num))
	}

	idBytes, _ := json.Marshal(id)
	return idBytes
}

func (m *JSONRPCMessage) IsNotification() bool {
	return len(m.ID) == 0
}

func (m *JSONRPCMessage) GetIDString() string {
	return IDToString(m.ID)
}
