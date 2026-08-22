package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// exportSchema parses one export-bundle JSON schema. AdditionalProperties
// stays raw: closed objects pin false while the manifest's files map pins a
// string-value schema.
type exportSchema struct {
	SchemaURI            string                  `json:"$schema"`
	Title                string                  `json:"title"`
	Type                 string                  `json:"type"`
	Format               string                  `json:"format"`
	Const                *string                 `json:"const"`
	Enum                 []string                `json:"enum"`
	Minimum              *int                    `json:"minimum"`
	MinProperties        *int                    `json:"minProperties"`
	Required             []string                `json:"required"`
	Properties           map[string]exportSchema `json:"properties"`
	AdditionalProperties json.RawMessage         `json:"additionalProperties"`
	Items                *exportSchema           `json:"items"`
}

func loadExportSchema(t *testing.T, file string) exportSchema {
	t.Helper()
	schema, err := readExportSchema(file)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func readExportSchema(file string) (exportSchema, error) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "contracts", "export-schemas", file))
	if err != nil {
		return exportSchema{}, err
	}
	var schema exportSchema
	if err := json.Unmarshal(contents, &schema); err != nil {
		return exportSchema{}, err
	}
	return schema, nil
}

func bundleSchemaClosed(value exportSchema, required []string) bool {
	if value.Type != "object" || !sameSet(value.Required, required) {
		return false
	}
	var additional bool
	if err := json.Unmarshal(value.AdditionalProperties, &additional); err != nil || additional {
		return false
	}
	return true
}

func bundleSchemaIsString(value exportSchema) bool  { return value.Type == "string" }
func bundleSchemaIsInteger(value exportSchema) bool { return value.Type == "integer" }
func bundleSchemaIsBoolean(value exportSchema) bool { return value.Type == "boolean" }

func bundleSchemaDateTime(value exportSchema) bool {
	return value.Type == "string" && value.Format == "date-time"
}

func bundleSchemaStringEnum(value exportSchema, values []string) bool {
	return value.Type == "string" && sameSet(value.Enum, values)
}

func bundleSchemaMinimumZero(value exportSchema) bool {
	return value.Minimum != nil && *value.Minimum == 0
}

