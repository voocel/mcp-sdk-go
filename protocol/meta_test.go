package protocol

import (
	"encoding/json"
	"testing"
)

func TestRequestMetaMarshal(t *testing.T) {
	m := RequestMeta{
		ProtocolVersion: Version,
		ClientInfo:      &Implementation{Name: "ExampleClient", Version: "1.0.0"},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	// clientCapabilities is required alongside the version, even when empty.
	jsonEqual(t, raw, []byte(`{
		"io.modelcontextprotocol/protocolVersion": "2026-07-28",
		"io.modelcontextprotocol/clientInfo": { "name": "ExampleClient", "version": "1.0.0" },
		"io.modelcontextprotocol/clientCapabilities": {}
	}`))
}

func TestRequestMetaRoundTripExtra(t *testing.T) {
	wire := []byte(`{
		"io.modelcontextprotocol/protocolVersion": "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": {"elicitation":{"form":{}}},
		"progressToken": "tok-1",
		"traceparent": "00-abc-def-01",
		"com.example.mcp/custom": {"a":1}
	}`)
	var m RequestMeta
	if err := json.Unmarshal(wire, &m); err != nil {
		t.Fatal(err)
	}
	if m.ProtocolVersion != Version {
		t.Fatalf("version = %q", m.ProtocolVersion)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !m.ClientCapabilities.Elicitation.SupportsForm() {
		t.Fatal("elicitation form capability lost")
	}
	if m.ProgressToken != StringID("tok-1") {
		t.Fatalf("progressToken = %#v", m.ProgressToken)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, out, wire) // traceparent + vendor key round-trip verbatim
}

func TestRequestMetaValidate(t *testing.T) {
	var missing RequestMeta
	if err := missing.Validate(); err == nil || err.Code != CodeInvalidParams {
		t.Fatalf("missing version: got %v", missing.Validate())
	}
	// Version present but capabilities key absent.
	var noCaps RequestMeta
	if err := json.Unmarshal([]byte(`{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}`), &noCaps); err != nil {
		t.Fatal(err)
	}
	err := noCaps.Validate()
	if err == nil || err.Code != CodeInvalidParams || err.HTTPStatus() != 400 {
		t.Fatalf("missing caps: got %v", err)
	}
}

func TestResultMetaRoundTrip(t *testing.T) {
	m := ResultMeta{ServerInfo: &Implementation{Name: "S", Version: "1"}, SubscriptionID: IntID(1)}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{
		"io.modelcontextprotocol/serverInfo": {"name":"S","version":"1"},
		"io.modelcontextprotocol/subscriptionId": 1
	}`))
	var back ResultMeta
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.ServerInfo.Name != "S" || back.SubscriptionID != IntID(1) {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestWithMetaOmittedWhenZero(t *testing.T) {
	raw, err := MarshalResult(&GetPromptResult{Messages: []PromptMessage{}})
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{"resultType":"complete","messages":[]}`))
}
