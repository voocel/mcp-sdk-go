package protocol

// PingParams ping request parameters (empty parameters)
type PingParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

// ProgressNotificationParams progress notification parameters
type ProgressNotificationParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	// Progress token to associate this notification with an ongoing request
	ProgressToken any `json:"progressToken"`
	// Current progress value, should increase with each progress update
	Progress float64 `json:"progress"`
	// Total progress value (if known), 0 indicates unknown
	Total float64 `json:"total,omitempty"`
	// Optional progress description message
	Message string `json:"message,omitempty"`
}

// CancelledNotificationParams cancellation notification parameters
type CancelledNotificationParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	// Request ID to cancel
	RequestID any `json:"requestId"`
	// Optional cancellation reason description
	Reason string `json:"reason,omitempty"`
}

// RootsListChangedParams roots list change notification parameters
type RootsListChangedParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

// ResourceUpdatedNotificationParams resource update notification parameters
type ResourceUpdatedNotificationParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	// Updated resource URI
	URI string `json:"uri"`
}
