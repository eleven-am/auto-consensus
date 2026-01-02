package gossip

import (
	"strings"
	"sync"

	"github.com/hashicorp/memberlist"
)

type delegate struct {
	advertiseAddr string
	raftAddr      string
	bootstrapped  bool
	mu            sync.RWMutex
}

func newDelegate(advertiseAddr, raftAddr string) *delegate {
	return &delegate{
		advertiseAddr: advertiseAddr,
		raftAddr:      raftAddr,
	}
}

func (d *delegate) SetBootstrapped(bootstrapped bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bootstrapped = bootstrapped
}

func (d *delegate) NodeMeta(limit int) []byte {
	d.mu.RLock()
	defer d.mu.RUnlock()

	meta := d.advertiseAddr + "|raft=" + d.raftAddr
	if d.bootstrapped {
		meta += "|bootstrapped"
	}
	if len(meta) > limit {
		return []byte(meta[:limit])
	}
	return []byte(meta)
}

func (d *delegate) NotifyMsg([]byte) {}

func (d *delegate) GetBroadcasts(overhead, limit int) [][]byte {
	return nil
}

func (d *delegate) LocalState(join bool) []byte {
	return nil
}

func (d *delegate) MergeRemoteState(buf []byte, join bool) {}

type eventDelegate struct {
	handler EventHandler
}

func newEventDelegate(handler EventHandler) *eventDelegate {
	if handler == nil {
		handler = NoopEventHandler{}
	}
	return &eventDelegate{handler: handler}
}

func (e *eventDelegate) NotifyJoin(node *memberlist.Node) {
	e.handler.OnJoin(nodeToInfo(node))
}

func (e *eventDelegate) NotifyLeave(node *memberlist.Node) {
	e.handler.OnLeave(nodeToInfo(node))
}

func (e *eventDelegate) NotifyUpdate(node *memberlist.Node) {
	e.handler.OnUpdate(nodeToInfo(node))
}

func nodeToInfo(node *memberlist.Node) NodeInfo {
	meta := string(node.Meta)
	address := ""
	raftAddr := ""
	bootstrapped := false

	parts := strings.Split(meta, "|")
	for i, part := range parts {
		if i == 0 {
			address = part
		} else if strings.HasPrefix(part, "raft=") {
			raftAddr = strings.TrimPrefix(part, "raft=")
		} else if part == "bootstrapped" {
			bootstrapped = true
		}
	}

	return NodeInfo{
		ID:           node.Name,
		Address:      address,
		RaftAddr:     raftAddr,
		Bootstrapped: bootstrapped,
	}
}
