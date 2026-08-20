package contract

import (
	"strings"
	"testing"
)

// Delivery lifecycle vocabulary shared by the DeliveryView enums and the ops
// count buckets.
var (
	deliveryStates     = []string{"scheduled", "sending", "succeeded", "failed", "suppressed"}
	suppressionReasons = []string{"todo_completed", "todo_deleted", "version_stale", "channel_unavailable", "plan_revoked"}
	receiptStates      = []string{"received_ok", "received_failed"}
	deliveryBuckets    = []string{"scheduled", "sending", "retrying", "succeeded", "failed", "suppressed"}
)

func reminderRoutes() []struct {
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
		{"/api/v1/reminders", "get",
			map[string]string{"200": "DeliveryList", "401": "ErrorEnvelope", "422": "ErrorEnvelope"}, ""},
		{"/api/v1/ops/reminder", "get",
			map[string]string{"200": "ReminderOps", "401": "ErrorEnvelope"}, ""},
		{"/api/v1/webhooks/receipts/sms", "post",
			map[string]string{"200": "ReceiptAccepted", "401": "ErrorEnvelope", "422": "ErrorEnvelope"}, "ReceiptRequest"},
		{"/api/v1/dev/reminder-outbox", "get",
			map[string]string{"200": "DevOutboxMessageList", "422": "ErrorEnvelope"}, ""},
	}
}

