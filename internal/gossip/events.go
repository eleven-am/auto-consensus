package gossip

import "errors"

var (
	ErrMissingNodeID   = errors.New("node ID is required")
	ErrMissingBindAddr = errors.New("bind address is required")
	ErrNotStarted      = errors.New("gossip not started")
	ErrAlreadyStarted  = errors.New("gossip already started")
)

type NodeInfo struct {
	ID           string
	Address      string
	RaftAddr     string
	Bootstrapped bool
}

type EventHandler interface {
	OnJoin(node NodeInfo)
	OnLeave(node NodeInfo)
	OnUpdate(node NodeInfo)
}

type NoopEventHandler struct{}

func (NoopEventHandler) OnJoin(NodeInfo)   {}
func (NoopEventHandler) OnLeave(NodeInfo)  {}
func (NoopEventHandler) OnUpdate(NodeInfo) {}
