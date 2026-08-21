package contract

import (
	"strings"
	"testing"
)

// Import lifecycle vocabulary: the states an import row renders and the
// outcome and kind one preview/report decision line can carry.
var (
	importStates     = []string{"pending", "committed", "expired"}
	decisionKinds    = []string{"todo", "channel", "delivery"}
	decisionOutcomes = []string{"new", "skipped", "conflict", "invalid"}
)

// portabilityRoutes is the route table the portability contract documents.
// It extends the reminder table's conventions in two ways the shared
// assertDocRoutes harness predates: an empty schema name marks a response
// with no JSON body — the export download answers raw application/zip
// bytes — and a non-empty body names a multipart/form-data schema, since
// the upload carries the bundle as a form file.
func portabilityRoutes() []struct {
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
		{"/api/v1/portability/export", "post",
			map[string]string{"200": "", "401": "ErrorEnvelope"}, ""},
		{"/api/v1/portability/imports", "post",
			map[string]string{"201": "UploadAccepted", "401": "ErrorEnvelope", "422": "ErrorEnvelope"}, "UploadRequest"},
		{"/api/v1/portability/imports/{importId}", "get",
			map[string]string{"200": "ImportView", "401": "ErrorEnvelope", "404": "ErrorEnvelope"}, ""},
		{"/api/v1/portability/imports/{importId}/confirm", "post",
			map[string]string{"200": "ImportReport", "401": "ErrorEnvelope", "404": "ErrorEnvelope", "409": "ErrorEnvelope"}, ""},
	}
}

func TestPortabilityContractRoutesCodesAndSchemas(t *testing.T) {
	document := loadDoc(t, "portability.yaml")
	if document.OpenAPI != "3.1.1" {
		t.Fatalf("openapi = %q, want 3.1.1", document.OpenAPI)
	}
	assertPortabilityRoutes(t, document)

	// The export is a zip download; its description must name the
	// Content-Disposition attachment filename.
	export := document.Paths["/api/v1/portability/export"].Post
	if !strings.Contains(export.Description, "Content-Disposition") ||
		!strings.Contains(export.Description, "artificial-brain-export-") {
		t.Fatalf("export description = %q, want Content-Disposition attachment filename", export.Description)
	}
	// The upload documents its multipart form field and every 422 code.
	upload := document.Paths["/api/v1/portability/imports"].Post
	for _, wanted := range []string{"multipart/form-data", "bundle", "bundle_too_large", "bundle_invalid", "unsupported_schema_version", "checksum_mismatch"} {
		if !strings.Contains(upload.Description, wanted) {
			t.Fatalf("upload description = %q, want %q", upload.Description, wanted)
		}
	}
	// Committed and expired confirms share the import_conflict code.
	confirm := document.Paths["/api/v1/portability/imports/{importId}/confirm"].Post
	if !strings.Contains(confirm.Description, "import_conflict") {
		t.Fatalf("confirm description = %q, want import_conflict", confirm.Description)
	}

	schemas := document.Components.Schemas
	assertExactSet(t, "schemas", mapKeys(schemas), []string{
		"ErrorEnvelope", "UploadRequest", "UploadAccepted", "ImportDecision",
		"ImportPreview", "ImportReport", "ImportView",
	})
	if !docErrorEnvelope(schemas["ErrorEnvelope"]) {
		t.Fatalf("ErrorEnvelope = %#v", schemas["ErrorEnvelope"])
	}

	uploadRequest := schemas["UploadRequest"]
	if !docClosedObject(uploadRequest, []string{"bundle"}) || !docPropertiesAre(uploadRequest, []string{"bundle"}) ||
		!docIsString(uploadRequest.Properties["bundle"]) || uploadRequest.Properties["bundle"].Format != "binary" {
		t.Fatalf("UploadRequest = %#v", uploadRequest)
	}

	accepted := schemas["UploadAccepted"]
	if !docClosedObject(accepted, []string{"importId", "preview"}) || !docPropertiesAre(accepted, []string{"importId", "preview"}) ||
		!docIsString(accepted.Properties["importId"]) ||
		accepted.Properties["preview"].Ref != "#/components/schemas/ImportPreview" {
		t.Fatalf("UploadAccepted = %#v", accepted)
	}

	decision := schemas["ImportDecision"]
	decisionFields := []string{"kind", "sourceRecordId", "outcome", "reason"}
	if !docClosedObject(decision, decisionFields) || !docPropertiesAre(decision, decisionFields) ||
		!docStringEnum(decision.Properties["kind"], decisionKinds) ||
		!docIsString(decision.Properties["sourceRecordId"]) ||
		!docStringEnum(decision.Properties["outcome"], decisionOutcomes) ||
		!docIsString(decision.Properties["reason"]) {
		t.Fatalf("ImportDecision = %#v", decision)
	}

	counts := []string{"new", "skipped", "conflicts", "invalid"}
	preview := schemas["ImportPreview"]
	previewFields := append(append([]string{}, counts...), "details", "truncated")
	if !docClosedObject(preview, previewFields) || !docPropertiesAre(preview, previewFields) {
		t.Fatalf("ImportPreview = %#v", preview)
	}
	for _, count := range counts {
		if !docIsInteger(preview.Properties[count]) {
			t.Fatalf("ImportPreview.%s = %#v, want integer", count, preview.Properties[count])
		}
	}
	if !docArrayOfRef(preview.Properties["details"], "ImportDecision") || !docIsBoolean(preview.Properties["truncated"]) {
		t.Fatalf("ImportPreview details/truncated = %#v", preview.Properties)
	}

	report := schemas["ImportReport"]
	reportFields := append(append([]string{}, counts...), "details", "truncated", "committedAt")
	if !docClosedObject(report, reportFields) || !docPropertiesAre(report, reportFields) {
		t.Fatalf("ImportReport = %#v", report)
	}
	for _, count := range counts {
		if !docIsInteger(report.Properties[count]) {
			t.Fatalf("ImportReport.%s = %#v, want integer", count, report.Properties[count])
		}
	}
	if !docArrayOfRef(report.Properties["details"], "ImportDecision") || !docIsBoolean(report.Properties["truncated"]) ||
		!docDateTime(report.Properties["committedAt"]) {
		t.Fatalf("ImportReport details/truncated/committedAt = %#v", report.Properties)
	}

	view := schemas["ImportView"]
	required := []string{"importId", "state", "sourceInstanceId", "preview", "createdAt"}
	optional := []string{"report", "committedAt"}
	if !docClosedObject(view, required) || !docPropertiesAre(view, append(append([]string{}, required...), optional...)) {
		t.Fatalf("ImportView = %#v", view)
	}
	if !docIsString(view.Properties["importId"]) ||
		!docStringEnum(view.Properties["state"], importStates) ||
		!docIsString(view.Properties["sourceInstanceId"]) ||
		view.Properties["preview"].Ref != "#/components/schemas/ImportPreview" ||
		view.Properties["report"].Ref != "#/components/schemas/ImportReport" ||
		!docDateTime(view.Properties["createdAt"]) || !docDateTime(view.Properties["committedAt"]) {
		t.Fatalf("ImportView properties = %#v", view.Properties)
	}
}

