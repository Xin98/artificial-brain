package main

import "testing"

func TestRunReturnsFailureForMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if got := run(); got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
}
