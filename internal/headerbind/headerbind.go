// Package headerbind implements the x-mcp-header annotation of the Streamable
// HTTP transport: extracting bindings from tool input schemas, validating the
// spec's constraints, and encoding/decoding header values with the
// =?base64?...?= sentinel.
//
// It is shared by the server (which fails loudly at registration on invalid
// bindings) and the client (which must exclude invalid tools from tools/list).
package headerbind

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	// AnnotationKey marks a schema property for header transmission.
	AnnotationKey = "x-mcp-header"
	// Prefix is prepended to the annotated name to form the HTTP header.
	Prefix = "Mcp-Param-"

	sentinelPrefix = "=?base64?"
	sentinelSuffix = "?="

	// MaxSafeInteger bounds integer header values (±(2^53−1)).
	MaxSafeInteger = 1<<53 - 1
)

// Binding is one annotated property: a chain of properties keys from the
// schema root to a primitive property, and the header name it maps to.
type Binding struct {
	Path   []string // property names from the root
	Header string   // annotated name, without the Mcp-Param- prefix
	Type   string   // "string" | "integer" | "boolean"
}

// HeaderName returns the full HTTP header name.
func (b Binding) HeaderName() string { return Prefix + b.Header }

// Extract walks a tool input schema and returns its header bindings. It
// enforces every constraint the spec places on x-mcp-header: non-empty tchar
// names, primitive property types (number is not permitted), case-insensitive
// uniqueness, and static reachability — an annotation anywhere the properties
// walk cannot reach (items, composition keywords, $ref targets, the schema
// root) makes the whole tool definition invalid, per spec, rather than being
// silently ignored.
func Extract(schema map[string]any) ([]Binding, error) {
	var out []Binding
	seen := make(map[string]string) // lowercase name -> original
	if err := walk(schema, nil, seen, &out); err != nil {
		return nil, err
	}
	if n := countAnnotations(schema); n != len(out) {
		return nil, fmt.Errorf("%d %s annotation(s) in positions not statically reachable from the schema root",
			n-len(out), AnnotationKey)
	}
	return out, nil
}

// nameKeyedKeywords are the schema keywords whose children are keyed by
// author-chosen names: under them, "x-mcp-header" is a property name rather
// than an annotation.
var nameKeyedKeywords = map[string]bool{
	"properties":        true,
	"patternProperties": true,
	"dependentSchemas":  true,
	"$defs":             true,
	"definitions":       true,
}

// countAnnotations counts AnnotationKey occurrences anywhere in the schema
// tree, so Extract can detect annotations its properties-only walk missed.
func countAnnotations(v any) int {
	n := 0
	switch t := v.(type) {
	case map[string]any:
		for k, sub := range t {
			if k == AnnotationKey {
				// The value is a header name, not a subschema.
				n++
				continue
			}
			if nameKeyedKeywords[k] {
				if named, ok := sub.(map[string]any); ok {
					for _, child := range named {
						n += countAnnotations(child)
					}
					continue
				}
			}
			n += countAnnotations(sub)
		}
	case []any:
		for _, sub := range t {
			n += countAnnotations(sub)
		}
	}
	return n
}

func walk(schema map[string]any, path []string, seen map[string]string, out *[]Binding) error {
	props, _ := schema["properties"].(map[string]any)
	for name, sub := range props {
		subSchema, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		propPath := append(append([]string{}, path...), name)
		if ann, present := subSchema[AnnotationKey]; present {
			header, ok := ann.(string)
			if !ok {
				return fmt.Errorf("property %q: %s must be a string", strings.Join(propPath, "."), AnnotationKey)
			}
			if err := validateName(header); err != nil {
				return fmt.Errorf("property %q: %w", strings.Join(propPath, "."), err)
			}
			typ, _ := subSchema["type"].(string)
			switch typ {
			case "string", "integer", "boolean":
			default:
				return fmt.Errorf("property %q: %s requires a primitive type (string, integer or boolean), got %q",
					strings.Join(propPath, "."), AnnotationKey, typ)
			}
			lower := strings.ToLower(header)
			if prev, dup := seen[lower]; dup {
				return fmt.Errorf("property %q: header name %q conflicts case-insensitively with %q",
					strings.Join(propPath, "."), header, prev)
			}
			seen[lower] = header
			*out = append(*out, Binding{Path: propPath, Header: header, Type: typ})
		}
		if err := walk(subSchema, propPath, seen, out); err != nil {
			return err
		}
	}
	return nil
}

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%s must not be empty", AnnotationKey)
	}
	for i := 0; i < len(name); i++ {
		if !isTchar(name[i]) {
			return fmt.Errorf("%s value %q contains invalid character %q", AnnotationKey, name, name[i])
		}
	}
	return nil
}

// isTchar reports whether c is a token character per RFC 9110 §5.6.2.
func isTchar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

// Lookup resolves a binding path in a tool's arguments. The second return is
// false when the value is absent or nil (the header must then be omitted).
func Lookup(args map[string]any, path []string) (any, bool) {
	cur := any(args)
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	if cur == nil {
		return nil, false
	}
	return cur, true
}

// Format converts a primitive argument value to its header string form:
// strings as-is, integers as decimal, booleans lowercase.
func Format(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case float64:
		if x != math.Trunc(x) || math.Abs(x) > MaxSafeInteger {
			return "", fmt.Errorf("integer header value %v out of range or not integral", x)
		}
		return strconv.FormatInt(int64(x), 10), nil
	case int:
		return formatInt(int64(x))
	case int64:
		return formatInt(x)
	case json.Number:
		i, err := x.Int64()
		if err != nil {
			return "", fmt.Errorf("integer header value %v not integral", x)
		}
		return formatInt(i)
	}
	return "", fmt.Errorf("unsupported header value type %T", v)
}

func formatInt(i int64) (string, error) {
	if i > MaxSafeInteger || i < -MaxSafeInteger {
		return "", fmt.Errorf("integer header value %d out of ±(2^53−1) range", i)
	}
	return strconv.FormatInt(i, 10), nil
}

// Encode applies the =?base64?...?= sentinel when the value is not safely
// ASCII-representable or itself matches the sentinel pattern.
func Encode(s string) string {
	if !needsEncoding(s) {
		return s
	}
	return sentinelPrefix + base64.StdEncoding.EncodeToString([]byte(s)) + sentinelSuffix
}

// Decode reverses Encode. Values without the sentinel pass through unchanged.
func Decode(s string) (string, error) {
	if !strings.HasPrefix(s, sentinelPrefix) || !strings.HasSuffix(s, sentinelSuffix) {
		return s, nil
	}
	inner := s[len(sentinelPrefix) : len(s)-len(sentinelSuffix)]
	raw, err := base64.StdEncoding.DecodeString(inner)
	if err != nil {
		return "", fmt.Errorf("invalid base64 header value: %w", err)
	}
	return string(raw), nil
}

func needsEncoding(s string) bool {
	if s == "" {
		return false
	}
	if leadingOrTrailingWhitespace(s) {
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7E {
			return true
		}
	}
	// A plain-ASCII value that looks like the sentinel must itself be encoded.
	return strings.HasPrefix(s, sentinelPrefix) && strings.HasSuffix(s, sentinelSuffix)
}

func leadingOrTrailingWhitespace(s string) bool {
	first, last := s[0], s[len(s)-1]
	return first == ' ' || first == '\t' || last == ' ' || last == '\t'
}
