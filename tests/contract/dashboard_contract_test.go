package contract

import "testing"

var dashboardCounters = []string{
	"pendingTotal", "dueToday", "overdue", "noDue",
	"completedLast7Days", "reminderRetrying", "reminderFailed",
}

func TestDashboardContractRoutesCodesAndSchemas(t *testing.T) {
	document := loadDoc(t, "dashboard.yaml")
	if document.OpenAPI != "3.1.1" {
		t.Fatalf("openapi = %q, want 3.1.1", document.OpenAPI)
	}
	assertExactSet(t, "paths", mapKeys(document.Paths), []string{"/api/v1/dashboard/summary"})
	path := document.Paths["/api/v1/dashboard/summary"]
	assertExactSet(t, "GET /api/v1/dashboard/summary responses", mapKeys(path.Get.Responses), []string{"200", "401", "422"})
	if got := path.Get.Responses["200"].Content["application/json"].Schema.Ref; got != "#/components/schemas/DashboardSummary" {
		t.Fatalf("200 schema = %q", got)
	}
	for _, code := range []string{"401", "422"} {
		if got := path.Get.Responses[code].Content["application/json"].Schema.Ref; got != "#/components/schemas/ErrorEnvelope" {
			t.Fatalf("%s schema = %q", code, got)
		}
	}

	schemas := document.Components.Schemas
	if !docErrorEnvelope(schemas["ErrorEnvelope"]) {
		t.Fatalf("ErrorEnvelope = %#v", schemas["ErrorEnvelope"])
	}
	summary := schemas["DashboardSummary"]
	if !docClosedObject(summary, append([]string{}, append(dashboardCounters, "checkedAt")...)) ||
		!docPropertiesAre(summary, append(dashboardCounters, "checkedAt")) {
		t.Fatalf("DashboardSummary = %#v", summary)
	}
	for _, counter := range dashboardCounters {
		if !docIsInteger(summary.Properties[counter]) {
			t.Fatalf("DashboardSummary.%s = %#v, want integer", counter, summary.Properties[counter])
		}
	}
	if !docDateTime(summary.Properties["checkedAt"]) {
		t.Fatalf("DashboardSummary.checkedAt = %#v", summary.Properties["checkedAt"])
	}
}

func TestDashboardContractRejectsMutation(t *testing.T) {
	document := loadDoc(t, "dashboard.yaml")
	if !dashboardContractValid(document) {
		t.Fatal("shipped dashboard contract failed its own validator")
	}
	// Dropping the deterministic reminder-failed counter must fail.
	mutated := loadDoc(t, "dashboard.yaml")
	summary := mutated.Components.Schemas["DashboardSummary"]
	summary.Required = dashboardCounters
	summary.Properties = map[string]docSchema{}
	for _, counter := range dashboardCounters {
		summary.Properties[counter] = docSchema{Type: "integer"}
	}
	summary.Properties["checkedAt"] = docSchema{Type: "string", Format: "date-time"}
	mutated.Components.Schemas["DashboardSummary"] = summary
	if dashboardContractValid(mutated) {
		t.Fatal("mutation (missing reminderFailed) unexpectedly passed validation")
	}
}

func dashboardContractValid(document docDocument) bool {
	if document.OpenAPI != "3.1.1" || !sameSet(mapKeys(document.Paths), []string{"/api/v1/dashboard/summary"}) {
		return false
	}
	path := document.Paths["/api/v1/dashboard/summary"]
	if !sameSet(mapKeys(path.Get.Responses), []string{"200", "401", "422"}) ||
		path.Get.Responses["200"].Content["application/json"].Schema.Ref != "#/components/schemas/DashboardSummary" ||
		path.Get.Responses["401"].Content["application/json"].Schema.Ref != "#/components/schemas/ErrorEnvelope" ||
		path.Get.Responses["422"].Content["application/json"].Schema.Ref != "#/components/schemas/ErrorEnvelope" {
		return false
	}
	schemas := document.Components.Schemas
	if !docErrorEnvelope(schemas["ErrorEnvelope"]) {
		return false
	}
	summary := schemas["DashboardSummary"]
	if !docClosedObject(summary, append(append([]string{}, dashboardCounters...), "checkedAt")) ||
		!docDateTime(summary.Properties["checkedAt"]) {
		return false
	}
	for _, counter := range dashboardCounters {
		if !docIsInteger(summary.Properties[counter]) {
			return false
		}
	}
	return true
}
