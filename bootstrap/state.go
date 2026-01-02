package bootstrap

type State int

const (
	StateInit State = iota
	StateMDNSAdvertiserUp
	StateGossipUp
	StateDiscovering
	StateJoining
	StateBootstrapping
	StateRetrying
	StateRunning
	StateFailed
)

func (s State) String() string {
	switch s {
	case StateInit:
		return "Init"
	case StateMDNSAdvertiserUp:
		return "MDNSAdvertiserUp"
	case StateGossipUp:
		return "GossipUp"
	case StateDiscovering:
		return "Discovering"
	case StateJoining:
		return "Joining"
	case StateBootstrapping:
		return "Bootstrapping"
	case StateRetrying:
		return "Retrying"
	case StateRunning:
		return "Running"
	case StateFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}
