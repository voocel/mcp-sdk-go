package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// jsonEqual compares two JSON documents structurally.
func jsonEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got is not valid JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("want is not valid JSON: %v\n%s", err, want)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("JSON mismatch\ngot:  %s\nwant: %s", got, want)
	}
}

func TestRequestIDRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		wire string
		id   RequestID
	}{
		{`"abc"`, StringID("abc")},
		{`42`, IntID(42)},
		{`-7`, IntID(-7)},
	} {
		var id RequestID
		if err := json.Unmarshal([]byte(tc.wire), &id); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.wire, err)
		}
		if id != tc.id {
			t.Fatalf("unmarshal %s: got %#v want %#v", tc.wire, id, tc.id)
		}
		out, err := json.Marshal(id)
		if err != nil {
			t.Fatalf("marshal %#v: %v", id, err)
		}
		if string(out) != tc.wire {
			t.Fatalf("marshal: got %s want %s", out, tc.wire)
		}
	}
}

func TestRequestIDRejectsInvalid(t *testing.T) {
	for _, wire := range []string{`1.5`, `true`, `{}`, `[]`} {
		var id RequestID
		if err := json.Unmarshal([]byte(wire), &id); err == nil {
			t.Errorf("unmarshal %s: expected error, got %#v", wire, id)
		}
	}
}

func TestRequestIDNull(t *testing.T) {
	var id RequestID
	if err := json.Unmarshal([]byte(`null`), &id); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if !id.IsZero() {
		t.Fatalf("null must decode to the zero id, got %#v", id)
	}
}

func TestErrorResponseIDNull(t *testing.T) {
	// 2026-07-28: ids MUST NOT be null; an error response whose request id
	// could not be read omits the id field. Incoming id:null (sloppy peers)
	// still decodes leniently to the zero id.
	out, err := json.Marshal(NewErrorResponse(RequestID{}, Errorf(CodeParseError, "bad")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `"id"`) {
		t.Fatalf("id must be omitted, not emitted: %s", out)
	}
	out = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"bad"}}`)
	var m Message
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m.Kind() != KindError || !m.ID.IsZero() {
		t.Fatalf("round-trip: kind=%v id=%#v", m.Kind(), m.ID)
	}

	// Notifications and requests are unaffected: no id vs. a real id.
	note, _ := NewNotification("notifications/progress", nil)
	if raw, _ := json.Marshal(note); strings.Contains(string(raw), `"id"`) {
		t.Fatalf("notification must omit id: %s", raw)
	}
	req, _ := NewRequest(IntID(7), "tools/list", nil)
	if raw, _ := json.Marshal(req); !strings.Contains(string(raw), `"id":7`) {
		t.Fatalf("request id lost: %s", raw)
	}
}

func TestRequestIDAsMapKey(t *testing.T) {
	m := map[RequestID]string{StringID("a"): "x", IntID(1): "y"}
	if m[StringID("a")] != "x" || m[IntID(1)] != "y" {
		t.Fatal("RequestID map lookups failed")
	}
}

func TestMessageKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire string
		kind Kind
	}{
		{"request", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, KindRequest},
		{"notification", `{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`, KindNotification},
		{"response", `{"jsonrpc":"2.0","id":1,"result":{"resultType":"complete"}}`, KindResponse},
		{"error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"nope"}}`, KindError},
		{"error without id", `{"jsonrpc":"2.0","error":{"code":-32600,"message":"bad"}}`, KindError},
		{"wrong version", `{"jsonrpc":"1.0","id":1,"method":"x"}`, KindInvalid},
		{"response without id", `{"jsonrpc":"2.0","result":{}}`, KindInvalid},
		{"nothing", `{"jsonrpc":"2.0"}`, KindInvalid},
	} {
		var m Message
		if err := json.Unmarshal([]byte(tc.wire), &m); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := m.Kind(); got != tc.kind {
			t.Errorf("%s: Kind() = %d, want %d", tc.name, got, tc.kind)
		}
	}
}

type emptyResult struct{ WithMeta }

func (*emptyResult) ResultType() string { return ResultTypeComplete }

func TestMarshalResultSplicesResultType(t *testing.T) {
	raw, err := MarshalResult(&emptyResult{})
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{"resultType":"complete"}`))

	res := NewToolResultText("hi")
	raw, err = MarshalResult(res)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{"resultType":"complete","content":[{"type":"text","text":"hi"}]}`))
}

func TestPeekResultType(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{`{"resultType":"input_required"}`, ResultTypeInputRequired},
		{`{"resultType":"task"}`, "task"},
		{`{"content":[]}`, ResultTypeComplete}, // absent => complete per spec
	} {
		got, err := PeekResultType([]byte(tc.raw))
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("PeekResultType(%s) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestErrorRoundTrip(t *testing.T) {
	e := UnsupportedVersionError("1900-01-01", []string{"2026-07-28"})
	msg := NewErrorResponse(IntID(1), e)
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	// Verbatim shape from basic/versioning.md.
	jsonEqual(t, raw, []byte(`{
		"jsonrpc": "2.0", "id": 1,
		"error": {
			"code": -32022,
			"message": "Unsupported protocol version",
			"data": { "supported": ["2026-07-28"], "requested": "1900-01-01" }
		}
	}`))

	var back Message
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	data, ok := back.Error.UnsupportedVersion()
	if !ok || data.Requested != "1900-01-01" || data.Supported[0] != "2026-07-28" {
		t.Fatalf("data payload lost in round trip: %+v ok=%v", data, ok)
	}
}

func TestErrorHTTPStatus(t *testing.T) {
	for _, tc := range []struct {
		err  *Error
		want int
	}{
		{MethodNotFoundError("x"), 404},
		{HeaderMismatchError("x"), 400},
		{UnsupportedVersionError("v", nil), 400},
		{MissingCapabilityError(ClientCapabilities{}), 400},
		{Errorf(CodeInvalidParams, "unknown tool"), 200},
		{Errorf(CodeInternal, "boom"), 200},
	} {
		if got := tc.err.HTTPStatus(); got != tc.want {
			t.Errorf("HTTPStatus(%d %q) = %d, want %d", tc.err.Code, tc.err.Message, got, tc.want)
		}
	}
}
