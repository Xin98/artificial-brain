package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const contractProbePrefix = "systemhealth-contract-probe:"

func TestSystemHealthContractDefinesExactVersionedHealthResponses(t *testing.T) {
	document := parseSystemHealthContract(t)
	if document.OpenAPI != "3.1.1" {
		t.Fatalf("openapi = %q, want 3.1.1", document.OpenAPI)
	}
	assertExactSet(t, "paths", mapKeys(document.Paths), []string{"/api/v1/system/health", "/health/live", "/health/ready"})
	for _, route := range []struct {
		path   string
		codes  []string
		schema string
	}{
		{"/api/v1/system/health", []string{"200"}, "SystemHealthReport"},
		{"/health/live", []string{"200"}, "Liveness"},
		{"/health/ready", []string{"200", "503"}, "SystemHealthReport"},
	} {
		path, ok := document.Paths[route.path]
		if !ok {
			t.Fatalf("GET %s is missing", route.path)
		}
		assertExactSet(t, "GET "+route.path+" responses", mapKeys(path.Get.Responses), route.codes)
		for _, code := range route.codes {
			if got := path.Get.Responses[code].Content["application/json"].Schema.Ref; got != "#/components/schemas/"+route.schema {
				t.Fatalf("GET %s response %s schema = %q", route.path, code, got)
			}
		}
	}

	report := document.Components.Schemas["SystemHealthReport"]
	assertObject(t, "SystemHealthReport", report, []string{"status", "checkedAt", "correlationId", "components"})
	assertString(t, "SystemHealthReport.status", report.Properties["status"])
	assertExactSet(t, "SystemHealthReport.status enum", report.Properties["status"].Enum, []string{"healthy", "degraded"})
	assertDateTime(t, "SystemHealthReport.checkedAt", report.Properties["checkedAt"])
	assertString(t, "SystemHealthReport.correlationId", report.Properties["correlationId"])

	components := report.Properties["components"]
	assertObject(t, "SystemHealthReport.components", components, []string{"api", "database", "worker"})
	for _, name := range []string{"api", "database", "worker"} {
		if got := components.Properties[name].Ref; got != "#/components/schemas/HealthComponent" {
			t.Fatalf("components.%s ref = %q", name, got)
		}
	}

	component := document.Components.Schemas["HealthComponent"]
	assertObject(t, "HealthComponent", component, []string{"status", "checkedAt"})
	assertString(t, "HealthComponent.status", component.Properties["status"])
	assertExactSet(t, "HealthComponent.status enum", component.Properties["status"].Enum, []string{"healthy", "unavailable"})
	assertDateTime(t, "HealthComponent.checkedAt", component.Properties["checkedAt"])
	assertString(t, "HealthComponent.detail", component.Properties["detail"])
	if component.Properties["detail"].MaxLength == nil || *component.Properties["detail"].MaxLength != 200 {
		t.Fatalf("HealthComponent.detail maxLength = %#v", component.Properties["detail"].MaxLength)
	}

	liveness := document.Components.Schemas["Liveness"]
	assertObject(t, "Liveness", liveness, []string{"status", "checkedAt", "correlationId"})
	assertExactSet(t, "Liveness.status enum", liveness.Properties["status"].Enum, []string{"healthy"})
	assertDateTime(t, "Liveness.checkedAt", liveness.Properties["checkedAt"])

	assertRepresentativeReportJSON(t, report, component)
}

func TestSystemHealthContractRejectsMalformedOrIncompleteDocuments(t *testing.T) {
	for _, source := range []string{
		"openapi: 3.1.1\npaths: [",
		"openapi: 3.1.1\npaths:\n  # /api/v1/system/health:\ncomponents: {}\n",
		"openapi: 3.1.1\npaths:\n  /api/v1/system/health:\n    get:\n      responses:\n        '200': {}\ncomponents: {}\n",
		"openapi: 3.1.1\npaths: {}\ncomponents:\n  schemas:\n    SystemHealthReport:\n      type: object\n      properties:\n        status:\n          type: string\n      enum: [healthy, degraded]\n",
		"openapi: 3.1.1\npaths: {}\ncomponents:\n  schemas:\n    SystemHealthReport:\n      type: object\n      properties:\n        components:\n          type: object\n          properties:\n            api: { $ref: '#/components/schemas/HealthComponent' }\n",
	} {
		var document contractDocument
		if err := yaml.Unmarshal([]byte(source), &document); err == nil && systemHealthContractValid(document) {
			t.Fatalf("invalid contract unexpectedly passed: %s", source)
		}
	}
	for _, mutation := range []struct {
		name  string
		apply func(*contractDocument)
	}{
		{"missing nested worker requirement", func(document *contractDocument) {
			report := document.Components.Schemas["SystemHealthReport"]
			nested := report.Properties["components"]
			nested.Required = []string{"api", "database"}
			report.Properties["components"] = nested
			document.Components.Schemas["SystemHealthReport"] = report
		}},
		{"misplaced report status enum", func(document *contractDocument) {
			report := document.Components.Schemas["SystemHealthReport"]
			status := report.Properties["status"]
			status.Enum = nil
			report.Properties["status"] = status
			report.Enum = []string{"healthy", "degraded"}
			document.Components.Schemas["SystemHealthReport"] = report
		}},
		{"missing readiness response schema", func(document *contractDocument) {
			ready := document.Paths["/health/ready"]
			response := ready.Get.Responses["503"]
			response.Content = nil
			ready.Get.Responses["503"] = response
			document.Paths["/health/ready"] = ready
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			document := parseSystemHealthContract(t)
			mutation.apply(&document)
			if systemHealthContractValid(document) {
				t.Fatal("invalid contract unexpectedly passed validation")
			}
		})
	}
}