func TestPortabilityContractRejectsMutation(t *testing.T) {
	document := loadDoc(t, "portability.yaml")
	if !portabilityContractValid(document) {
		t.Fatal("shipped portability contract failed its own validator")
	}
	// Widening the import state enum must fail.
	widened := loadDoc(t, "portability.yaml")
	view := widened.Components.Schemas["ImportView"]
	view.Properties["state"] = docSchema{Type: "string", Enum: append(append([]string{}, importStates...), "cancelled")}
	widened.Components.Schemas["ImportView"] = view
	if portabilityContractValid(widened) {
		t.Fatal("mutation (widened state enum) unexpectedly passed validation")
	}
	// Dropping a required preview field must fail.
	dropped := loadDoc(t, "portability.yaml")
	preview := dropped.Components.Schemas["ImportPreview"]
	preview.Required = []string{"new", "skipped", "conflicts", "invalid", "details"}
	dropped.Components.Schemas["ImportPreview"] = preview
	if portabilityContractValid(dropped) {
		t.Fatal("mutation (truncated no longer required) unexpectedly passed validation")
	}
}

// assertPortabilityRoutes mirrors assertDocRoutes with the two portability
// extensions: an empty schema name in the route table requires the response
// to carry application/zip binary content and no JSON body, and a non-empty
// body is checked against multipart/form-data instead of application/json.
func assertPortabilityRoutes(t *testing.T, document docDocument) {
	t.Helper()
	routes := portabilityRoutes()
	paths := make([]string, 0, len(routes))
	seen := make(map[string]bool, len(routes))
	for _, route := range routes {
		if !seen[route.path] {
			seen[route.path] = true
			paths = append(paths, route.path)
		}
	}
	assertExactSet(t, "paths", mapKeys(document.Paths), paths)
	for _, route := range routes {
		item, ok := document.Paths[route.path]
		if !ok {
			t.Fatalf("%s is missing", route.path)
		}
		operation := opFor(item, route.method)
		codes := make([]string, 0, len(route.schemas))
		for code := range route.schemas {
			codes = append(codes, code)
		}
		assertExactSet(t, route.method+" "+route.path+" responses", mapKeys(operation.Responses), codes)
		for code, schema := range route.schemas {
			response := operation.Responses[code]
			if schema == "" {
				if _, hasJSON := response.Content["application/json"]; hasJSON {
					t.Fatalf("%s %s response %s carries a JSON body, want none", route.method, route.path, code)
				}
				zipMedia, hasZip := response.Content["application/zip"]
				if !hasZip || zipMedia.Schema.Type != "string" || zipMedia.Schema.Format != "binary" {
					t.Fatalf("%s %s response %s content = %#v, want application/zip binary", route.method, route.path, code, response.Content)
				}
				continue
			}
			if got := response.Content["application/json"].Schema.Ref; got != "#/components/schemas/"+schema {
				t.Fatalf("%s %s response %s schema = %q, want %s", route.method, route.path, code, got, schema)
			}
		}
		if route.body != "" {
			if operation.RequestBody == nil {
				t.Fatalf("%s %s has no request body", route.method, route.path)
			}
			if got := operation.RequestBody.Content["multipart/form-data"].Schema.Ref; got != "#/components/schemas/"+route.body {
				t.Fatalf("%s %s request body schema = %q, want %s", route.method, route.path, got, route.body)
			}
		}
	}
}

