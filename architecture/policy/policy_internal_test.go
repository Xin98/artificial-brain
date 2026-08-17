package policy

import (
	"go/build"
	"os"
	"path/filepath"
	"testing"
)

func TestIsStandardLibraryRejectsResolvableNonGOROOTPackage(t *testing.T) {
	workspace := t.TempDir()
	packageDirectory := filepath.Join(workspace, "src", "example.com", "thirdparty")
	if err := os.MkdirAll(packageDirectory, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDirectory, "external.go"), []byte("package external\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	context := build.Default
	context.GOPATH = workspace
	if isStandardLibraryWithContext(context, "example.com/thirdparty") {
		t.Fatal("isStandardLibrary() accepted a resolvable package outside GOROOT")
	}
	if !isStandardLibraryWithContext(context, "fmt") {
		t.Fatal("isStandardLibrary() rejected fmt")
	}
}
