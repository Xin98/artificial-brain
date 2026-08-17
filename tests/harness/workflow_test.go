package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type workflow struct {
	Jobs map[string]struct {
		Steps []struct {
			Run  string `yaml:"run"`
			Uses string `yaml:"uses"`
			With struct {
				Path string `yaml:"path"`
			} `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestWorkflowRunsUnifiedVerificationInOrder(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "workflows", "ci.yml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}

	commands, err := executableRunCommands(contents)
	if err != nil {
		t.Fatalf("parse CI workflow: %v", err)
	}

	want := [][]string{{"make", "verify"}, {"make", "migration-test"}, {"make", "smoke-test"}}
	position := -1
	for _, invocation := range want {
		found := false
		for index := position + 1; index < len(commands); index++ {
			fields := strings.Fields(commands[index])
			if len(fields) >= len(invocation) && fields[0] == invocation[0] && fields[1] == invocation[1] {
				position = index
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("workflow executable steps %q do not run %q after prior gates", commands, strings.Join(invocation, " "))
		}
	}
}

func TestWorkflowRedactsBeforeUploadingAndNeverUploadsRawLogs(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "workflows", "ci.yml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}

	var parsed workflow
	if err := yaml.Unmarshal(contents, &parsed); err != nil {
		t.Fatalf("parse CI workflow: %v", err)
	}

	for _, job := range parsed.Jobs {
		redactionStep := -1
		removalStep := -1
		uploadStep := -1
		for index, step := range job.Steps {
			for _, command := range executableLines(step.Run) {
				switch command {
				case "sh scripts/redact-logs.sh <.artifacts/smoke-raw.log >.artifacts/smoke-diagnostics.tmp":
					redactionStep = index
				case "rm .artifacts/smoke-raw.log":
					removalStep = index
				}
			}
			if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
				uploadStep = index
				if step.With.Path != ".artifacts/smoke-diagnostics.log" {
					t.Fatalf("artifact path = %q, want only redacted diagnostics", step.With.Path)
				}
			}
		}

		if redactionStep >= 0 && removalStep >= redactionStep && uploadStep > removalStep {
			return
		}
	}

	t.Fatal("workflow must invoke the shared redactor, remove raw logs, then upload only redacted diagnostics")
}

func TestWorkflowIgnoresCommentsAndUnrelatedScalars(t *testing.T) {
	contents := []byte(`
name: make verify
jobs:
  test:
    description: make migration-test
    steps:
      - run: |
          # make smoke-test
          echo ready
`)

	commands, err := executableRunCommands(contents)
	if err != nil {
		t.Fatalf("parse workflow fixture: %v", err)
	}
	if strings.Join(commands, "\n") != "echo ready" {
		t.Fatalf("executable commands = %q, want only echo ready", commands)
	}
}

func executableRunCommands(contents []byte) ([]string, error) {
	var parsed workflow
	if err := yaml.Unmarshal(contents, &parsed); err != nil {
		return nil, err
	}

	var commands []string
	for _, job := range parsed.Jobs {
		for _, step := range job.Steps {
			commands = append(commands, executableLines(step.Run)...)
		}
	}
	return commands, nil
}

func executableLines(run string) []string {
	var commands []string
	for _, line := range strings.Split(run, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		commands = append(commands, line)
	}
	return commands
}
