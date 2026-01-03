package node

import (
	"github.com/eleven-am/auto-consensus/internal/bootstrap"
	"github.com/eleven-am/auto-consensus/internal/gossip"
)

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

type MemberState int

const (
	MemberStateAlive MemberState = iota
	MemberStateSuspect
	MemberStateDead
	MemberStateLeft
)

type MemberInfo struct {
	ID      string
	Address string
	AppMeta []byte
	State   MemberState
}

func memberInfoFromGossip(info gossip.NodeInfo) MemberInfo {
	return MemberInfo{
		ID:      info.ID,
		Address: info.Address,
		AppMeta: info.AppMeta,
		State:   MemberState(info.State),
	}
}
