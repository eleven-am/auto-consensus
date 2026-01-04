package raft

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/eleven-am/auto-consensus/internal/consensus"
	"github.com/eleven-am/auto-consensus/internal/gossip"
	"github.com/hashicorp/raft"
)

type mockConfigurationFuture struct {
	servers []raft.Server
	err     error
}

func (m *mockConfigurationFuture) Configuration() raft.Configuration {
	return raft.Configuration{Servers: m.servers}
}

func (m *mockConfigurationFuture) Error() error {
	return m.err
}

func (m *mockConfigurationFuture) Index() uint64 {
	return 0
}

type mockApplyFuture struct {
	err error
}

func (m *mockApplyFuture) Error() error {
	return m.err
}

func (m *mockApplyFuture) Response() interface{} {
	return nil
}

func (m *mockApplyFuture) Index() uint64 {
	return 0
}

type mockRaft struct {
	mu               sync.Mutex
	state            raft.RaftState
	configuration    []raft.Server
	addVoterCalls    []addVoterCall
	removeServerCalls []string
}

type addVoterCall struct {
	id   string
	addr string
}

func (m *mockRaft) State() raft.RaftState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *mockRaft) GetConfiguration() raft.ConfigurationFuture {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &mockConfigurationFuture{servers: m.configuration}
}

func (m *mockRaft) AddVoter(id raft.ServerID, addr raft.ServerAddress, prevIndex uint64, timeout time.Duration) raft.IndexFuture {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addVoterCalls = append(m.addVoterCalls, addVoterCall{id: string(id), addr: string(addr)})
	return &mockApplyFuture{}
}

func (m *mockRaft) RemoveServer(id raft.ServerID, prevIndex uint64, timeout time.Duration) raft.IndexFuture {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeServerCalls = append(m.removeServerCalls, string(id))
	return &mockApplyFuture{}
}

type mockGossip struct {
	members []gossip.NodeInfo
}

func (m *mockGossip) Members() []gossip.NodeInfo {
	return m.members
}

func newTestNode() *Node {
	return &Node{
		logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		config: Config{
			ReconcileInterval: time.Second,
		},
		stopCh: make(chan struct{}),
	}
}

func TestHandlePeerJoin_SameAddress_NoAction(t *testing.T) {
	node := newTestNode()
	node.selfInfo = consensus.NodeInfo{ID: "self"}

	mock := &mockRaft{
		state: raft.Leader,
		configuration: []raft.Server{
			{ID: "peer1", Address: "127.0.0.1:5000"},
		},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "peer1", RaftAddr: "127.0.0.1:5000", Bootstrapped: true},
		},
	}

	node.gossipFn = func() *gossip.Gossip {
		return nil
	}

	node.handlePeerJoinWithRaft(mock, consensus.NodeInfo{
		ID:       "peer1",
		RaftAddr: "127.0.0.1:5000",
	}, mockG)

	if len(mock.removeServerCalls) != 0 {
		t.Errorf("expected no RemoveServer calls, got %d", len(mock.removeServerCalls))
	}
	if len(mock.addVoterCalls) != 0 {
		t.Errorf("expected no AddVoter calls when address unchanged, got %d", len(mock.addVoterCalls))
	}
}

