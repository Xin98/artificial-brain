package contract

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTodoCompletedSinceContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "openapi", "todo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]struct {
			Get struct {
				Parameters []struct {
					Name     string    `yaml:"name"`
					In       string    `yaml:"in"`
					Required bool      `yaml:"required"`
					Schema   docSchema `yaml:"schema"`
				} `yaml:"parameters"`
			} `yaml:"get"`
		} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, parameter := range doc.Paths["/api/v1/todos"].Get.Parameters {
		if parameter.Name == "completedSince" {
			if parameter.In != "query" || parameter.Required || !docDateTime(parameter.Schema) {
				t.Fatalf("completedSince parameter = %#v", parameter)
			}
			return
		}
	}
	t.Fatal("missing optional completedSince UTC query parameter")
}
