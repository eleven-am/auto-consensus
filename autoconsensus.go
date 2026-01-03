package autoconsensus

import "github.com/eleven-am/auto-consensus/internal/node"

type Node = node.Node
type Config = node.Config
type Storages = node.Storages
type StorageFactory = node.StorageFactory
type State = node.State
type StateChange = node.StateChange
type Discoverer = node.Discoverer
type MDNSConfig = node.MDNSConfig
type DNSConfig = node.DNSConfig
type MemberInfo = node.MemberInfo
type MemberState = node.MemberState

var (
	ErrMissingNodeID  = node.ErrMissingNodeID
	ErrMissingFactory = node.ErrMissingFactory
	ErrMissingFSM     = node.ErrMissingFSM
)

const (
	StateInit             = node.StateInit
	StateMDNSAdvertiserUp = node.StateMDNSAdvertiserUp
	StateGossipUp         = node.StateGossipUp
	StateDiscovering      = node.StateDiscovering
	StateJoining          = node.StateJoining
	StateBootstrapping    = node.StateBootstrapping
	StateRetrying         = node.StateRetrying
	StateRunning          = node.StateRunning
	StateFailed           = node.StateFailed
)

const (
	MemberStateAlive   = node.MemberStateAlive
	MemberStateSuspect = node.MemberStateSuspect
	MemberStateDead    = node.MemberStateDead
	MemberStateLeft    = node.MemberStateLeft
)

func New(cfg Config) (*Node, error) {
	return node.New(cfg)
}

func NewMDNSDiscovery(cfg MDNSConfig) Discoverer {
	return node.NewMDNSDiscovery(cfg)
}

func NewDNSDiscovery(cfg DNSConfig) Discoverer {
	return node.NewDNSDiscovery(cfg)
}

func NewStaticDiscovery(peers []string) Discoverer {
	return node.NewStaticDiscovery(peers)
}

func NewCascade(discoverers ...Discoverer) Discoverer {
	return node.NewCascade(discoverers...)
}
