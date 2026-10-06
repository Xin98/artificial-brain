package contract

import "testing"

func conversationRoutes() []struct {
	path    string
	method  string
	schemas map[string]string
	body    string
} {
	return []struct {
		path    string
		method  string
		schemas map[string]string
		body    string
	}{
		{"/api/v1/conversation/messages", "post",
			map[string]string{"200": "ConversationResponse", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "422": "ErrorEnvelope"}, "ConversationMessageRequest"},
		{"/api/v1/conversation/sessions", "get",
			map[string]string{"200": "SessionListResponse", "401": "ErrorEnvelope", "422": "ErrorEnvelope"}, ""},
		{"/api/v1/conversation/sessions", "post",
			map[string]string{"201": "SessionView", "401": "ErrorEnvelope", "422": "ErrorEnvelope"}, "SessionCreateRequest"},
		{"/api/v1/conversation/sessions/{sessionId}/messages", "get",
			map[string]string{"200": "SessionHistoryResponse", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "422": "ErrorEnvelope"}, ""},
		{"/api/v1/conversation/sessions/{sessionId}", "patch",
			map[string]string{"200": "SessionView", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "422": "ErrorEnvelope"}, "SessionRenameRequest"},
		{"/api/v1/conversation/sessions/{sessionId}", "delete",
			map[string]string{"204": "", "401": "ErrorEnvelope", "404": "ErrorEnvelope"}, ""},
		{"/api/v1/confirmations", "post",
			map[string]string{"201": "ConfirmationCreated", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "409": "ErrorEnvelope"}, "ConfirmationRequest"},
		{"/api/v1/confirmations/{confirmationId}/confirm", "post",
			map[string]string{"200": "ConversationResponse", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "409": "ErrorEnvelope", "410": "ErrorEnvelope", "422": "ErrorEnvelope"}, "ConfirmActionRequest"},
	}
}

var conversationKinds = []string{
	"todo_created", "clarification", "candidates", "confirmation_required",
	"todo_list", "todo_deleted", "not_found", "unsupported", "chat",
}

