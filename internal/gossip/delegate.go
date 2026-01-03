package gossip

import (
	"encoding/base64"
	"strings"
	"sync"

	"github.com/hashicorp/memberlist"
)

type delegate struct {
	advertiseAddr string
	raftAddr      string
	grpcAddr      string
	bootstrapped  bool
	appMeta       []byte
	broadcasts    *memberlist.TransmitLimitedQueue
	onAppMessage  func(from string, msg []byte)
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

func (d *delegate) SetGRPCAddr(addr string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.grpcAddr = addr
}

func (d *delegate) SetRaftAddr(addr string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.raftAddr = addr
}

func (d *delegate) SetAppMeta(meta []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.appMeta = meta
}

func (d *delegate) SetOnAppMessage(fn func(from string, msg []byte)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onAppMessage = fn
}

func (d *delegate) SetBroadcastQueue(q *memberlist.TransmitLimitedQueue) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.broadcasts = q
}

func (d *delegate) NodeMeta(limit int) []byte {
	d.mu.RLock()
	defer d.mu.RUnlock()

	meta := d.advertiseAddr
	if d.raftAddr != "" {
		meta += "|raft=" + d.raftAddr
	}
	if d.grpcAddr != "" {
		meta += "|grpc=" + d.grpcAddr
	}
	if d.bootstrapped {
		meta += "|bootstrapped"
	}
	if len(d.appMeta) > 0 {
		meta += "|app=" + base64.StdEncoding.EncodeToString(d.appMeta)
	}
	if len(meta) > limit {
		return []byte(meta[:limit])
	}
	return []byte(meta)
}

func (d *delegate) NotifyMsg(msg []byte) {
	if len(msg) == 0 {
		return
	}
	d.mu.RLock()
	handler := d.onAppMessage
	d.mu.RUnlock()

	if handler != nil && msg[0] == 0x01 && len(msg) > 1 {
		parts := strings.SplitN(string(msg[1:]), "|", 2)
		if len(parts) == 2 {
			handler(parts[0], []byte(parts[1]))
		}
	}
}

func (d *delegate) GetBroadcasts(overhead, limit int) [][]byte {
	d.mu.RLock()
	q := d.broadcasts
	d.mu.RUnlock()

	if q == nil {
		return nil
	}
	return q.GetBroadcasts(overhead, limit)
}

func (d *delegate) QueueBroadcast(nodeID string, msg []byte) {
	d.mu.RLock()
	q := d.broadcasts
	d.mu.RUnlock()

	if q == nil {
		return
	}
	payload := append([]byte{0x01}, []byte(nodeID+"|")...)
	payload = append(payload, msg...)
	q.QueueBroadcast(&appBroadcast{msg: payload})
}

type appBroadcast struct {
	msg []byte
}

func (b *appBroadcast) Invalidates(other memberlist.Broadcast) bool {
	return false
}

func (b *appBroadcast) Message() []byte {
	return b.msg
}

func (b *appBroadcast) Finished() {}

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
	grpcAddr := ""
	bootstrapped := false
	var appMeta []byte

	parts := strings.Split(meta, "|")
	for i, part := range parts {
		if i == 0 {
			address = part
		} else if strings.HasPrefix(part, "raft=") {
			raftAddr = strings.TrimPrefix(part, "raft=")
		} else if strings.HasPrefix(part, "grpc=") {
			grpcAddr = strings.TrimPrefix(part, "grpc=")
		} else if strings.HasPrefix(part, "app=") {
			encoded := strings.TrimPrefix(part, "app=")
			if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
				appMeta = decoded
			}
		} else if part == "bootstrapped" {
			bootstrapped = true
		}
	}

	var state NodeState
	switch node.State {
	case memberlist.StateAlive:
		state = NodeStateAlive
	case memberlist.StateSuspect:
		state = NodeStateSuspect
	case memberlist.StateDead:
		state = NodeStateDead
	case memberlist.StateLeft:
		state = NodeStateLeft
	default:
		state = NodeStateAlive
	}

	return NodeInfo{
		ID:           node.Name,
		Address:      address,
		RaftAddr:     raftAddr,
		GRPCAddr:     grpcAddr,
		Bootstrapped: bootstrapped,
		State:        state,
		AppMeta:      appMeta,
	}
}