func TestReminderContractRoutesCodesAndSchemas(t *testing.T) {
	document := loadDoc(t, "reminder.yaml")
	if document.OpenAPI != "3.1.1" {
		t.Fatalf("openapi = %q, want 3.1.1", document.OpenAPI)
	}
	assertDocRoutes(t, document, reminderRoutes())

	// The receipt webhook carries no session; its description must name the
	// HMAC-SHA256 signature header that authenticates it.
	receipt := document.Paths["/api/v1/webhooks/receipts/sms"].Post
	if !strings.Contains(receipt.Description, "HMAC-SHA256") || !strings.Contains(receipt.Description, "X-Receipt-Signature") {
		t.Fatalf("receipt webhook description = %q, want HMAC-SHA256 X-Receipt-Signature", receipt.Description)
	}
	// The dev outbox is double gated; its description must say so and state
	// the route is absent otherwise.
	outbox := document.Paths["/api/v1/dev/reminder-outbox"].Get
	if !strings.Contains(outbox.Description, "APP_ENV") || !strings.Contains(outbox.Description, "REMINDER_DEV_OUTBOX_ENABLED") ||
		!strings.Contains(outbox.Description, "absent") {
		t.Fatalf("dev outbox description = %q, want double gate and absent-otherwise", outbox.Description)
	}

	schemas := document.Components.Schemas
	assertExactSet(t, "schemas", mapKeys(schemas), []string{
		"ErrorEnvelope", "DeliveryView", "DeliveryList", "ReminderOps", "QueueDepth",
		"DeliveryCounts", "ReceiptRequest", "ReceiptAccepted", "DevOutboxMessage", "DevOutboxMessageList",
	})
	if !docErrorEnvelope(schemas["ErrorEnvelope"]) {
		t.Fatalf("ErrorEnvelope = %#v", schemas["ErrorEnvelope"])
	}

	delivery := schemas["DeliveryView"]
	required := []string{"id", "todoId", "todoTitle", "channel", "state", "attemptCount", "scheduledAt", "createdAt"}
	optional := []string{"suppressionReason", "providerMessageId", "lastErrorCode", "submittedAt", "finalizedAt", "receiptState", "receiptAt", "receiptErrorCode"}
	if !docClosedObject(delivery, required) || !docPropertiesAre(delivery, append(append([]string{}, required...), optional...)) {
		t.Fatalf("DeliveryView = %#v", delivery)
	}
	if !docIsString(delivery.Properties["id"]) || !docIsString(delivery.Properties["todoId"]) || !docIsString(delivery.Properties["todoTitle"]) ||
		!docStringEnum(delivery.Properties["channel"], []string{"email", "sms"}) ||
		!docStringEnum(delivery.Properties["state"], deliveryStates) ||
		!docStringEnum(delivery.Properties["suppressionReason"], suppressionReasons) ||
		!docIsInteger(delivery.Properties["attemptCount"]) ||
		!docIsString(delivery.Properties["providerMessageId"]) || !docIsString(delivery.Properties["lastErrorCode"]) ||
		!docDateTime(delivery.Properties["scheduledAt"]) || !docDateTime(delivery.Properties["createdAt"]) ||
		!docDateTime(delivery.Properties["submittedAt"]) || !docDateTime(delivery.Properties["finalizedAt"]) ||
		!docStringEnum(delivery.Properties["receiptState"], receiptStates) ||
		!docDateTime(delivery.Properties["receiptAt"]) || !docIsString(delivery.Properties["receiptErrorCode"]) {
		t.Fatalf("DeliveryView properties = %#v", delivery.Properties)
	}

	list := schemas["DeliveryList"]
	if !docClosedObject(list, []string{"deliveries"}) || !docArrayOfRef(list.Properties["deliveries"], "DeliveryView") {
		t.Fatalf("DeliveryList = %#v", list)
	}

	ops := schemas["ReminderOps"]
	opsFields := []string{"queues", "deliveries", "retryRate", "latencyP95Ms", "checkedAt"}
	if !docClosedObject(ops, opsFields) || !docPropertiesAre(ops, opsFields) {
		t.Fatalf("ReminderOps = %#v", ops)
	}
	if !docArrayOfRef(ops.Properties["queues"], "QueueDepth") ||
		ops.Properties["deliveries"].Ref != "#/components/schemas/DeliveryCounts" ||
		ops.Properties["retryRate"].Type != "number" ||
		!docIsInteger(ops.Properties["latencyP95Ms"]) ||
		!docDateTime(ops.Properties["checkedAt"]) {
		t.Fatalf("ReminderOps properties = %#v", ops.Properties)
	}

	queue := schemas["QueueDepth"]
	if !docClosedObject(queue, []string{"queue", "depth", "oldestWaitSeconds"}) ||
		!docIsString(queue.Properties["queue"]) ||
		!docIsInteger(queue.Properties["depth"]) ||
		!docIsInteger(queue.Properties["oldestWaitSeconds"]) {
		t.Fatalf("QueueDepth = %#v", queue)
	}

	counts := schemas["DeliveryCounts"]
	if !docClosedObject(counts, deliveryBuckets) || !docPropertiesAre(counts, deliveryBuckets) {
		t.Fatalf("DeliveryCounts = %#v", counts)
	}
	for _, bucket := range deliveryBuckets {
		if !docIsInteger(counts.Properties[bucket]) {
			t.Fatalf("DeliveryCounts.%s = %#v, want integer", bucket, counts.Properties[bucket])
		}
	}

	receiptRequest := schemas["ReceiptRequest"]
	if !docClosedObject(receiptRequest, []string{"providerMessageId", "delivered"}) ||
		!docPropertiesAre(receiptRequest, []string{"providerMessageId", "delivered", "errorCode"}) ||
		!docIsString(receiptRequest.Properties["providerMessageId"]) ||
		!docIsBoolean(receiptRequest.Properties["delivered"]) ||
		!docIsString(receiptRequest.Properties["errorCode"]) {
		t.Fatalf("ReceiptRequest = %#v", receiptRequest)
	}

	accepted := schemas["ReceiptAccepted"]
	if !docClosedObject(accepted, []string{}) || len(accepted.Properties) != 0 {
		t.Fatalf("ReceiptAccepted = %#v", accepted)
	}

	message := schemas["DevOutboxMessage"]
	messageFields := []string{"address", "channel", "todoId", "body", "createdAt"}
	if !docClosedObject(message, messageFields) || !docPropertiesAre(message, messageFields) ||
		!docIsString(message.Properties["address"]) || !docIsString(message.Properties["channel"]) ||
		!docIsString(message.Properties["todoId"]) || !docIsString(message.Properties["body"]) ||
		!docDateTime(message.Properties["createdAt"]) {
		t.Fatalf("DevOutboxMessage = %#v", message)
	}

	messages := schemas["DevOutboxMessageList"]
	if !docClosedObject(messages, []string{"messages"}) || !docArrayOfRef(messages.Properties["messages"], "DevOutboxMessage") {
		t.Fatalf("DevOutboxMessageList = %#v", messages)
	}
}

