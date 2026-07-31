package headerbind

import (
	"testing"
)

func objSchema(props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props}
}

func TestExtract(t *testing.T) {
	bindings, err := Extract(objSchema(map[string]any{
		"region": map[string]any{"type": "string", "x-mcp-header": "Region"},
		"nested": objSchema(map[string]any{
			"count": map[string]any{"type": "integer", "x-mcp-header": "Count"},
		}),
		"plain": map[string]any{"type": "string"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 2 {
		t.Fatalf("bindings = %+v", bindings)
	}
	for _, b := range bindings {
		switch b.Header {
		case "Region":
			if len(b.Path) != 1 || b.Path[0] != "region" {
				t.Fatalf("Region path = %v", b.Path)
			}
			if b.HeaderName() != "Mcp-Param-Region" {
				t.Fatalf("HeaderName = %q", b.HeaderName())
			}
		case "Count":
			if len(b.Path) != 2 || b.Path[0] != "nested" || b.Path[1] != "count" {
				t.Fatalf("Count path = %v", b.Path)
			}
		default:
			t.Fatalf("unexpected binding %+v", b)
		}
	}
}

func TestExtractRejections(t *testing.T) {
	cases := map[string]map[string]any{
		"number type": objSchema(map[string]any{
			"v": map[string]any{"type": "number", "x-mcp-header": "V"},
		}),
		"object type": objSchema(map[string]any{
			"v": map[string]any{"type": "object", "x-mcp-header": "V"},
		}),
		"empty name": objSchema(map[string]any{
			"v": map[string]any{"type": "string", "x-mcp-header": ""},
		}),
		"invalid char": objSchema(map[string]any{
			"v": map[string]any{"type": "string", "x-mcp-header": "Bad Name"},
		}),
		"non-string annotation": objSchema(map[string]any{
			"v": map[string]any{"type": "string", "x-mcp-header": 42},
		}),
		"case-insensitive duplicate": objSchema(map[string]any{
			"a": map[string]any{"type": "string", "x-mcp-header": "Region"},
			"b": map[string]any{"type": "string", "x-mcp-header": "region"},
		}),
	}
	for name, schema := range cases {
		if _, err := Extract(schema); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestExtractRejectsNonReachableAnnotations(t *testing.T) {
	// Spec: "An x-mcp-header annotation anywhere else makes the annotation —
	// and thus the tool definition — invalid." Annotations inside oneOf/items
	// are unreachable through a pure properties chain and must invalidate the
	// tool, not be silently ignored.
	_, err := Extract(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"choice": map[string]any{
				"oneOf": []any{
					objSchema(map[string]any{
						"hidden": map[string]any{"type": "string", "x-mcp-header": "Hidden"},
					}),
				},
			},
		},
	})
	if err == nil {
		t.Fatal("annotation in a non-reachable position must invalidate the tool")
	}
}

// Encoding examples verbatim from the streamable-http spec table.
func TestEncodeSpecTable(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"us-west1", "us-west1"},
		{"Hello, 世界", "=?base64?SGVsbG8sIOS4lueVjA==?="},
		{" padded ", "=?base64?IHBhZGRlZCA=?="},
		{"line1\nline2", "=?base64?bGluZTEKbGluZTI=?="},
		{"=?base64?literal?=", "=?base64?PT9iYXNlNjQ/bGl0ZXJhbD89?="},
	} {
		if got := Encode(tc.in); got != tc.want {
			t.Errorf("Encode(%q) = %q, want %q", tc.in, got, tc.want)
		}
		back, err := Decode(Encode(tc.in))
		if err != nil || back != tc.in {
			t.Errorf("Decode(Encode(%q)) = %q, %v", tc.in, back, err)
		}
	}
}

func TestFormat(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{"s", "s"},
		{true, "true"},
		{false, "false"},
		{float64(42), "42"},
		{int64(-1), "-1"},
	} {
		got, err := Format(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Format(%v) = %q, %v", tc.in, got, err)
		}
	}
	for _, bad := range []any{42.5, float64(1 << 60), []string{"x"}, nil} {
		if _, err := Format(bad); err == nil {
			t.Errorf("Format(%v): expected error", bad)
		}
	}
}

func TestLookup(t *testing.T) {
	args := map[string]any{
		"region": "us",
		"nested": map[string]any{"count": float64(3)},
		"null":   nil,
	}
	if v, ok := Lookup(args, []string{"region"}); !ok || v != "us" {
		t.Fatalf("region = %v, %v", v, ok)
	}
	if v, ok := Lookup(args, []string{"nested", "count"}); !ok || v != float64(3) {
		t.Fatalf("count = %v, %v", v, ok)
	}
	if _, ok := Lookup(args, []string{"missing"}); ok {
		t.Fatal("missing should not be found")
	}
	if _, ok := Lookup(args, []string{"null"}); ok {
		t.Fatal("nil value must be treated as absent")
	}
}
