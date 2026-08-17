package tests

import (
	"path/filepath"
	"testing"

	"github.com/Xin98/artificial-brain/architecture/policy"
)

func TestRepositoryHasNoArchitectureViolations(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}

	violations, err := policy.Validate(root)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("architecture violations:\n%v", violations)
	}
}