func TestConversationContractRoutesCodesAndSchemas(t *testing.T) {
	document := loadDoc(t, "conversation.yaml")
	if document.OpenAPI != "3.1.1" {
		t.Fatalf("openapi = %q, want 3.1.1", document.OpenAPI)
	}
	assertDocRoutes(t, document, conversationRoutes())

	schemas := document.Components.Schemas
	if !docErrorEnvelope(schemas["ErrorEnvelope"]) {
		t.Fatalf("ErrorEnvelope = %#v", schemas["ErrorEnvelope"])
	}

	message := schemas["ConversationMessageRequest"]
	if !docClosedObject(message, []string{"text", "timezone"}) ||
		!docPropertiesAre(message, []string{"text", "timezone", "sessionId"}) ||
		!docMaxLength(message.Properties["text"], 1000) || !docIsString(message.Properties["sessionId"]) {
		t.Fatalf("ConversationMessageRequest = %#v", message)
	}

	request := schemas["ConfirmationRequest"]
	if !docClosedObject(request, []string{"intent", "todoId"}) || !docStringEnum(request.Properties["intent"], []string{"todo.delete"}) {
		t.Fatalf("ConfirmationRequest = %#v", request)
	}

	created := schemas["ConfirmationCreated"]
	if !docClosedObject(created, []string{"confirmationId", "expiresAt"}) || !docDateTime(created.Properties["expiresAt"]) {
		t.Fatalf("ConfirmationCreated = %#v", created)
	}

	response := schemas["ConversationResponse"]
	required := []string{"kind", "correlationId"}
	optional := []string{"reply", "sessionId", "todo", "resolvedDueAtUtc", "localEcho", "timezoneEcho", "missingFields",
		"candidates", "confirmationId", "expiresAt", "todos", "todoId"}
	if !docClosedObject(response, required) || !docPropertiesAre(response, append(required, optional...)) {
		t.Fatalf("ConversationResponse = %#v", response)
	}
	if !docIsString(response.Properties["reply"]) || !docMaxLength(response.Properties["reply"], 2000) ||
		!docIsString(response.Properties["sessionId"]) {
		t.Fatalf("ConversationResponse reply/sessionId = %#v", response.Properties)
	}
	if !docStringEnum(response.Properties["kind"], conversationKinds) {
		t.Fatalf("ConversationResponse.kind enum = %#v", response.Properties["kind"].Enum)
	}
	if !docDateTime(response.Properties["resolvedDueAtUtc"]) || !docDateTime(response.Properties["expiresAt"]) {
		t.Fatalf("ConversationResponse times = %#v", response.Properties)
	}
	missingFields := response.Properties["missingFields"]
	if missingFields.Type != "array" || missingFields.Items == nil || missingFields.Items.Type != "string" {
		t.Fatalf("ConversationResponse.missingFields = %#v", missingFields)
	}
	if !docArrayOfRef(response.Properties["candidates"], "Candidate") || !docArrayOfRef(response.Properties["todos"], "TodoView") {
		t.Fatalf("ConversationResponse arrays = %#v", response.Properties)
	}
	if response.Properties["todo"].Ref != "#/components/schemas/TodoView" {
		t.Fatalf("ConversationResponse.todo ref = %q", response.Properties["todo"].Ref)
	}

	candidate := schemas["Candidate"]
	if !docClosedObject(candidate, []string{"todoId", "title", "version"}) ||
		!docPropertiesAre(candidate, []string{"todoId", "title", "version", "dueAtUtc"}) ||
		!docIsInteger(candidate.Properties["version"]) || !docDateTime(candidate.Properties["dueAtUtc"]) {
		t.Fatalf("Candidate = %#v", candidate)
	}

	todoView := schemas["TodoView"]
	viewRequired := []string{"id", "title", "status", "overdue", "reminderVersion", "version", "createdAt", "updatedAt"}
	if !docClosedObject(todoView, viewRequired) ||
		!docStringEnum(todoView.Properties["status"], []string{"pending", "completed"}) ||
		!docMaxLength(todoView.Properties["title"], 200) {
		t.Fatalf("TodoView = %#v", todoView)
	}

	sessionView := schemas["SessionView"]
	if !docClosedObject(sessionView, []string{"id", "title", "createdAt", "updatedAt"}) ||
		!docPropertiesAre(sessionView, []string{"id", "title", "createdAt", "updatedAt"}) ||
		!docMaxLength(sessionView.Properties["title"], 50) ||
		!docDateTime(sessionView.Properties["createdAt"]) || !docDateTime(sessionView.Properties["updatedAt"]) {
		t.Fatalf("SessionView = %#v", sessionView)
	}

	sessionList := schemas["SessionListResponse"]
	if !docClosedObject(sessionList, []string{"sessions", "hasMore"}) || !docArrayOfRef(sessionList.Properties["sessions"], "SessionView") {
		t.Fatalf("SessionListResponse = %#v", sessionList)
	}

	sessionCreate := schemas["SessionCreateRequest"]
	if !docClosedObject(sessionCreate, nil) || !docPropertiesAre(sessionCreate, []string{"title"}) ||
		!docMinLength(sessionCreate.Properties["title"], 1) || !docMaxLength(sessionCreate.Properties["title"], 50) {
		t.Fatalf("SessionCreateRequest = %#v", sessionCreate)
	}

	sessionRename := schemas["SessionRenameRequest"]
	if !docClosedObject(sessionRename, []string{"title"}) || !docPropertiesAre(sessionRename, []string{"title"}) ||
		!docMinLength(sessionRename.Properties["title"], 1) || !docMaxLength(sessionRename.Properties["title"], 50) {
		t.Fatalf("SessionRenameRequest = %#v", sessionRename)
	}

	history := schemas["SessionHistoryResponse"]
	if !docClosedObject(history, []string{"sessionId", "title", "messages", "hasMore"}) ||
		!docPropertiesAre(history, []string{"sessionId", "title", "messages", "hasMore", "nextBefore"}) ||
		!docArrayOfRef(history.Properties["messages"], "MessageView") {
		t.Fatalf("SessionHistoryResponse = %#v", history)
	}

	messageView := schemas["MessageView"]
	if !docClosedObject(messageView, []string{"id", "role", "body", "createdAt"}) ||
		!docPropertiesAre(messageView, []string{"id", "role", "body", "resolvedIntent", "createdAt"}) ||
		!docStringEnum(messageView.Properties["role"], []string{"user", "assistant"}) ||
		!docIsString(messageView.Properties["resolvedIntent"]) || !docDateTime(messageView.Properties["createdAt"]) {
		t.Fatalf("MessageView = %#v", messageView)
	}
}