func assertRepresentativeReportJSON(t *testing.T, reportSchema, componentSchema schema) {
	t.Helper()
	encoded := productionRepresentativeJSON(t)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &top); err != nil {
		t.Fatal(err)
	}
	assertExactSet(t, "serialized report keys", mapKeys(top), []string{"status", "checkedAt", "correlationId", "components"})
	var status string
	if err := json.Unmarshal(top["status"], &status); err != nil {
		t.Fatal(err)
	}
	if !contains(reportSchema.Properties["status"].Enum, status) {
		t.Fatalf("production report status %q absent from contract enum", status)
	}
	var serializedTime string
	if err := json.Unmarshal(top["checkedAt"], &serializedTime); err != nil {
		t.Fatal(err)
	}
	if serializedTime != "2026-08-13T04:00:00Z" {
		t.Fatalf("serialized checkedAt = %q", serializedTime)
	}
	var correlationID string
	if err := json.Unmarshal(top["correlationId"], &correlationID); err != nil {
		t.Fatal(err)
	}
	if correlationID != "req-1" {
		t.Fatalf("serialized correlationId = %q", correlationID)
	}
	var components map[string]json.RawMessage
	if err := json.Unmarshal(top["components"], &components); err != nil {
		t.Fatal(err)
	}
	assertExactSet(t, "serialized component keys", mapKeys(components), []string{"api", "database", "worker"})
	for name, raw := range components {
		var component map[string]json.RawMessage
		if err := json.Unmarshal(raw, &component); err != nil {
			t.Fatal(err)
		}
		assertExactSet(t, "serialized "+name+" keys", mapKeys(component), []string{"status", "checkedAt"})
		if _, ok := component["detail"]; ok {
			t.Fatalf("serialized %s has empty detail", name)
		}
		if err := json.Unmarshal(component["checkedAt"], &serializedTime); err != nil {
			t.Fatal(err)
		}
		if serializedTime != "2026-08-13T04:00:00Z" {
			t.Fatalf("serialized %s checkedAt = %q", name, serializedTime)
		}
		if err := json.Unmarshal(component["status"], &status); err != nil {
			t.Fatal(err)
		}
		if !contains(componentSchema.Properties["status"].Enum, status) {
			t.Fatalf("production component status %q absent from contract enum", status)
		}
	}
}

// productionRepresentativeJSON crosses Go's internal-package boundary without
// adding a public platform facade: it runs a narrowly gated internal test probe.
func productionRepresentativeJSON(t *testing.T) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-json", "./backend/internal/platform/systemhealth", "-run", "^TestContractRepresentativeReportProbe$")
	command.Dir = filepath.Join("..", "..")
	command.Env = append(os.Environ(), "SYSTEM_HEALTH_CONTRACT_PROBE=1")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("production report probe failed: %v", err)
	}
	var payloads [][]byte
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var event struct {
			Output string `json:"Output"`
		}
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(event.Output, contractProbePrefix) {
			payloads = append(payloads, []byte(strings.TrimPrefix(event.Output, contractProbePrefix)))
		}
	}
	if len(payloads) != 1 {
		t.Fatalf("production report probe payloads = %d, want 1", len(payloads))
	}
	return payloads[0]
}

