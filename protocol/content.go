package protocol

import (
	"encoding/json"
	"fmt"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Annotations attach audience/priority/recency hints to content and resources.
type Annotations struct {
	Audience     []Role   `json:"audience,omitempty"`
	Priority     *float64 `json:"priority,omitempty"`
	LastModified string   `json:"lastModified,omitempty"`
}

// Content is the sealed union of message content blocks. Concrete types:
// TextContent, ImageContent, AudioContent, ResourceLink, EmbeddedResource,
// and UnknownContent (which round-trips unrecognized types verbatim).
type Content interface {
	isContent()
}

type TextContent struct {
	Text        string                     `json:"text"`
	Annotations *Annotations               `json:"annotations,omitempty"`
	Meta        map[string]json.RawMessage `json:"_meta,omitempty"`
}

type ImageContent struct {
	Data        string                     `json:"data"` // base64
	MimeType    string                     `json:"mimeType"`
	Annotations *Annotations               `json:"annotations,omitempty"`
	Meta        map[string]json.RawMessage `json:"_meta,omitempty"`
}

type AudioContent struct {
	Data        string                     `json:"data"` // base64
	MimeType    string                     `json:"mimeType"`
	Annotations *Annotations               `json:"annotations,omitempty"`
	Meta        map[string]json.RawMessage `json:"_meta,omitempty"`
}

type ResourceLink struct {
	URI string `json:"uri"`
	// Name is required per BaseMetadata (no omitempty, matching Resource).
	Name        string                     `json:"name"`
	Title       string                     `json:"title,omitempty"`
	Description string                     `json:"description,omitempty"`
	MimeType    string                     `json:"mimeType,omitempty"`
	Size        int64                      `json:"size,omitempty"`
	Icons       []Icon                     `json:"icons,omitempty"`
	Annotations *Annotations               `json:"annotations,omitempty"`
	Meta        map[string]json.RawMessage `json:"_meta,omitempty"`
}

type EmbeddedResource struct {
	Resource    ResourceContents           `json:"resource"`
	Annotations *Annotations               `json:"annotations,omitempty"`
	Meta        map[string]json.RawMessage `json:"_meta,omitempty"`
}

// UnknownContent preserves a content block whose type this SDK does not
// recognize. It round-trips byte-for-byte instead of silently corrupting.
type UnknownContent struct {
	Type string
	Raw  json.RawMessage
}

func (TextContent) isContent()      {}
func (ImageContent) isContent()     {}
func (AudioContent) isContent()     {}
func (ResourceLink) isContent()     {}
func (EmbeddedResource) isContent() {}
func (UnknownContent) isContent()   {}

const (
	contentTypeText     = "text"
	contentTypeImage    = "image"
	contentTypeAudio    = "audio"
	contentTypeLink     = "resource_link"
	contentTypeResource = "resource"
)

// marshalWithType marshals v and splices the type discriminator in as the
// first key. v must be an alias type without a MarshalJSON method.
func marshalWithType(typ string, v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return insertKey(body, "type", typ)
}

func (c TextContent) MarshalJSON() ([]byte, error) {
	type alias TextContent
	return marshalWithType(contentTypeText, alias(c))
}

func (c ImageContent) MarshalJSON() ([]byte, error) {
	type alias ImageContent
	return marshalWithType(contentTypeImage, alias(c))
}

func (c AudioContent) MarshalJSON() ([]byte, error) {
	type alias AudioContent
	return marshalWithType(contentTypeAudio, alias(c))
}

func (c ResourceLink) MarshalJSON() ([]byte, error) {
	type alias ResourceLink
	return marshalWithType(contentTypeLink, alias(c))
}

func (c EmbeddedResource) MarshalJSON() ([]byte, error) {
	type alias EmbeddedResource
	return marshalWithType(contentTypeResource, alias(c))
}

func (c UnknownContent) MarshalJSON() ([]byte, error) {
	if len(c.Raw) == 0 {
		return nil, fmt.Errorf("protocol: empty UnknownContent")
	}
	return c.Raw, nil
}

// UnmarshalContent decodes a single content block by its type discriminator.
func UnmarshalContent(data []byte) (Content, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	switch probe.Type {
	case contentTypeText:
		var c TextContent
		return c, json.Unmarshal(data, &c)
	case contentTypeImage:
		var c ImageContent
		return c, json.Unmarshal(data, &c)
	case contentTypeAudio:
		var c AudioContent
		return c, json.Unmarshal(data, &c)
	case contentTypeLink:
		var c ResourceLink
		return c, json.Unmarshal(data, &c)
	case contentTypeResource:
		var c EmbeddedResource
		return c, json.Unmarshal(data, &c)
	case "":
		return nil, fmt.Errorf("protocol: content block missing type")
	default:
		raw := make(json.RawMessage, len(data))
		copy(raw, data)
		return UnknownContent{Type: probe.Type, Raw: raw}, nil
	}
}

// ContentList is the decodable form of []Content. It marshals nil as [].
type ContentList []Content

func (l ContentList) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]Content(l))
}

func (l *ContentList) UnmarshalJSON(b []byte) error {
	var raws []json.RawMessage
	if err := json.Unmarshal(b, &raws); err != nil {
		return err
	}
	out := make(ContentList, 0, len(raws))
	for _, raw := range raws {
		c, err := UnmarshalContent(raw)
		if err != nil {
			return err
		}
		out = append(out, c)
	}
	*l = out
	return nil
}

func NewTextContent(text string) TextContent { return TextContent{Text: text} }

func NewImageContent(data, mimeType string) ImageContent {
	return ImageContent{Data: data, MimeType: mimeType}
}

func NewAudioContent(data, mimeType string) AudioContent {
	return AudioContent{Data: data, MimeType: mimeType}
}

func NewResourceLink(uri, name string) ResourceLink {
	return ResourceLink{URI: uri, Name: name}
}

func NewEmbeddedResource(contents ResourceContents) EmbeddedResource {
	return EmbeddedResource{Resource: contents}
}
