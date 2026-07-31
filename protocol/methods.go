package protocol

// The complete 2026-07-28 method set. sampling/createMessage and roots/list
// exist on the wire only as MRTR InputRequest payload shapes and are not
// modeled by this SDK (both features are deprecated).
const (
	MethodDiscover               = "server/discover"
	MethodToolsList              = "tools/list"
	MethodToolsCall              = "tools/call"
	MethodPromptsList            = "prompts/list"
	MethodPromptsGet             = "prompts/get"
	MethodResourcesList          = "resources/list"
	MethodResourcesTemplatesList = "resources/templates/list"
	MethodResourcesRead          = "resources/read"
	MethodCompletionComplete     = "completion/complete"
	MethodSubscriptionsListen    = "subscriptions/listen"

	// MethodElicitationCreate appears only inside MRTR inputRequests, never
	// as a dispatched RPC.
	MethodElicitationCreate = "elicitation/create"

	NotificationProgress                  = "notifications/progress"
	NotificationCancelled                 = "notifications/cancelled"
	NotificationSubscriptionsAcknowledged = "notifications/subscriptions/acknowledged"
	NotificationToolsListChanged          = "notifications/tools/list_changed"
	NotificationPromptsListChanged        = "notifications/prompts/list_changed"
	NotificationResourcesListChanged      = "notifications/resources/list_changed"
	NotificationResourcesUpdated          = "notifications/resources/updated"
)

// ProgressParams is the payload of notifications/progress. Progress
// notifications flow only on the response stream of the request that supplied
// the progressToken, never on a subscriptions/listen stream.
type ProgressParams struct {
	Meta          NotificationMeta `json:"_meta,omitzero"`
	ProgressToken ProgressToken    `json:"progressToken"`
	Progress      float64          `json:"progress"`
	Total         float64          `json:"total,omitempty"`
	Message       string           `json:"message,omitempty"`
}

// CancelledParams is the payload of notifications/cancelled. On stdio the
// client sends it to cancel an in-flight request; servers send it only to tear
// down a subscriptions/listen stream.
type CancelledParams struct {
	Meta      NotificationMeta `json:"_meta,omitzero"`
	RequestID RequestID        `json:"requestId"`
	Reason    string           `json:"reason,omitempty"`
}
