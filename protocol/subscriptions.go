package protocol

import (
	"encoding/json"
	"maps"
)

// SubscriptionFilter selects which notification types a subscriptions/listen
// stream carries. Omitted fields mean "not subscribed"; the server must not
// send types the client did not request. Extra carries extension filter
// fields (e.g. the tasks extension's taskIds).
type SubscriptionFilter struct {
	ToolsListChanged      bool
	PromptsListChanged    bool
	ResourcesListChanged  bool
	ResourceSubscriptions []string
	Extra                 map[string]json.RawMessage
}

func (f SubscriptionFilter) IsZero() bool {
	return !f.ToolsListChanged && !f.PromptsListChanged && !f.ResourcesListChanged &&
		len(f.ResourceSubscriptions) == 0 && len(f.Extra) == 0
}

func (f SubscriptionFilter) MarshalJSON() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(f.Extra)+4)
	maps.Copy(out, f.Extra)
	set := func(key string, v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out[key] = raw
		return nil
	}
	if f.ToolsListChanged {
		if err := set("toolsListChanged", true); err != nil {
			return nil, err
		}
	}
	if f.PromptsListChanged {
		if err := set("promptsListChanged", true); err != nil {
			return nil, err
		}
	}
	if f.ResourcesListChanged {
		if err := set("resourcesListChanged", true); err != nil {
			return nil, err
		}
	}
	if len(f.ResourceSubscriptions) > 0 {
		if err := set("resourceSubscriptions", f.ResourceSubscriptions); err != nil {
			return nil, err
		}
	}
	return json.Marshal(out)
}

func (f *SubscriptionFilter) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*f = SubscriptionFilter{}
	for k, v := range raw {
		var err error
		switch k {
		case "toolsListChanged":
			err = json.Unmarshal(v, &f.ToolsListChanged)
		case "promptsListChanged":
			err = json.Unmarshal(v, &f.PromptsListChanged)
		case "resourcesListChanged":
			err = json.Unmarshal(v, &f.ResourcesListChanged)
		case "resourceSubscriptions":
			err = json.Unmarshal(v, &f.ResourceSubscriptions)
		default:
			if f.Extra == nil {
				f.Extra = make(map[string]json.RawMessage)
			}
			f.Extra[k] = v
		}
		if err != nil {
			return err
		}
	}
	return nil
}

type ListenParams struct {
	Meta          RequestMeta        `json:"_meta"`
	Notifications SubscriptionFilter `json:"notifications"`
}

// AckParams is the payload of notifications/subscriptions/acknowledged — the
// first message on every subscription stream, carrying the honored subset.
type AckParams struct {
	Meta          NotificationMeta   `json:"_meta,omitzero"`
	Notifications SubscriptionFilter `json:"notifications"`
}

// ListenResult is the empty "complete" result a server sends to gracefully
// end a subscription stream. Its _meta must carry the subscription ID.
type ListenResult struct {
	WithMeta
}

func (*ListenResult) ResultType() string { return ResultTypeComplete }
