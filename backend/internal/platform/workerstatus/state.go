package workerstatus

import "sync/atomic"

type State struct {
	heartbeatReady atomic.Bool
}

func (s *State) MarkHeartbeatSuccess() { s.heartbeatReady.Store(true) }

func (s *State) MarkHeartbeatFailure() { s.heartbeatReady.Store(false) }

func (s *State) Ready() bool { return s.heartbeatReady.Load() }
