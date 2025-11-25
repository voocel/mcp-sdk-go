package protocol

// LoggingLevel logging level
// Maps to syslog message severity as described in RFC-5424:
// https://datatracker.ietf.org/doc/html/rfc5424#section-6.2.1
type LoggingLevel string

const (
	LogLevelDebug     LoggingLevel = "debug"     // Debug level messages
	LogLevelInfo      LoggingLevel = "info"      // Informational messages
	LogLevelNotice    LoggingLevel = "notice"    // Normal but significant messages
	LogLevelWarning   LoggingLevel = "warning"   // Warning messages
	LogLevelError     LoggingLevel = "error"     // Error messages
	LogLevelCritical  LoggingLevel = "critical"  // Critical error messages
	LogLevelAlert     LoggingLevel = "alert"     // Action must be taken immediately
	LogLevelEmergency LoggingLevel = "emergency" // System is unusable
)

// logLevelSeverity returns the severity value of the log level, higher values indicate more severe
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
		return -1 // Unknown level
	}
}

// ShouldLog determines whether a log of the specified level should be sent
// messageLevel: the level of the message to send
// minLevel: the minimum level set by the client
// Returns true if the message should be sent (messageLevel >= minLevel)
func ShouldLog(messageLevel, minLevel LoggingLevel) bool {
	msgSeverity := logLevelSeverity(messageLevel)
	minSeverity := logLevelSeverity(minLevel)

	// If level is unknown, don't filter by default
	if msgSeverity == -1 || minSeverity == -1 {
		return true
	}

	return msgSeverity >= minSeverity
}

// SetLoggingLevelParams logging/setLevel request parameters
type SetLoggingLevelParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	// The log level the client wishes to receive from the server
	// The server should send all logs at this level and higher (i.e., more severe) to the client
	Level LoggingLevel `json:"level"`
}

// LoggingMessageParams notifications/message notification parameters
type LoggingMessageParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	// Data to log, such as a string message or object
	// Allows any JSON-serializable type
	Data any `json:"data"`
	// Severity level of this log message
	Level LoggingLevel `json:"level"`
	// Optional name of the logger that emitted this message
	Logger string `json:"logger,omitempty"`
}
