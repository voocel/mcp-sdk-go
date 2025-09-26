package protocol

// Root 根目录定义
type Root struct {
	URI  string `json:"uri"`            // 根目录URI，必须是 file:// 协议
	Name string `json:"name,omitempty"` // 可选的人类可读名称
}

// ListRootsRequest roots/list 请求
type ListRootsRequest struct {
	// 根据MCP协议，roots/list请求没有参数
}

// ListRootsParams 列出根目录的参数类型
type ListRootsParams struct {
	// 根据MCP协议，roots/list请求没有参数
}

// ListRootsResult roots/list 响应
type ListRootsResult struct {
	Roots []Root `json:"roots"`
}

// RootsListChangedNotification 根目录列表变更通知
type RootsListChangedNotification struct{}

// NewRoot 创建新的根目录定义
func NewRoot(uri, name string) Root {
	return Root{
		URI:  uri,
		Name: name,
	}
}

// NewListRootsResult 创建根目录列表结果
func NewListRootsResult(roots ...Root) *ListRootsResult {
	return &ListRootsResult{
		Roots: roots,
	}
}

// NewRootsListChangedNotification 创建根目录列表变更通知
func NewRootsListChangedNotification() RootsListChangedNotification {
	return RootsListChangedNotification{}
}
