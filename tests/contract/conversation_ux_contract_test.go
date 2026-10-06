package contract

import "testing"

func TestConversationUXContractPagingSchedulingAndConfirmation(t *testing.T) {
	doc := loadDoc(t, "conversation.yaml")
	schemas := doc.Components.Schemas
	for _, name := range []string{"TodoView"} {
		view := schemas[name]
		channels := view.Properties["reminderChannels"]
		if !docIsBoolean(view.Properties["reminderScheduled"]) || channels.Type != "array" || channels.Items == nil || channels.Items.Type != "string" {
			t.Fatalf("%s lacks scheduling feedback", name)
		}
	}
	if !docIsBoolean(schemas["SessionListResponse"].Properties["hasMore"]) || !docIsInteger(schemas["SessionListResponse"].Properties["nextOffset"]) {
		t.Fatal("missing session pagination metadata")
	}
	if !docIsBoolean(schemas["SessionHistoryResponse"].Properties["hasMore"]) || !docIsString(schemas["SessionHistoryResponse"].Properties["nextBefore"]) {
		t.Fatal("missing history pagination metadata")
	}
	body := doc.Paths["/api/v1/confirmations/{confirmationId}/confirm"].Post.RequestBody
	if body == nil || body.Content["application/json"].Schema.Ref != "#/components/schemas/ConfirmActionRequest" {
		t.Fatal("confirm body missing optional session")
	}
	if !docClosedObject(schemas["ConfirmActionRequest"], nil) || !docIsString(schemas["ConfirmActionRequest"].Properties["sessionId"]) {
		t.Fatal("confirm body must preserve optional session")
	}
	todo := loadDoc(t, "todo.yaml").Components.Schemas["Todo"]
	if !docIsBoolean(todo.Properties["reminderScheduled"]) || todo.Properties["reminderChannels"].Type != "array" {
		t.Fatal("todo scheduling contract missing")
	}
}
