package protocol

// PingParams ping 请求参数 (空参数)
type PingParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

// ProgressNotificationParams 进度通知参数
type ProgressNotificationParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	// 进度令牌,用于关联此通知与正在进行的请求
	ProgressToken any `json:"progressToken"`
	// 当前进度值,每次进度更新时应该增加
	Progress float64 `json:"progress"`
	// 总进度值(如果已知),0 表示未知
	Total float64 `json:"total,omitempty"`
	// 可选的进度描述消息
	Message string `json:"message,omitempty"`
}

// CancelledNotificationParams 取消请求通知参数
type CancelledNotificationParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	// 要取消的请求 ID
	RequestID any `json:"requestId"`
	// 可选的取消原因描述
	Reason string `json:"reason,omitempty"`
}

// ResourceTemplateListChangedParams 资源模板列表变更通知参数
type ResourceTemplateListChangedParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

// RootsListChangedParams 根目录列表变更通知参数
type RootsListChangedParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
}

// ResourceUpdatedParams 资源更新通知参数
type ResourceUpdatedParams struct {
	Meta map[string]any `json:"_meta,omitempty"`
	// 更新的资源 URI
	URI string `json:"uri"`
}
