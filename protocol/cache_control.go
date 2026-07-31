package protocol

type CacheScope string

const (
	CacheScopePublic  CacheScope = "public"
	CacheScopePrivate CacheScope = "private"
)

// CacheControl is embedded in the six cacheable results (server/discover,
// tools/list, prompts/list, resources/list, resources/templates/list,
// resources/read). Both fields are required on the wire: ttlMs deliberately
// has no omitempty because 0 is meaningful ("immediately stale").
type CacheControl struct {
	TTLMs      int64      `json:"ttlMs"`
	CacheScope CacheScope `json:"cacheScope"`
}

// Cacheable is satisfied by every result embedding CacheControl; the server
// uses it to fill defaults centrally.
type Cacheable interface {
	CacheControlRef() *CacheControl
}

func (c *CacheControl) CacheControlRef() *CacheControl { return c }