func TestConversationContractRejectsMutation(t *testing.T) {
	document := loadDoc(t, "conversation.yaml")
	if !conversationContractValid(document) {
		t.Fatal("shipped conversation contract failed its own validator")
	}
	// Dropping the expired (410) outcome of confirm must fail.
	mutated := loadDoc(t, "conversation.yaml")
	confirm := mutated.Paths["/api/v1/confirmations/{confirmationId}/confirm"]
	delete(confirm.Post.Responses, "410")
	mutated.Paths["/api/v1/confirmations/{confirmationId}/confirm"] = confirm
	if conversationContractValid(mutated) {
		t.Fatal("mutation (missing 410) unexpectedly passed validation")
	}
	// Giving the content-less delete (204) a JSON body must fail.
	mutated = loadDoc(t, "conversation.yaml")
	session := mutated.Paths["/api/v1/conversation/sessions/{sessionId}"]
	session.Delete.Responses["204"] = docResponse{Content: map[string]docMediaType{
		"application/json": {Schema: docSchema{Ref: "#/components/schemas/ErrorEnvelope"}},
	}}
	mutated.Paths["/api/v1/conversation/sessions/{sessionId}"] = session
	if conversationContractValid(mutated) {
		t.Fatal("mutation (204 with content) unexpectedly passed validation")
	}
}

func conversationContractValid(document docDocument) bool {
	if document.OpenAPI != "3.1.1" {
		return false
	}
	for _, route := range conversationRoutes() {
		item, ok := document.Paths[route.path]
		if !ok {
			return false
		}
		operation := opFor(item, route.method)
		if !sameSet(mapKeys(operation.Responses), mapKeys(route.schemas)) {
			return false
		}
		for code, schema := range route.schemas {
			if schema == "" {
				if len(operation.Responses[code].Content) != 0 {
					return false
				}
				continue
			}
			if operation.Responses[code].Content["application/json"].Schema.Ref != "#/components/schemas/"+schema {
				return false
			}
		}
		if route.body != "" {
			if operation.RequestBody == nil ||
				operation.RequestBody.Content["application/json"].Schema.Ref != "#/components/schemas/"+route.body {
				return false
			}
		}
	}
	schemas := document.Components.Schemas
	if !docErrorEnvelope(schemas["ErrorEnvelope"]) {
		return false
	}
	message := schemas["ConversationMessageRequest"]
	if !docClosedObject(message, []string{"text", "timezone"}) || !docMaxLength(message.Properties["text"], 1000) {
		return false
	}
	request := schemas["ConfirmationRequest"]
	if !docClosedObject(request, []string{"intent", "todoId"}) || !docStringEnum(request.Properties["intent"], []string{"todo.delete"}) {
		return false
	}
	created := schemas["ConfirmationCreated"]
	if !docClosedObject(created, []string{"confirmationId", "expiresAt"}) || !docDateTime(created.Properties["expiresAt"]) {
		return false
	}
	response := schemas["ConversationResponse"]
	if !docClosedObject(response, []string{"kind", "correlationId"}) || !docStringEnum(response.Properties["kind"], conversationKinds) {
		return false
	}
	candidate := schemas["Candidate"]
	if !docClosedObject(candidate, []string{"todoId", "title", "version"}) || !docIsInteger(candidate.Properties["version"]) {
		return false
	}
	todoView := schemas["TodoView"]
	if !docClosedObject(todoView, []string{"id", "title", "status", "overdue", "reminderVersion", "version", "createdAt", "updatedAt"}) ||
		!docStringEnum(todoView.Properties["status"], []string{"pending", "completed"}) {
		return false
	}
	sessionView := schemas["SessionView"]
	if !docClosedObject(sessionView, []string{"id", "title", "createdAt", "updatedAt"}) || !docMaxLength(sessionView.Properties["title"], 50) {
		return false
	}
	sessionList := schemas["SessionListResponse"]
	if !docClosedObject(sessionList, []string{"sessions", "hasMore"}) || !docArrayOfRef(sessionList.Properties["sessions"], "SessionView") {
		return false
	}
	sessionCreate := schemas["SessionCreateRequest"]
	if !docClosedObject(sessionCreate, nil) || !docMinLength(sessionCreate.Properties["title"], 1) {
		return false
	}
	sessionRename := schemas["SessionRenameRequest"]
	if !docClosedObject(sessionRename, []string{"title"}) || !docMinLength(sessionRename.Properties["title"], 1) {
		return false
	}
	history := schemas["SessionHistoryResponse"]
	if !docClosedObject(history, []string{"sessionId", "title", "messages", "hasMore"}) || !docArrayOfRef(history.Properties["messages"], "MessageView") {
		return false
	}
	messageView := schemas["MessageView"]
	return docClosedObject(messageView, []string{"id", "role", "body", "createdAt"}) &&
		docStringEnum(messageView.Properties["role"], []string{"user", "assistant"})
}