func TestHandlePeerJoin_AddressChanged_RemovesThenAdds(t *testing.T) {
	node := newTestNode()
	node.selfInfo = consensus.NodeInfo{ID: "self"}

	mock := &mockRaft{
		state: raft.Leader,
		configuration: []raft.Server{
			{ID: "peer1", Address: "127.0.0.1:5000"},
		},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "peer1", RaftAddr: "127.0.0.1:6000", Bootstrapped: true},
		},
	}

	node.gossipFn = func() *gossip.Gossip {
		return nil
	}

	node.handlePeerJoinWithRaft(mock, consensus.NodeInfo{
		ID:       "peer1",
		RaftAddr: "127.0.0.1:6000",
	}, mockG)

	if len(mock.removeServerCalls) != 1 {
		t.Errorf("expected 1 RemoveServer call, got %d", len(mock.removeServerCalls))
	}
	if len(mock.removeServerCalls) > 0 && mock.removeServerCalls[0] != "peer1" {
		t.Errorf("expected RemoveServer for 'peer1', got '%s'", mock.removeServerCalls[0])
	}
	if len(mock.addVoterCalls) != 1 {
		t.Errorf("expected 1 AddVoter call, got %d", len(mock.addVoterCalls))
	}
	if len(mock.addVoterCalls) > 0 {
		if mock.addVoterCalls[0].id != "peer1" {
			t.Errorf("expected AddVoter for 'peer1', got '%s'", mock.addVoterCalls[0].id)
		}
		if mock.addVoterCalls[0].addr != "127.0.0.1:6000" {
			t.Errorf("expected AddVoter with new addr '127.0.0.1:6000', got '%s'", mock.addVoterCalls[0].addr)
		}
	}
}

func TestHandlePeerJoin_NewPeer_AddsDirectly(t *testing.T) {
	node := newTestNode()
	node.selfInfo = consensus.NodeInfo{ID: "self"}

	mock := &mockRaft{
		state:         raft.Leader,
		configuration: []raft.Server{},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "peer1", RaftAddr: "127.0.0.1:5000", Bootstrapped: true},
		},
	}

	node.gossipFn = func() *gossip.Gossip {
		return nil
	}

	node.handlePeerJoinWithRaft(mock, consensus.NodeInfo{
		ID:       "peer1",
		RaftAddr: "127.0.0.1:5000",
	}, mockG)

	if len(mock.removeServerCalls) != 0 {
		t.Errorf("expected no RemoveServer calls for new peer, got %d", len(mock.removeServerCalls))
	}
	if len(mock.addVoterCalls) != 1 {
		t.Errorf("expected 1 AddVoter call, got %d", len(mock.addVoterCalls))
	}
}

func TestHandlePeerJoin_SelfPeer_NoAction(t *testing.T) {
	node := newTestNode()
	node.selfInfo = consensus.NodeInfo{ID: "self"}

	mock := &mockRaft{
		state: raft.Leader,
	}

	mockG := &mockGossip{}

	node.handlePeerJoinWithRaft(mock, consensus.NodeInfo{
		ID:       "self",
		RaftAddr: "127.0.0.1:5000",
	}, mockG)

	if len(mock.addVoterCalls) != 0 {
		t.Errorf("expected no AddVoter calls for self, got %d", len(mock.addVoterCalls))
	}
}

func TestHandlePeerJoin_NotLeader_NoAction(t *testing.T) {
	node := newTestNode()
	node.selfInfo = consensus.NodeInfo{ID: "self"}

	mock := &mockRaft{
		state: raft.Follower,
	}

	mockG := &mockGossip{}

	node.handlePeerJoinWithRaft(mock, consensus.NodeInfo{
		ID:       "peer1",
		RaftAddr: "127.0.0.1:5000",
	}, mockG)

	if len(mock.addVoterCalls) != 0 {
		t.Errorf("expected no AddVoter calls when not leader, got %d", len(mock.addVoterCalls))
	}
}

func TestHandlePeerJoin_NotBootstrapped_NoAction(t *testing.T) {
	node := newTestNode()
	node.selfInfo = consensus.NodeInfo{ID: "self"}

	mock := &mockRaft{
		state: raft.Leader,
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "peer1", RaftAddr: "127.0.0.1:5000", Bootstrapped: false},
		},
	}

	node.gossipFn = func() *gossip.Gossip {
		return nil
	}

	node.handlePeerJoinWithRaft(mock, consensus.NodeInfo{
		ID:       "peer1",
		RaftAddr: "127.0.0.1:5000",
	}, mockG)

	if len(mock.addVoterCalls) != 0 {
		t.Errorf("expected no AddVoter calls for non-bootstrapped peer, got %d", len(mock.addVoterCalls))
	}
}
