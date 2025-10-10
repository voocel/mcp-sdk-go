package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
)

const (
	MCPVersion     = "2025-06-18"
	JSONRPCVersion = "2.0"

	// 支持的协议版本列表（用于向后兼容性检查）
	MCPVersion2025_03_26 = "2025-03-26"
	MCPVersionLegacy     = "2024-11-05"
)

// JSON-RPC 2.0 标准错误代码
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// MCP 特定错误代码
const (
	ToolNotFound     = -32000 // 工具未找到
	ResourceNotFound = -32002 // 资源未找到
	PromptNotFound   = -32001 // 提示模板未找到
	InvalidTool      = -32003 // 无效工具
	InvalidResource  = -32004 // 无效资源
	InvalidPrompt    = -32005 // 无效提示模板

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

// MCPError MCP特定错误类型
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *MCPError) Error() string {
	return e.Message
}

// NewMCPError 创建新的MCP错误
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
)

// Annotation 内容注解 (MCP 2025-06-18)
type Annotation struct {
	Audience     []Role  `json:"audience,omitempty"`     // 目标受众 (user, assistant)
	Priority     float64 `json:"priority,omitempty"`     // 优先级 (0.0-1.0)
	LastModified string  `json:"lastModified,omitempty"` // 最后修改时间 (ISO 8601)
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

// AudioContent 音频内容 (MCP 2025-06-18)
type AudioContent struct {
	Type        ContentType `json:"type"`
	Data        string      `json:"data"`     // Base64 编码的音频数据
	MimeType    string      `json:"mimeType"` // 例如: audio/wav, audio/mp3
	Annotations *Annotation `json:"annotations,omitempty"`
}

// ResourceLinkContent 资源链接 (MCP 2025-06-18)
type ResourceLinkContent struct {
	Type        ContentType `json:"type"`
	URI         string      `json:"uri"`
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
	MimeType    string      `json:"mimeType,omitempty"`
	Annotations *Annotation `json:"annotations,omitempty"`
}

// EmbeddedResourceContent 嵌入式资源 (MCP 2025-06-18)
type EmbeddedResourceContent struct {
	Type     ContentType      `json:"type"`
	Resource ResourceContents `json:"resource"`
}

type Content interface {
	GetType() ContentType
}

func (tc TextContent) GetType() ContentType              { return tc.Type }
func (ic ImageContent) GetType() ContentType             { return ic.Type }
func (ac AudioContent) GetType() ContentType             { return ac.Type }
func (rlc ResourceLinkContent) GetType() ContentType     { return rlc.Type }
func (erc EmbeddedResourceContent) GetType() ContentType { return erc.Type }

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
	default:
		// 默认当作文本内容处理
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

// NewAudioContent 创建音频内容 (MCP 2025-06-18)
func NewAudioContent(data, mimeType string) AudioContent {
	return AudioContent{Type: ContentTypeAudio, Data: data, MimeType: mimeType}
}

// NewResourceLinkContent 创建资源链接内容 (MCP 2025-06-18)
func NewResourceLinkContent(uri string) ResourceLinkContent {
	return ResourceLinkContent{Type: ContentTypeResourceLink, URI: uri}
}

// NewResourceLinkContentWithDetails 创建带详细信息的资源链接 (MCP 2025-06-18)
func NewResourceLinkContentWithDetails(uri, name, description, mimeType string) ResourceLinkContent {
	return ResourceLinkContent{
		Type:        ContentTypeResourceLink,
		URI:         uri,
		Name:        name,
		Description: description,
		MimeType:    mimeType,
	}
}

// NewEmbeddedResourceContent 创建嵌入式资源内容 (MCP 2025-06-18)
func NewEmbeddedResourceContent(resource ResourceContents) EmbeddedResourceContent {
	return EmbeddedResourceContent{Type: ContentTypeResource, Resource: resource}
}

// WithAnnotations 为内容添加注解 (MCP 2025-06-18)
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

// NewAnnotation 创建注解 (MCP 2025-06-18)
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
	Experimental map[string]interface{} `json:"experimental,omitempty"`
}

type ServerCapabilities struct {
	Tools        *ToolsCapability       `json:"tools,omitempty"`
	Resources    *ResourcesCapability   `json:"resources,omitempty"`
	Prompts      *PromptsCapability     `json:"prompts,omitempty"`
	Logging      *LoggingCapability     `json:"logging,omitempty"`
	Experimental map[string]interface{} `json:"experimental,omitempty"`
}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type SamplingCapability struct{}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
	Templates   bool `json:"templates,omitempty"`
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type LoggingCapability struct{}

// Elicitation 能力声明
type ElicitationCapability struct{}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeRequest struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

type JSONSchema map[string]interface{}

type PaginationParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type PaginatedResult struct {
	NextCursor *string `json:"nextCursor,omitempty"`
}

// IsVersionSupported 检查协议版本是否受支持
func IsVersionSupported(version string) bool {
	supportedVersions := []string{
		MCPVersion,           // 2025-06-18
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
		MCPVersion,           // 最新版本优先
		MCPVersion2025_03_26, // 中间版本
		MCPVersionLegacy,     // 向后兼容
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

// StringToID 将字符串转换为 JSON-RPC ID
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
