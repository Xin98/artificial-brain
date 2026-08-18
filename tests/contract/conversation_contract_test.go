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
			map[string]string{"200": "ConversationResponse", "401": "ErrorEnvelope", "422": "ErrorEnvelope"}, "ConversationMessageRequest"},
		{"/api/v1/confirmations", "post",
			map[string]string{"201": "ConfirmationCreated", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "409": "ErrorEnvelope"}, "ConfirmationRequest"},
		{"/api/v1/confirmations/{confirmationId}/confirm", "post",
			map[string]string{"200": "ConversationResponse", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "409": "ErrorEnvelope", "410": "ErrorEnvelope"}, ""},
	}
}

var conversationKinds = []string{
	"todo_created", "clarification", "candidates", "confirmation_required",
	"todo_list", "todo_deleted", "not_found", "unsupported",
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
	if !docClosedObject(message, []string{"text", "timezone"}) || !docMaxLength(message.Properties["text"], 1000) {
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
	optional := []string{"todo", "resolvedDueAtUtc", "localEcho", "timezoneEcho", "missingFields",
		"candidates", "confirmationId", "expiresAt", "todos", "todoId"}
	if !docClosedObject(response, required) || !docPropertiesAre(response, append(required, optional...)) {
		t.Fatalf("ConversationResponse = %#v", response)
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
	return docClosedObject(todoView, []string{"id", "title", "status", "overdue", "reminderVersion", "version", "createdAt", "updatedAt"}) &&
		docStringEnum(todoView.Properties["status"], []string{"pending", "completed"})
}
