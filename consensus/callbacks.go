package consensus

type NodeInfo struct {
	ID      string
	RaftAddr string
}

type RaftCallbacks interface {
	OnBootstrap(self NodeInfo) error
	OnJoin(self NodeInfo, peers []NodeInfo) error
	OnPeerJoin(peer NodeInfo)
	OnPeerLeave(peer NodeInfo)
}

type noopCallbacks struct{}

func (noopCallbacks) OnBootstrap(NodeInfo) error              { return nil }
func (noopCallbacks) OnJoin(NodeInfo, []NodeInfo) error       { return nil }
func (noopCallbacks) OnPeerJoin(NodeInfo)                     {}
func (noopCallbacks) OnPeerLeave(NodeInfo)                    {}

func NoopCallbacks() RaftCallbacks {
	return noopCallbacks{}
}