// portabilityContractValid mirrors the route and schema assertions of
// TestPortabilityContractRoutesCodesAndSchemas in boolean form so the
// mutation test can replay mutated documents through it.
func portabilityContractValid(document docDocument) bool {
	if document.OpenAPI != "3.1.1" {
		return false
	}
	paths := []string{}
	for _, route := range portabilityRoutes() {
		item, ok := document.Paths[route.path]
		if !ok {
			return false
		}
		paths = append(paths, route.path)
		operation := opFor(item, route.method)
		if !sameSet(mapKeys(operation.Responses), mapKeys(route.schemas)) {
			return false
		}
		for code, schema := range route.schemas {
			response := operation.Responses[code]
			if schema == "" {
				if _, hasJSON := response.Content["application/json"]; hasJSON {
					return false
				}
				zipMedia, hasZip := response.Content["application/zip"]
				if !hasZip || zipMedia.Schema.Type != "string" || zipMedia.Schema.Format != "binary" {
					return false
				}
				continue
			}
			if response.Content["application/json"].Schema.Ref != "#/components/schemas/"+schema {
				return false
			}
		}
		if route.body != "" {
			if operation.RequestBody == nil ||
				operation.RequestBody.Content["multipart/form-data"].Schema.Ref != "#/components/schemas/"+route.body {
				return false
			}
		}
	}
	if !sameSet(mapKeys(document.Paths), paths) {
		return false
	}
	schemas := document.Components.Schemas
	if !docErrorEnvelope(schemas["ErrorEnvelope"]) {
		return false
	}
	uploadRequest := schemas["UploadRequest"]
	if !docClosedObject(uploadRequest, []string{"bundle"}) ||
		!docIsString(uploadRequest.Properties["bundle"]) || uploadRequest.Properties["bundle"].Format != "binary" {
		return false
	}
	accepted := schemas["UploadAccepted"]
	if !docClosedObject(accepted, []string{"importId", "preview"}) ||
		!docIsString(accepted.Properties["importId"]) ||
		accepted.Properties["preview"].Ref != "#/components/schemas/ImportPreview" {
		return false
	}
	decision := schemas["ImportDecision"]
	if !docClosedObject(decision, []string{"kind", "sourceRecordId", "outcome", "reason"}) ||
		!docStringEnum(decision.Properties["kind"], decisionKinds) ||
		!docIsString(decision.Properties["sourceRecordId"]) ||
		!docStringEnum(decision.Properties["outcome"], decisionOutcomes) ||
		!docIsString(decision.Properties["reason"]) {
		return false
	}
	counts := []string{"new", "skipped", "conflicts", "invalid"}
	preview := schemas["ImportPreview"]
	if !docClosedObject(preview, append(append([]string{}, counts...), "details", "truncated")) ||
		!docArrayOfRef(preview.Properties["details"], "ImportDecision") ||
		!docIsBoolean(preview.Properties["truncated"]) {
		return false
	}
	for _, count := range counts {
		if !docIsInteger(preview.Properties[count]) {
			return false
		}
	}
	report := schemas["ImportReport"]
	if !docClosedObject(report, append(append([]string{}, counts...), "details", "truncated", "committedAt")) ||
		!docArrayOfRef(report.Properties["details"], "ImportDecision") ||
		!docIsBoolean(report.Properties["truncated"]) ||
		!docDateTime(report.Properties["committedAt"]) {
		return false
	}
	for _, count := range counts {
		if !docIsInteger(report.Properties[count]) {
			return false
		}
	}
	view := schemas["ImportView"]
	return docClosedObject(view, []string{"importId", "state", "sourceInstanceId", "preview", "createdAt"}) &&
		sameSet(mapKeys(view.Properties), []string{"importId", "state", "sourceInstanceId", "preview", "report", "createdAt", "committedAt"}) &&
		docIsString(view.Properties["importId"]) &&
		docStringEnum(view.Properties["state"], importStates) &&
		docIsString(view.Properties["sourceInstanceId"]) &&
		view.Properties["preview"].Ref == "#/components/schemas/ImportPreview" &&
		view.Properties["report"].Ref == "#/components/schemas/ImportReport" &&
		docDateTime(view.Properties["createdAt"]) &&
		docDateTime(view.Properties["committedAt"])
}
