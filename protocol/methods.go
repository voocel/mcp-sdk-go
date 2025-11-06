package protocol

const (
	MethodInitialize = "initialize"
	MethodPing       = "ping"

	MethodToolsList = "tools/list"
	MethodToolsCall = "tools/call"

	MethodResourcesList          = "resources/list"
	MethodResourcesRead          = "resources/read"
	MethodResourcesTemplatesList = "resources/templates/list"
	MethodResourcesSubscribe     = "resources/subscribe"
	MethodResourcesUnsubscribe   = "resources/unsubscribe"

	MethodPromptsList = "prompts/list"
	MethodPromptsGet  = "prompts/get"

	MethodCompletionComplete = "completion/complete"

	MethodRootsList = "roots/list"

	MethodSamplingCreateMessage = "sampling/createMessage"

	MethodElicitationCreate = "elicitation/create"

	MethodLoggingSetLevel = "logging/setLevel"
)

const (
	NotificationInitialized = "notifications/initialized"

	NotificationToolsListChanged = "notifications/tools/list_changed"

	NotificationResourcesListChanged          = "notifications/resources/list_changed"
	NotificationResourcesUpdated              = "notifications/resources/updated"
	NotificationResourcesTemplatesListChanged = "notifications/resources/templates/list_changed"

	NotificationPromptsListChanged = "notifications/prompts/list_changed"

	NotificationRootsListChanged = "notifications/roots/list_changed"

	NotificationProgress  = "notifications/progress"
	NotificationCancelled = "notifications/cancelled"

	NotificationLoggingMessage = "notifications/message"
)

