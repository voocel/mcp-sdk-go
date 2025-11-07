package protocol

// LoggingLevel 日志级别
// 映射到 syslog 消息严重性,如 RFC-5424 中所述:
// https://datatracker.ietf.org/doc/html/rfc5424#section-6.2.1
type LoggingLevel string

const (
	LogLevelDebug     LoggingLevel = "debug"     // 调试级别消息
	LogLevelInfo      LoggingLevel = "info"      // 信息级别消息
	LogLevelNotice    LoggingLevel = "notice"    // 正常但重要的消息
	LogLevelWarning   LoggingLevel = "warning"   // 警告消息
	LogLevelError     LoggingLevel = "error"     // 错误消息
	LogLevelCritical  LoggingLevel = "critical"  // 严重错误消息
	LogLevelAlert     LoggingLevel = "alert"     // 需要立即采取行动
	LogLevelEmergency LoggingLevel = "emergency" // 系统不可用
)

// SetLoggingLevelParams logging/setLevel 请求参数
type SetLoggingLevelParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	// 客户端希望从服务器接收的日志级别
	// 服务器应该发送此级别及更高级别(即更严重)的所有日志到客户端
	Level LoggingLevel `json:"level"`
}

// LoggingMessageParams notifications/message 通知参数
type LoggingMessageParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	// 要记录的数据,例如字符串消息或对象
	// 允许任何 JSON 可序列化类型
	Data any `json:"data"`
	// 此日志消息的严重性级别
	Level LoggingLevel `json:"level"`
	// 发出此消息的日志记录器的可选名称
	Logger string `json:"logger,omitempty"`
}

