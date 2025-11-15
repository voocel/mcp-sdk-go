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

// logLevelSeverity 返回日志级别的严重性数值,数值越大越严重
func logLevelSeverity(level LoggingLevel) int {
	switch level {
	case LogLevelDebug:
		return 0
	case LogLevelInfo:
		return 1
	case LogLevelNotice:
		return 2
	case LogLevelWarning:
		return 3
	case LogLevelError:
		return 4
	case LogLevelCritical:
		return 5
	case LogLevelAlert:
		return 6
	case LogLevelEmergency:
		return 7
	default:
		return -1 // 未知级别
	}
}

// ShouldLog 判断是否应该发送指定级别的日志
// messageLevel: 要发送的消息级别
// minLevel: 客户端设置的最低级别
// 返回 true 表示应该发送(messageLevel >= minLevel)
func ShouldLog(messageLevel, minLevel LoggingLevel) bool {
	msgSeverity := logLevelSeverity(messageLevel)
	minSeverity := logLevelSeverity(minLevel)

	// 如果级别未知,默认不过滤
	if msgSeverity == -1 || minSeverity == -1 {
		return true
	}

	return msgSeverity >= minSeverity
}

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

