package autoconsensus

import "github.com/eleven-am/auto-consensus/internal/bootstrap"

type State = bootstrap.State

type StateChange struct {
	Old State
	New State
}

const (
	StateInit             = bootstrap.StateInit
	StateMDNSAdvertiserUp = bootstrap.StateMDNSAdvertiserUp
	StateGossipUp         = bootstrap.StateGossipUp
	StateDiscovering      = bootstrap.StateDiscovering
	StateJoining          = bootstrap.StateJoining
	StateBootstrapping    = bootstrap.StateBootstrapping
	StateRetrying         = bootstrap.StateRetrying
	StateRunning          = bootstrap.StateRunning
	StateFailed           = bootstrap.StateFailed
)
