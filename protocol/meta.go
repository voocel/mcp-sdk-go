package protocol

import (
	"encoding/json"
	"maps"
)

// Version is the only protocol revision this SDK speaks.
const Version = "2026-07-28"

// Reserved _meta keys.
const (
	MetaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	MetaClientInfo         = "io.modelcontextprotocol/clientInfo"
	MetaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	MetaServerInfo         = "io.modelcontextprotocol/serverInfo"
	MetaSubscriptionID     = "io.modelcontextprotocol/subscriptionId"
	metaProgressToken      = "progressToken"
)

// Implementation identifies a client or server. Self-reported and unverified;
// never use it for security decisions.
type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// RequestMeta is the typed view of params._meta on every request. The
// protocol version and client capabilities are required on the wire; the
// stateless server reads all per-request context from here.
type RequestMeta struct {
	ProgressToken      ProgressToken
	ProtocolVersion    string
	ClientInfo         *Implementation
	ClientCapabilities ClientCapabilities

	// Extra holds non-reserved keys (traceparent, tracestate, baggage,
	// vendor keys, ...) and round-trips them verbatim.
	Extra map[string]json.RawMessage

	capsSet bool
}

func (m RequestMeta) IsZero() bool {
	return m.ProtocolVersion == "" && !m.capsSet && m.ClientInfo == nil &&
		m.ProgressToken.IsZero() && len(m.Extra) == 0
}

func (m RequestMeta) MarshalJSON() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(m.Extra)+4)
	maps.Copy(out, m.Extra)
	if !m.ProgressToken.IsZero() {
		raw, err := json.Marshal(m.ProgressToken)
		if err != nil {
			return nil, err
		}
		out[metaProgressToken] = raw
	}
	if m.ProtocolVersion != "" {
		raw, err := json.Marshal(m.ProtocolVersion)
		if err != nil {
			return nil, err
		}
		out[MetaProtocolVersion] = raw
		// clientCapabilities is required alongside the version, even if empty.
		caps, err := json.Marshal(m.ClientCapabilities)
		if err != nil {
			return nil, err
		}
		out[MetaClientCapabilities] = caps
	}
	if m.ClientInfo != nil {
		raw, err := json.Marshal(m.ClientInfo)
		if err != nil {
			return nil, err
		}
		out[MetaClientInfo] = raw
	}
	return json.Marshal(out)
}

func (m *RequestMeta) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*m = RequestMeta{}
	for k, v := range raw {
		switch k {
		case metaProgressToken:
			if err := json.Unmarshal(v, &m.ProgressToken); err != nil {
				return err
			}
		case MetaProtocolVersion:
			if err := json.Unmarshal(v, &m.ProtocolVersion); err != nil {
				return err
			}
		case MetaClientInfo:
			if err := json.Unmarshal(v, &m.ClientInfo); err != nil {
				return err
			}
		case MetaClientCapabilities:
			if err := json.Unmarshal(v, &m.ClientCapabilities); err != nil {
				return err
			}
			m.capsSet = true
		default:
			if m.Extra == nil {
				m.Extra = make(map[string]json.RawMessage)
			}
			m.Extra[k] = v
		}
	}
	return nil
}

// Validate enforces the required _meta fields. A request missing any of them
// is malformed and must be rejected with -32602 (HTTP 400).
func (m *RequestMeta) Validate() *Error {
	if m.ProtocolVersion == "" {
		return Errorf(CodeInvalidParams, "missing required _meta field %q", MetaProtocolVersion).withStatus(400)
	}
	if !m.capsSet {
		return Errorf(CodeInvalidParams, "missing required _meta field %q", MetaClientCapabilities).withStatus(400)
	}
	return nil
}

// ResultMeta is the typed view of result._meta.
type ResultMeta struct {
	ServerInfo     *Implementation
	SubscriptionID RequestID
	Extra          map[string]json.RawMessage
}

func (m ResultMeta) IsZero() bool {
	return m.ServerInfo == nil && m.SubscriptionID.IsZero() && len(m.Extra) == 0
}

func (m ResultMeta) MarshalJSON() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(m.Extra)+2)
	maps.Copy(out, m.Extra)
	if m.ServerInfo != nil {
		raw, err := json.Marshal(m.ServerInfo)
		if err != nil {
			return nil, err
		}
		out[MetaServerInfo] = raw
	}
	if !m.SubscriptionID.IsZero() {
		raw, err := json.Marshal(m.SubscriptionID)
		if err != nil {
			return nil, err
		}
		out[MetaSubscriptionID] = raw
	}
	return json.Marshal(out)
}

func (m *ResultMeta) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*m = ResultMeta{}
	for k, v := range raw {
		switch k {
		case MetaServerInfo:
			if err := json.Unmarshal(v, &m.ServerInfo); err != nil {
				return err
			}
		case MetaSubscriptionID:
			if err := json.Unmarshal(v, &m.SubscriptionID); err != nil {
				return err
			}
		default:
			if m.Extra == nil {
				m.Extra = make(map[string]json.RawMessage)
			}
			m.Extra[k] = v
		}
	}
	return nil
}

// NotificationMeta is the typed view of a notification's params._meta.
// Notifications delivered on a subscriptions/listen stream must carry the
// subscription ID.
type NotificationMeta struct {
	SubscriptionID RequestID
	Extra          map[string]json.RawMessage
}

func (m NotificationMeta) IsZero() bool {
	return m.SubscriptionID.IsZero() && len(m.Extra) == 0
}

func (m NotificationMeta) MarshalJSON() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(m.Extra)+1)
	maps.Copy(out, m.Extra)
	if !m.SubscriptionID.IsZero() {
		raw, err := json.Marshal(m.SubscriptionID)
		if err != nil {
			return nil, err
		}
		out[MetaSubscriptionID] = raw
	}
	return json.Marshal(out)
}

func (m *NotificationMeta) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*m = NotificationMeta{}
	for k, v := range raw {
		switch k {
		case MetaSubscriptionID:
			if err := json.Unmarshal(v, &m.SubscriptionID); err != nil {
				return err
			}
		default:
			if m.Extra == nil {
				m.Extra = make(map[string]json.RawMessage)
			}
			m.Extra[k] = v
		}
	}
	return nil
}

// WithMeta is embedded by every result struct to carry result._meta and to
// give the server a uniform seam for stamping serverInfo.
type WithMeta struct {
	Meta ResultMeta `json:"_meta,omitzero"`
}

// ResultMetaRef exposes the embedded meta for central stamping.
func (w *WithMeta) ResultMetaRef() *ResultMeta { return &w.Meta }

// MetaCarrier is satisfied by every result struct (via WithMeta).
type MetaCarrier interface {
	ResultMetaRef() *ResultMeta
}

// EmptyResult is a bare complete result, used by acknowledgement-style
// methods (e.g. the tasks extension's tasks/update and tasks/cancel).
type EmptyResult struct{ WithMeta }

func (*EmptyResult) ResultType() string { return ResultTypeComplete }
