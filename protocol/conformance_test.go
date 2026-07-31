package protocol

import (
	"encoding/json"
	"testing"
)

// Fixtures in this file are verbatim wire examples from the 2026-07-28
// specification pages; each test asserts this SDK produces or consumes them
// exactly.

func TestDiscoverResponseFixture(t *testing.T) {
	res := &DiscoverResult{
		SupportedVersions: []string{"2026-07-28"},
		Capabilities: ServerCapabilities{
			Tools:     &ToolsCapability{},
			Resources: &ResourcesCapability{},
		},
		Instructions: "This server provides weather and resource utilities.",
		CacheControl: CacheControl{TTLMs: 3600000, CacheScope: CacheScopePublic},
	}
	res.Meta.ServerInfo = &Implementation{Name: "ExampleServer", Version: "1.0.0"}

	msg, err := NewResponse(StringID("discover-1"), res)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{
		"jsonrpc": "2.0", "id": "discover-1",
		"result": {
			"resultType": "complete",
			"supportedVersions": ["2026-07-28"],
			"capabilities": { "tools": {}, "resources": {} },
			"_meta": { "io.modelcontextprotocol/serverInfo": { "name": "ExampleServer", "version": "1.0.0" } },
			"instructions": "This server provides weather and resource utilities.",
			"ttlMs": 3600000,
			"cacheScope": "public"
		}
	}`))
}

func TestSubscriptionAckFixture(t *testing.T) {
	ack := AckParams{
		Meta: NotificationMeta{SubscriptionID: IntID(1)},
		Notifications: SubscriptionFilter{
			ToolsListChanged:      true,
			ResourceSubscriptions: []string{"file:///project/config.json"},
		},
	}
	msg, err := NewNotification(NotificationSubscriptionsAcknowledged, ack)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{
		"jsonrpc": "2.0", "method": "notifications/subscriptions/acknowledged",
		"params": { "_meta": { "io.modelcontextprotocol/subscriptionId": 1 },
			"notifications": { "toolsListChanged": true,
				"resourceSubscriptions": ["file:///project/config.json"] } }
	}`))
}

func TestSubscriptionFilterExtensionFieldsRoundTrip(t *testing.T) {
	wire := []byte(`{"toolsListChanged":true,"taskIds":["t-1","t-2"]}`)
	var f SubscriptionFilter
	if err := json.Unmarshal(wire, &f); err != nil {
		t.Fatal(err)
	}
	if !f.ToolsListChanged || string(f.Extra["taskIds"]) != `["t-1","t-2"]` {
		t.Fatalf("filter decode mismatch: %+v", f)
	}
	out, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, out, wire)
}

func TestResourceNotFoundFixture(t *testing.T) {
	msg := NewErrorResponse(IntID(5), ResourceNotFoundError("file:///nonexistent.txt"))
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	// Verbatim from server/resources.md: -32602, never -32002.
	jsonEqual(t, raw, []byte(`{
		"jsonrpc": "2.0", "id": 5,
		"error": { "code": -32602, "message": "Resource not found",
			"data": { "uri": "file:///nonexistent.txt" } }
	}`))
}

func TestResourceUpdatedNotificationFixture(t *testing.T) {
	msg, err := NewNotification(NotificationResourcesUpdated, ResourceUpdatedParams{
		Meta: NotificationMeta{SubscriptionID: IntID(1)},
		URI:  "file:///project/config.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{
		"jsonrpc": "2.0", "method": "notifications/resources/updated",
		"params": { "_meta": { "io.modelcontextprotocol/subscriptionId": 1 },
			"uri": "file:///project/config.json" }
	}`))
}

func TestListenResultCarriesSubscriptionID(t *testing.T) {
	res := &ListenResult{}
	res.Meta.SubscriptionID = IntID(1)
	raw, err := MarshalResult(res)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{
		"resultType": "complete",
		"_meta": { "io.modelcontextprotocol/subscriptionId": 1 }
	}`))
}

func TestCacheableResultsCarryRequiredFields(t *testing.T) {
	// ttlMs must appear even when 0; cacheScope is required.
	res := &ListToolsResult{Tools: []*Tool{}, CacheControl: CacheControl{CacheScope: CacheScopePrivate}}
	raw, err := MarshalResult(res)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{
		"resultType": "complete", "tools": [], "ttlMs": 0, "cacheScope": "private"
	}`))
}

func TestNonCacheableResultsCarryNoCacheFields(t *testing.T) {
	raw, err := MarshalResult(NewToolResultText("x"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["ttlMs"]; ok {
		t.Fatal("CallToolResult must not carry ttlMs")
	}
	if _, ok := m["cacheScope"]; ok {
		t.Fatal("CallToolResult must not carry cacheScope")
	}
}
