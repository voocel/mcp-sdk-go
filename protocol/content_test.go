package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestContentRoundTrip(t *testing.T) {
	priority := 0.8
	for _, tc := range []struct {
		name    string
		content Content
		wire    string
	}{
		{"text", NewTextContent("hello"), `{"type":"text","text":"hello"}`},
		{"image", NewImageContent("aGk=", "image/png"), `{"type":"image","data":"aGk=","mimeType":"image/png"}`},
		{"audio", NewAudioContent("aGk=", "audio/wav"), `{"type":"audio","data":"aGk=","mimeType":"audio/wav"}`},
		{"resource_link", NewResourceLink("file:///a.txt", "a"), `{"type":"resource_link","uri":"file:///a.txt","name":"a"}`},
		{"resource", NewEmbeddedResource(NewTextResourceContents("info://x", "hi")),
			`{"type":"resource","resource":{"uri":"info://x","text":"hi"}}`},
		{"annotated", TextContent{Text: "t", Annotations: &Annotations{Audience: []Role{RoleUser}, Priority: &priority}},
			`{"type":"text","text":"t","annotations":{"audience":["user"],"priority":0.8}}`},
	} {
		raw, err := json.Marshal(tc.content)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		jsonEqual(t, raw, []byte(tc.wire))

		back, err := UnmarshalContent(raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !reflect.DeepEqual(back, tc.content) {
			t.Errorf("%s: round trip mismatch\ngot:  %#v\nwant: %#v", tc.name, back, tc.content)
		}
	}
}

func TestUnknownContentRoundTrip(t *testing.T) {
	wire := []byte(`{"type":"hologram","shape":"cube","meta":{"x":1}}`)
	c, err := UnmarshalContent(wire)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := c.(UnknownContent)
	if !ok {
		t.Fatalf("expected UnknownContent, got %T", c)
	}
	if u.Type != "hologram" {
		t.Fatalf("Type = %q", u.Type)
	}
	out, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, out, wire) // byte-level preservation, no corruption
}

func TestContentMissingType(t *testing.T) {
	if _, err := UnmarshalContent([]byte(`{"text":"hi"}`)); err == nil {
		t.Fatal("expected error for content without type")
	}
}

func TestContentListNilMarshalsAsEmptyArray(t *testing.T) {
	res := &CallToolResult{}
	raw, err := MarshalResult(res)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, raw, []byte(`{"resultType":"complete","content":[]}`))
}

func TestContentListDecode(t *testing.T) {
	var l ContentList
	err := json.Unmarshal([]byte(`[{"type":"text","text":"a"},{"type":"vendor_x","v":1}]`), &l)
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 {
		t.Fatalf("len = %d", len(l))
	}
	if l[0].(TextContent).Text != "a" {
		t.Fatal("first element lost")
	}
	if l[1].(UnknownContent).Type != "vendor_x" {
		t.Fatal("unknown element lost")
	}
}

func TestPromptMessageDecode(t *testing.T) {
	var m PromptMessage
	err := json.Unmarshal([]byte(`{"role":"assistant","content":{"type":"text","text":"hi"}}`), &m)
	if err != nil {
		t.Fatal(err)
	}
	if m.Role != RoleAssistant || m.Content.(TextContent).Text != "hi" {
		t.Fatalf("decode mismatch: %+v", m)
	}
}
