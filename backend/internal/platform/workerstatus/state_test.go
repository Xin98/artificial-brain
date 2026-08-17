package workerstatus

import "testing"

func TestStateRequiresSuccessfulHeartbeat(t *testing.T) {
	var state State
	if state.Ready() {
		t.Fatal("new state must not be ready")
	}

	state.MarkHeartbeatSuccess()
	if !state.Ready() {
		t.Fatal("successful heartbeat must make state ready")
	}

	state.MarkHeartbeatFailure()
	if state.Ready() {
		t.Fatal("failed heartbeat must make state not ready")
	}
}