func TestReminderContractRejectsMutation(t *testing.T) {
	document := loadDoc(t, "reminder.yaml")
	if !reminderContractValid(document) {
		t.Fatal("shipped reminder contract failed its own validator")
	}
	// Widening the delivery state enum must fail.
	widened := loadDoc(t, "reminder.yaml")
	delivery := widened.Components.Schemas["DeliveryView"]
	delivery.Properties["state"] = docSchema{Type: "string", Enum: append(append([]string{}, deliveryStates...), "cancelled")}
	widened.Components.Schemas["DeliveryView"] = delivery
	if reminderContractValid(widened) {
		t.Fatal("mutation (widened state enum) unexpectedly passed validation")
	}
	// Dropping a required delivery field must fail.
	dropped := loadDoc(t, "reminder.yaml")
	droppedView := dropped.Components.Schemas["DeliveryView"]
	droppedView.Required = []string{"id", "todoId", "todoTitle", "channel", "state", "scheduledAt", "createdAt"}
	dropped.Components.Schemas["DeliveryView"] = droppedView
	if reminderContractValid(dropped) {
		t.Fatal("mutation (attemptCount no longer required) unexpectedly passed validation")
	}
}

func reminderContractValid(document docDocument) bool {
	if document.OpenAPI != "3.1.1" {
		return false
	}
	for _, route := range reminderRoutes() {
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
	delivery := schemas["DeliveryView"]
	if !docClosedObject(delivery, []string{"id", "todoId", "todoTitle", "channel", "state", "attemptCount", "scheduledAt", "createdAt"}) ||
		!docStringEnum(delivery.Properties["channel"], []string{"email", "sms"}) ||
		!docStringEnum(delivery.Properties["state"], deliveryStates) ||
		!docStringEnum(delivery.Properties["suppressionReason"], suppressionReasons) ||
		!docStringEnum(delivery.Properties["receiptState"], receiptStates) {
		return false
	}
	list := schemas["DeliveryList"]
	if !docClosedObject(list, []string{"deliveries"}) || !docArrayOfRef(list.Properties["deliveries"], "DeliveryView") {
		return false
	}
	ops := schemas["ReminderOps"]
	if !docClosedObject(ops, []string{"queues", "deliveries", "retryRate", "latencyP95Ms", "checkedAt"}) ||
		!docArrayOfRef(ops.Properties["queues"], "QueueDepth") ||
		ops.Properties["deliveries"].Ref != "#/components/schemas/DeliveryCounts" ||
		ops.Properties["retryRate"].Type != "number" ||
		!docIsInteger(ops.Properties["latencyP95Ms"]) ||
		!docDateTime(ops.Properties["checkedAt"]) {
		return false
	}
	counts := schemas["DeliveryCounts"]
	if !docClosedObject(counts, deliveryBuckets) {
		return false
	}
	for _, bucket := range deliveryBuckets {
		if !docIsInteger(counts.Properties[bucket]) {
			return false
		}
	}
	receiptRequest := schemas["ReceiptRequest"]
	if !docClosedObject(receiptRequest, []string{"providerMessageId", "delivered"}) ||
		!docIsBoolean(receiptRequest.Properties["delivered"]) {
		return false
	}
	message := schemas["DevOutboxMessage"]
	if !docClosedObject(message, []string{"address", "channel", "todoId", "body", "createdAt"}) {
		return false
	}
	messages := schemas["DevOutboxMessageList"]
	return docClosedObject(messages, []string{"messages"}) && docArrayOfRef(messages.Properties["messages"], "DevOutboxMessage")
}
