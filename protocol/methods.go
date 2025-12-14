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

	// Tasks methods (MCP 2025-11-25)
	MethodTasksGet    = "tasks/get"
	MethodTasksList   = "tasks/list"
	MethodTasksCancel = "tasks/cancel"
	MethodTasksResult = "tasks/result"
)

const (
	NotificationInitialized = "notifications/initialized"

	NotificationToolsListChanged = "notifications/tools/list_changed"

	NotificationResourcesListChanged = "notifications/resources/list_changed"
	NotificationResourcesUpdated     = "notifications/resources/updated"

	NotificationPromptsListChanged = "notifications/prompts/list_changed"

	NotificationRootsListChanged = "notifications/roots/list_changed"

	NotificationProgress  = "notifications/progress"
	NotificationCancelled = "notifications/cancelled"

	NotificationLoggingMessage = "notifications/message"

	// Elicitation notifications (MCP 2025-11-25)
	NotificationElicitationComplete = "notifications/elicitation/complete"

	// Tasks notifications (MCP 2025-11-25)
	NotificationTasksStatus = "notifications/tasks/status"
)
