package contract

import "testing"

func todoRoutes() []struct {
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
		{"/api/v1/todos", "get",
			map[string]string{"200": "TodoList", "401": "ErrorEnvelope", "422": "ErrorEnvelope"}, ""},
		{"/api/v1/todos", "post",
			map[string]string{"201": "Todo", "401": "ErrorEnvelope", "422": "ErrorEnvelope"}, "CreateTodoRequest"},
		{"/api/v1/todos/{todoId}", "get",
			map[string]string{"200": "Todo", "401": "ErrorEnvelope", "404": "ErrorEnvelope"}, ""},
		{"/api/v1/todos/{todoId}", "patch",
			map[string]string{"200": "Todo", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "409": "ErrorEnvelope", "422": "ErrorEnvelope"}, "UpdateTodoRequest"},
		{"/api/v1/todos/{todoId}/complete", "post",
			map[string]string{"200": "Todo", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "409": "ErrorEnvelope", "422": "ErrorEnvelope"}, "VersionRequest"},
	}
}

func TestTodoContractRoutesCodesAndSchemas(t *testing.T) {
	document := loadDoc(t, "todo.yaml")
	if document.OpenAPI != "3.1.1" {
		t.Fatalf("openapi = %q, want 3.1.1", document.OpenAPI)
	}
	assertDocRoutes(t, document, todoRoutes())

	schemas := document.Components.Schemas
	if !docErrorEnvelope(schemas["ErrorEnvelope"]) {
		t.Fatalf("ErrorEnvelope = %#v", schemas["ErrorEnvelope"])
	}

	todo := schemas["Todo"]
	required := []string{"id", "title", "status", "overdue", "reminderVersion", "version", "createdAt", "updatedAt"}
	optional := []string{"description", "dueAtUtc", "timezoneAtInput", "completedAt", "deletedAt"}
	if !docClosedObject(todo, required) || !docPropertiesAre(todo, append(required, optional...)) {
		t.Fatalf("Todo = %#v", todo)
	}
	if !docMaxLength(todo.Properties["title"], 200) ||
		!docStringEnum(todo.Properties["status"], []string{"pending", "completed"}) ||
		!docIsBoolean(todo.Properties["overdue"]) ||
		!docIsInteger(todo.Properties["reminderVersion"]) || !docIsInteger(todo.Properties["version"]) ||
		!docDateTime(todo.Properties["createdAt"]) || !docDateTime(todo.Properties["updatedAt"]) ||
		!docDateTime(todo.Properties["dueAtUtc"]) || !docDateTime(todo.Properties["completedAt"]) || !docDateTime(todo.Properties["deletedAt"]) {
		t.Fatalf("Todo properties = %#v", todo.Properties)
	}

	list := schemas["TodoList"]
	if !docClosedObject(list, []string{"todos"}) || !docArrayOfRef(list.Properties["todos"], "Todo") {
		t.Fatalf("TodoList = %#v", list)
	}

	create := schemas["CreateTodoRequest"]
	if !docClosedObject(create, []string{"title"}) ||
		!docPropertiesAre(create, []string{"title", "description", "dueAtUtc", "timezoneAtInput"}) ||
		!docMaxLength(create.Properties["title"], 200) || !docDateTime(create.Properties["dueAtUtc"]) {
		t.Fatalf("CreateTodoRequest = %#v", create)
	}

	update := schemas["UpdateTodoRequest"]
	if !docClosedObject(update, []string{"version"}) ||
		!docPropertiesAre(update, []string{"version", "title", "description", "dueAtUtc", "timezoneAtInput"}) ||
		!docIsInteger(update.Properties["version"]) || !docMaxLength(update.Properties["title"], 200) {
		t.Fatalf("UpdateTodoRequest = %#v", update)
	}

	version := schemas["VersionRequest"]
	if !docClosedObject(version, []string{"version"}) || !docIsInteger(version.Properties["version"]) {
		t.Fatalf("VersionRequest = %#v", version)
	}
}

func TestTodoContractRejectsMutation(t *testing.T) {
	document := loadDoc(t, "todo.yaml")
	if !todoContractValid(document) {
		t.Fatal("shipped todo contract failed its own validator")
	}
	// Dropping the optimistic version requirement must fail.
	mutated := loadDoc(t, "todo.yaml")
	update := mutated.Components.Schemas["UpdateTodoRequest"]
	update.Required = nil
	mutated.Components.Schemas["UpdateTodoRequest"] = update
	if todoContractValid(mutated) {
		t.Fatal("mutation (UpdateTodoRequest without required version) unexpectedly passed validation")
	}
}

func todoContractValid(document docDocument) bool {
	if document.OpenAPI != "3.1.1" {
		return false
	}
	for _, route := range todoRoutes() {
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
	todo := schemas["Todo"]
	if !docClosedObject(todo, []string{"id", "title", "status", "overdue", "reminderVersion", "version", "createdAt", "updatedAt"}) ||
		!docMaxLength(todo.Properties["title"], 200) ||
		!docStringEnum(todo.Properties["status"], []string{"pending", "completed"}) {
		return false
	}
	list := schemas["TodoList"]
	if !docClosedObject(list, []string{"todos"}) || !docArrayOfRef(list.Properties["todos"], "Todo") {
		return false
	}
	create := schemas["CreateTodoRequest"]
	if !docClosedObject(create, []string{"title"}) || !docMaxLength(create.Properties["title"], 200) {
		return false
	}
	update := schemas["UpdateTodoRequest"]
	if !docClosedObject(update, []string{"version"}) || !docIsInteger(update.Properties["version"]) {
		return false
	}
	version := schemas["VersionRequest"]
	return docClosedObject(version, []string{"version"}) && docIsInteger(version.Properties["version"])
}
