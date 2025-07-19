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
	ToolNotFound     = -32000
	ResourceNotFound = -32001
	PromptNotFound   = -32002
	InvalidTool      = -32003
	InvalidResource  = -32004
	InvalidPrompt    = -32005
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

type ContentType string

const (
	ContentTypeText  ContentType = "text"
	ContentTypeImage ContentType = "image"
)

type TextContent struct {
	Type ContentType `json:"type"`
	Text string      `json:"text"`
}

type ImageContent struct {
	Type     ContentType `json:"type"`
	Data     string      `json:"data"`
	MimeType string      `json:"mimeType"`
}

type Content interface {
	GetType() ContentType
}

func (tc TextContent) GetType() ContentType  { return tc.Type }
func (ic ImageContent) GetType() ContentType { return ic.Type }

// UnmarshalJSON 为 Content 接口实现自定义 JSON 反序列化
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

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

type ClientCapabilities struct {
	Roots        *RootsCapability       `json:"roots,omitempty"`
	Sampling     *SamplingCapability    `json:"sampling,omitempty"`
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
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type LoggingCapability struct{}

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

// GetSupportedVersions 返回所有支持的协议版本
func GetSupportedVersions() []string {
	return []string{
		MCPVersion,           // 最新版本优先
		MCPVersion2025_03_26, // 中间版本
		MCPVersionLegacy,     // 向后兼容
	}
}

// IDToString 将 JSON-RPC ID 转换为字符串
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
	return json.RawMessage(idBytes)
}

// IsNotification 检查消息是否为通知（没有 ID）
func (m *JSONRPCMessage) IsNotification() bool {
	return len(m.ID) == 0
}

// GetIDString 获取消息 ID 的字符串表示
func (m *JSONRPCMessage) GetIDString() string {
	return IDToString(m.ID)
}