func parseSystemHealthContract(t *testing.T) contractDocument {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "contracts", "openapi", "system-health.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var document contractDocument
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	return document
}
func assertObject(t *testing.T, name string, got schema, required []string) {
	t.Helper()
	if got.Type != "object" || got.AdditionalProperties == nil || *got.AdditionalProperties {
		t.Fatalf("%s must be a closed object: %#v", name, got)
	}
	assertExactSet(t, name+" required", got.Required, required)
	assertExactSet(t, name+" properties", mapKeys(got.Properties), append(required, optionalProperties(name)...))
}
func optionalProperties(name string) []string {
	if name == "HealthComponent" {
		return []string{"detail"}
	}
	return nil
}
func assertString(t *testing.T, name string, got schema) {
	t.Helper()
	if got.Type != "string" {
		t.Fatalf("%s type = %q", name, got.Type)
	}
}
func assertDateTime(t *testing.T, name string, got schema) {
	t.Helper()
	assertString(t, name, got)
	if got.Format != "date-time" {
		t.Fatalf("%s format = %q", name, got.Format)
	}
}
func assertExactSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !sameSet(got, want) {
		t.Fatalf("%s = %#v, want set %#v", name, got, want)
	}
}
func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(got))
	for _, value := range got {
		if seen[value] {
			return false
		}
		seen[value] = true
	}
	for _, value := range want {
		if !seen[value] {
			return false
		}
	}
	return true
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func mapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
func systemHealthContractValid(document contractDocument) bool {
	if document.OpenAPI != "3.1.1" || !sameSet(mapKeys(document.Paths), []string{"/api/v1/system/health", "/health/live", "/health/ready"}) {
		return false
	}
	if !responseHasSchema(document.Paths["/api/v1/system/health"], "200", "SystemHealthReport") || !responseHasSchema(document.Paths["/health/live"], "200", "Liveness") || !responseHasSchema(document.Paths["/health/ready"], "200", "SystemHealthReport") || !responseHasSchema(document.Paths["/health/ready"], "503", "SystemHealthReport") {
		return false
	}
	report := document.Components.Schemas["SystemHealthReport"]
	if !closedObject(report, []string{"status", "checkedAt", "correlationId", "components"}) || !stringEnum(report.Properties["status"], []string{"healthy", "degraded"}) || !dateTime(report.Properties["checkedAt"]) || !isString(report.Properties["correlationId"]) {
		return false
	}
	nested := report.Properties["components"]
	if !closedObject(nested, []string{"api", "database", "worker"}) {
		return false
	}
	for _, name := range []string{"api", "database", "worker"} {
		if nested.Properties[name].Ref != "#/components/schemas/HealthComponent" {
			return false
		}
	}
	component := document.Components.Schemas["HealthComponent"]
	if !closedObject(component, []string{"status", "checkedAt"}) || !sameSet(mapKeys(component.Properties), []string{"status", "checkedAt", "detail"}) || !stringEnum(component.Properties["status"], []string{"healthy", "unavailable"}) || !dateTime(component.Properties["checkedAt"]) || !isString(component.Properties["detail"]) || component.Properties["detail"].MaxLength == nil || *component.Properties["detail"].MaxLength != 200 {
		return false
	}
	liveness := document.Components.Schemas["Liveness"]
	return closedObject(liveness, []string{"status", "checkedAt", "correlationId"}) && stringEnum(liveness.Properties["status"], []string{"healthy"}) && dateTime(liveness.Properties["checkedAt"]) && isString(liveness.Properties["correlationId"])
}

func responseHasSchema(path pathItem, code, name string) bool {
	return path.Get.Responses[code].Content["application/json"].Schema.Ref == "#/components/schemas/"+name
}
func closedObject(value schema, required []string) bool {
	return value.Type == "object" && value.AdditionalProperties != nil && !*value.AdditionalProperties && sameSet(value.Required, required)
}
func stringEnum(value schema, values []string) bool {
	return isString(value) && sameSet(value.Enum, values)
}
func isString(value schema) bool { return value.Type == "string" }
func dateTime(value schema) bool { return isString(value) && value.Format == "date-time" }

type contractDocument struct {
	OpenAPI    string              `yaml:"openapi"`
	Paths      map[string]pathItem `yaml:"paths"`
	Components components          `yaml:"components"`
}
type pathItem struct {
	Get operation `yaml:"get"`
}
type operation struct {
	Responses map[string]response `yaml:"responses"`
}
type response struct {
	Content map[string]mediaType `yaml:"content"`
}
type mediaType struct {
	Schema schema `yaml:"schema"`
}
type components struct {
	Schemas map[string]schema `yaml:"schemas"`
}
type schema struct {
	Ref                  string            `yaml:"$ref"`
	Type                 string            `yaml:"type"`
	Format               string            `yaml:"format"`
	Properties           map[string]schema `yaml:"properties"`
	Required             []string          `yaml:"required"`
	AdditionalProperties *bool             `yaml:"additionalProperties"`
	Enum                 []string          `yaml:"enum"`
	MaxLength            *int              `yaml:"maxLength"`
}
