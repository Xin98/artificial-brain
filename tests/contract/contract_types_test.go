package contract

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// docDocument and friends parse the ITER-0002 contracts. They are separate
// from the system-health contract types so that contract and its test stay
// untouched.
type docDocument struct {
	OpenAPI    string                 `yaml:"openapi"`
	Paths      map[string]docPathItem `yaml:"paths"`
	Components docComponents          `yaml:"components"`
}

type docPathItem struct {
	Get   docOperation `yaml:"get"`
	Post  docOperation `yaml:"post"`
	Patch docOperation `yaml:"patch"`
}

type docOperation struct {
	Description string                 `yaml:"description"`
	Responses   map[string]docResponse `yaml:"responses"`
	RequestBody *docRequestBody        `yaml:"requestBody"`
}

type docRequestBody struct {
	Content map[string]docMediaType `yaml:"content"`
}

type docResponse struct {
	Content map[string]docMediaType `yaml:"content"`
}

type docMediaType struct {
	Schema docSchema `yaml:"schema"`
}

type docComponents struct {
	Schemas map[string]docSchema `yaml:"schemas"`
}

type docSchema struct {
	Ref                  string               `yaml:"$ref"`
	Type                 string               `yaml:"type"`
	Format               string               `yaml:"format"`
	Properties           map[string]docSchema `yaml:"properties"`
	Required             []string             `yaml:"required"`
	AdditionalProperties *bool                `yaml:"additionalProperties"`
	Enum                 []string             `yaml:"enum"`
	MaxLength            *int                 `yaml:"maxLength"`
	Items                *docSchema           `yaml:"items"`
}

func loadDoc(t *testing.T, file string) docDocument {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "contracts", "openapi", file))
	if err != nil {
		t.Fatal(err)
	}
	var document docDocument
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func opFor(item docPathItem, method string) docOperation {
	switch method {
	case "get":
		return item.Get
	case "post":
		return item.Post
	case "patch":
		return item.Patch
	}
	return docOperation{}
}

// assertDocRoutes checks every route's response code set and schema refs,
// plus the request body ref when body is non-empty.
func assertDocRoutes(t *testing.T, document docDocument, routes []struct {
	path    string
	method  string
	schemas map[string]string
	body    string
}) {
	t.Helper()
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
			if got := operation.Responses[code].Content["application/json"].Schema.Ref; got != "#/components/schemas/"+schema {
				t.Fatalf("%s %s response %s schema = %q, want %s", route.method, route.path, code, got, schema)
			}
		}
		if route.body != "" {
			if operation.RequestBody == nil {
				t.Fatalf("%s %s has no request body", route.method, route.path)
			}
			if got := operation.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/"+route.body {
				t.Fatalf("%s %s request body schema = %q, want %s", route.method, route.path, got, route.body)
			}
		}
	}
}

func docClosedObject(value docSchema, required []string) bool {
	return value.Type == "object" && value.AdditionalProperties != nil && !*value.AdditionalProperties && sameSet(value.Required, required)
}

func docPropertiesAre(value docSchema, properties []string) bool {
	return sameSet(mapKeys(value.Properties), properties)
}

func docStringEnum(value docSchema, values []string) bool {
	return value.Type == "string" && sameSet(value.Enum, values)
}

func docDateTime(value docSchema) bool {
	return value.Type == "string" && value.Format == "date-time"
}

func docIsString(value docSchema) bool { return value.Type == "string" }

func docIsInteger(value docSchema) bool { return value.Type == "integer" }

func docIsBoolean(value docSchema) bool { return value.Type == "boolean" }

func docMaxLength(value docSchema, limit int) bool {
	return value.MaxLength != nil && *value.MaxLength == limit
}

func docArrayOfRef(value docSchema, ref string) bool {
	return value.Type == "array" && value.Items != nil && value.Items.Ref == "#/components/schemas/"+ref
}

func docErrorEnvelope(value docSchema) bool {
	return docClosedObject(value, []string{"code", "message", "correlationId"}) &&
		docIsString(value.Properties["code"]) &&
		docIsString(value.Properties["message"]) &&
		docIsString(value.Properties["correlationId"])
}