func TestExportBundleContractSchemasPinWireShapes(t *testing.T) {
	manifest := loadExportSchema(t, "manifest.schema.json")
	manifestFields := []string{"schemaVersion", "sourceInstanceId", "exportedAt", "counts", "files"}
	if !bundleSchemaClosed(manifest, manifestFields) || !sameSet(mapKeys(manifest.Properties), manifestFields) {
		t.Fatalf("manifest schema = %#v", manifest)
	}
	version := manifest.Properties["schemaVersion"]
	if version.Const == nil || *version.Const != "1" {
		t.Fatalf("manifest schemaVersion = %#v, want const \"1\"", version)
	}
	if !bundleSchemaIsString(manifest.Properties["sourceInstanceId"]) ||
		!bundleSchemaDateTime(manifest.Properties["exportedAt"]) {
		t.Fatalf("manifest sourceInstanceId/exportedAt = %#v", manifest.Properties)
	}
	counts := manifest.Properties["counts"]
	countFields := []string{"todos", "deliveries", "channels"}
	if !bundleSchemaClosed(counts, countFields) || !sameSet(mapKeys(counts.Properties), countFields) {
		t.Fatalf("manifest counts = %#v", counts)
	}
	for _, field := range countFields {
		if !bundleSchemaIsInteger(counts.Properties[field]) || !bundleSchemaMinimumZero(counts.Properties[field]) {
			t.Fatalf("manifest counts.%s = %#v, want non-negative integer", field, counts.Properties[field])
		}
	}
	files := manifest.Properties["files"]
	if files.Type != "object" {
		t.Fatalf("manifest files = %#v, want object", files)
	}
	var fileValues exportSchema
	if err := json.Unmarshal(files.AdditionalProperties, &fileValues); err != nil || fileValues.Type != "string" {
		t.Fatalf("manifest files values = %s, want string checksums", files.AdditionalProperties)
	}
	if files.MinProperties == nil || *files.MinProperties != 1 {
		t.Fatalf("manifest files minProperties = %#v, want 1", files.MinProperties)
	}

	todos := loadExportSchema(t, "todos.schema.json")
	if todos.Type != "array" || todos.Items == nil {
		t.Fatalf("todos schema = %#v, want array of records", todos)
	}
	todoItem := *todos.Items
	todoRequired := []string{"id", "title", "status", "reminderVersion", "createdAt", "updatedAt"}
	todoOptional := []string{"description", "dueAtUtc", "timezoneAtInput", "completedAt", "deletedAt"}
	if !bundleSchemaClosed(todoItem, todoRequired) ||
		!sameSet(mapKeys(todoItem.Properties), append(append([]string{}, todoRequired...), todoOptional...)) {
		t.Fatalf("todos item = %#v", todoItem)
	}
	if !bundleSchemaIsString(todoItem.Properties["id"]) || !bundleSchemaIsString(todoItem.Properties["title"]) ||
		!bundleSchemaStringEnum(todoItem.Properties["status"], []string{"pending", "completed", "deleted"}) ||
		!bundleSchemaIsInteger(todoItem.Properties["reminderVersion"]) ||
		!bundleSchemaIsString(todoItem.Properties["description"]) || !bundleSchemaIsString(todoItem.Properties["timezoneAtInput"]) ||
		!bundleSchemaDateTime(todoItem.Properties["createdAt"]) || !bundleSchemaDateTime(todoItem.Properties["updatedAt"]) ||
		!bundleSchemaDateTime(todoItem.Properties["dueAtUtc"]) ||
		!bundleSchemaDateTime(todoItem.Properties["completedAt"]) || !bundleSchemaDateTime(todoItem.Properties["deletedAt"]) {
		t.Fatalf("todos item properties = %#v", todoItem.Properties)
	}

	deliveries := loadExportSchema(t, "reminder-deliveries.schema.json")
	if deliveries.Type != "array" || deliveries.Items == nil {
		t.Fatalf("reminder-deliveries schema = %#v, want array of records", deliveries)
	}
	deliveryItem := *deliveries.Items
	deliveryRequired := []string{"id", "sourceTodoRecordId", "channel", "state", "attemptCount", "todoTitleSnapshot", "scheduledAt", "createdAt", "origin"}
	deliveryOptional := []string{"suppressionReason", "providerMessageId", "lastErrorCode", "submittedAt", "finalizedAt", "receiptState", "receiptErrorCode", "receiptAt"}
	if !bundleSchemaClosed(deliveryItem, deliveryRequired) ||
		!sameSet(mapKeys(deliveryItem.Properties), append(append([]string{}, deliveryRequired...), deliveryOptional...)) {
		t.Fatalf("reminder-deliveries item = %#v", deliveryItem)
	}
	if !bundleSchemaIsString(deliveryItem.Properties["id"]) || !bundleSchemaIsString(deliveryItem.Properties["sourceTodoRecordId"]) ||
		!bundleSchemaStringEnum(deliveryItem.Properties["channel"], []string{"email", "sms"}) ||
		!bundleSchemaStringEnum(deliveryItem.Properties["state"], []string{"scheduled", "sending", "succeeded", "failed", "suppressed"}) ||
		!bundleSchemaStringEnum(deliveryItem.Properties["origin"], []string{"local", "imported"}) ||
		!bundleSchemaIsInteger(deliveryItem.Properties["attemptCount"]) || !bundleSchemaMinimumZero(deliveryItem.Properties["attemptCount"]) ||
		!bundleSchemaIsString(deliveryItem.Properties["todoTitleSnapshot"]) ||
		!bundleSchemaIsString(deliveryItem.Properties["suppressionReason"]) ||
		!bundleSchemaIsString(deliveryItem.Properties["providerMessageId"]) || !bundleSchemaIsString(deliveryItem.Properties["lastErrorCode"]) ||
		!bundleSchemaIsString(deliveryItem.Properties["receiptState"]) || !bundleSchemaIsString(deliveryItem.Properties["receiptErrorCode"]) ||
		!bundleSchemaDateTime(deliveryItem.Properties["scheduledAt"]) || !bundleSchemaDateTime(deliveryItem.Properties["createdAt"]) ||
		!bundleSchemaDateTime(deliveryItem.Properties["submittedAt"]) || !bundleSchemaDateTime(deliveryItem.Properties["finalizedAt"]) ||
		!bundleSchemaDateTime(deliveryItem.Properties["receiptAt"]) {
		t.Fatalf("reminder-deliveries item properties = %#v", deliveryItem.Properties)
	}

	preferences := loadExportSchema(t, "preferences.schema.json")
	if preferences.Type != "array" || preferences.Items == nil {
		t.Fatalf("preferences schema = %#v, want array of records", preferences)
	}
	channelItem := *preferences.Items
	channelFields := []string{"id", "kind", "address", "enabled"}
	if !bundleSchemaClosed(channelItem, channelFields) || !sameSet(mapKeys(channelItem.Properties), channelFields) {
		t.Fatalf("preferences item = %#v", channelItem)
	}
	if !bundleSchemaIsString(channelItem.Properties["id"]) ||
		!bundleSchemaStringEnum(channelItem.Properties["kind"], []string{"email", "sms"}) ||
		!bundleSchemaIsString(channelItem.Properties["address"]) ||
		!bundleSchemaIsBoolean(channelItem.Properties["enabled"]) {
		t.Fatalf("preferences item properties = %#v", channelItem.Properties)
	}
}

func TestExportBundleContractRejectsMutation(t *testing.T) {
	if !exportBundleSchemasValid() {
		t.Fatal("shipped export bundle schemas failed their own validator")
	}
	// Bumping the manifest schema version const must fail.
	bumped := loadExportSchema(t, "manifest.schema.json")
	version := bumped.Properties["schemaVersion"]
	two := "2"
	version.Const = &two
	bumped.Properties["schemaVersion"] = version
	if manifestSchemaValid(bumped) {
		t.Fatal("mutation (schemaVersion const \"2\") unexpectedly passed validation")
	}
	// Dropping a required todo field must fail.
	dropped := loadExportSchema(t, "todos.schema.json")
	item := *dropped.Items
	item.Required = []string{"id", "title", "status", "createdAt", "updatedAt"}
	dropped.Items = &item
	if todosSchemaValid(dropped) {
		t.Fatal("mutation (reminderVersion no longer required) unexpectedly passed validation")
	}
}

