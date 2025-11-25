package protocol

// Root root directory definition
type Root struct {
	URI  string `json:"uri"`            // Root directory URI, must use file:// protocol
	Name string `json:"name,omitempty"` // Optional human-readable name
}

// ListRootsRequest roots/list request
type ListRootsRequest struct {
	// According to MCP protocol, roots/list request has no parameters
}

// ListRootsParams parameter type for listing root directories
type ListRootsParams struct {
	// According to MCP protocol, roots/list request has no parameters
}

// ListRootsResult roots/list response
type ListRootsResult struct {
	Roots []Root `json:"roots"`
}

// RootsListChangedNotification root directory list change notification
type RootsListChangedNotification struct{}

func NewRoot(uri, name string) Root {
	return Root{
		URI:  uri,
		Name: name,
	}
}

func NewListRootsResult(roots ...Root) *ListRootsResult {
	return &ListRootsResult{
		Roots: roots,
	}
}

func NewRootsListChangedNotification() RootsListChangedNotification {
	return RootsListChangedNotification{}
}