// exportBundleSchemasValid replays every shipped schema file through its
// validator so the mutation test can compare mutated documents against it.
func exportBundleSchemasValid() bool {
	manifest, err := readExportSchema("manifest.schema.json")
	if err != nil || !manifestSchemaValid(manifest) {
		return false
	}
	todos, err := readExportSchema("todos.schema.json")
	if err != nil || !todosSchemaValid(todos) {
		return false
	}
	deliveries, err := readExportSchema("reminder-deliveries.schema.json")
	if err != nil || !deliveriesSchemaValid(deliveries) {
		return false
	}
	preferences, err := readExportSchema("preferences.schema.json")
	if err != nil || !preferencesSchemaValid(preferences) {
		return false
	}
	return true
}

func manifestSchemaValid(manifest exportSchema) bool {
	manifestFields := []string{"schemaVersion", "sourceInstanceId", "exportedAt", "counts", "files"}
	if !bundleSchemaClosed(manifest, manifestFields) || !sameSet(mapKeys(manifest.Properties), manifestFields) {
		return false
	}
	version := manifest.Properties["schemaVersion"]
	if version.Const == nil || *version.Const != "1" {
		return false
	}
	if !bundleSchemaIsString(manifest.Properties["sourceInstanceId"]) || !bundleSchemaDateTime(manifest.Properties["exportedAt"]) {
		return false
	}
	counts := manifest.Properties["counts"]
	countFields := []string{"todos", "deliveries", "channels"}
	if !bundleSchemaClosed(counts, countFields) || !sameSet(mapKeys(counts.Properties), countFields) {
		return false
	}
	for _, field := range countFields {
		if !bundleSchemaIsInteger(counts.Properties[field]) || !bundleSchemaMinimumZero(counts.Properties[field]) {
			return false
		}
	}
	files := manifest.Properties["files"]
	if files.Type != "object" || files.MinProperties == nil || *files.MinProperties != 1 {
		return false
	}
	var fileValues exportSchema
	return json.Unmarshal(files.AdditionalProperties, &fileValues) == nil && fileValues.Type == "string"
}

func todosSchemaValid(todos exportSchema) bool {
	if todos.Type != "array" || todos.Items == nil {
		return false
	}
	item := *todos.Items
	required := []string{"id", "title", "status", "reminderVersion", "createdAt", "updatedAt"}
	optional := []string{"description", "dueAtUtc", "timezoneAtInput", "completedAt", "deletedAt"}
	return bundleSchemaClosed(item, required) &&
		sameSet(mapKeys(item.Properties), append(append([]string{}, required...), optional...)) &&
		bundleSchemaStringEnum(item.Properties["status"], []string{"pending", "completed", "deleted"}) &&
		bundleSchemaIsInteger(item.Properties["reminderVersion"]) &&
		bundleSchemaDateTime(item.Properties["createdAt"]) && bundleSchemaDateTime(item.Properties["updatedAt"])
}

func deliveriesSchemaValid(deliveries exportSchema) bool {
	if deliveries.Type != "array" || deliveries.Items == nil {
		return false
	}
	item := *deliveries.Items
	required := []string{"id", "sourceTodoRecordId", "channel", "state", "attemptCount", "todoTitleSnapshot", "scheduledAt", "createdAt", "origin"}
	optional := []string{"suppressionReason", "providerMessageId", "lastErrorCode", "submittedAt", "finalizedAt", "receiptState", "receiptErrorCode", "receiptAt"}
	return bundleSchemaClosed(item, required) &&
		sameSet(mapKeys(item.Properties), append(append([]string{}, required...), optional...)) &&
		bundleSchemaStringEnum(item.Properties["channel"], []string{"email", "sms"}) &&
		bundleSchemaStringEnum(item.Properties["state"], []string{"scheduled", "sending", "succeeded", "failed", "suppressed"}) &&
		bundleSchemaStringEnum(item.Properties["origin"], []string{"local", "imported"}) &&
		bundleSchemaIsInteger(item.Properties["attemptCount"]) && bundleSchemaMinimumZero(item.Properties["attemptCount"]) &&
		bundleSchemaDateTime(item.Properties["scheduledAt"]) && bundleSchemaDateTime(item.Properties["createdAt"])
}

func preferencesSchemaValid(preferences exportSchema) bool {
	if preferences.Type != "array" || preferences.Items == nil {
		return false
	}
	item := *preferences.Items
	fields := []string{"id", "kind", "address", "enabled"}
	return bundleSchemaClosed(item, fields) &&
		sameSet(mapKeys(item.Properties), fields) &&
		bundleSchemaStringEnum(item.Properties["kind"], []string{"email", "sms"}) &&
		bundleSchemaIsBoolean(item.Properties["enabled"])
}
