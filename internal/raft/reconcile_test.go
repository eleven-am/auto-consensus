package raft

import (
	"testing"

	"github.com/eleven-am/auto-consensus/internal/gossip"
	"github.com/hashicorp/raft"
)

func TestReconcileMembers_SameAddresses_NoAction(t *testing.T) {
	node := newTestNode()

	mock := &mockRaft{
		state: raft.Leader,
		configuration: []raft.Server{
			{ID: "peer1", Address: "127.0.0.1:5000"},
		},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "self", RaftAddr: "127.0.0.1:4000", Bootstrapped: true},
			{ID: "peer1", RaftAddr: "127.0.0.1:5000", Bootstrapped: true},
		},
	}

	node.reconcileMembersWithRaft(mock, "self", mockG)

	if len(mock.removeServerCalls) != 0 {
		t.Errorf("expected no RemoveServer calls, got %d", len(mock.removeServerCalls))
	}
	if len(mock.addVoterCalls) != 0 {
		t.Errorf("expected no AddVoter calls, got %d", len(mock.addVoterCalls))
	}
}

func TestReconcileMembers_AddressChanged_RemovesThenAdds(t *testing.T) {
	node := newTestNode()

	mock := &mockRaft{
		state: raft.Leader,
		configuration: []raft.Server{
			{ID: "peer1", Address: "127.0.0.1:5000"},
		},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "self", RaftAddr: "127.0.0.1:4000", Bootstrapped: true},
			{ID: "peer1", RaftAddr: "127.0.0.1:6000", Bootstrapped: true},
		},
	}

	node.reconcileMembersWithRaft(mock, "self", mockG)

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

func TestReconcileMembers_NewPeer_AddsDirectly(t *testing.T) {
	node := newTestNode()

	mock := &mockRaft{
		state:         raft.Leader,
		configuration: []raft.Server{},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "self", RaftAddr: "127.0.0.1:4000", Bootstrapped: true},
			{ID: "peer1", RaftAddr: "127.0.0.1:5000", Bootstrapped: true},
		},
	}

	node.reconcileMembersWithRaft(mock, "self", mockG)

	if len(mock.removeServerCalls) != 0 {
		t.Errorf("expected no RemoveServer calls for new peer, got %d", len(mock.removeServerCalls))
	}
	if len(mock.addVoterCalls) != 1 {
		t.Errorf("expected 1 AddVoter call, got %d", len(mock.addVoterCalls))
	}
}

func TestReconcileMembers_RemovesStaleServerNotInGossip(t *testing.T) {
	node := newTestNode()

	mock := &mockRaft{
		state: raft.Leader,
		configuration: []raft.Server{
			{ID: "peer1", Address: "127.0.0.1:5000"},
			{ID: "stale_peer", Address: "127.0.0.1:9000"},
		},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "self", RaftAddr: "127.0.0.1:4000", Bootstrapped: true},
			{ID: "peer1", RaftAddr: "127.0.0.1:5000", Bootstrapped: true},
		},
	}

	node.reconcileMembersWithRaft(mock, "self", mockG)

	if len(mock.removeServerCalls) != 1 {
		t.Errorf("expected 1 RemoveServer call for stale peer, got %d", len(mock.removeServerCalls))
	}
	if len(mock.removeServerCalls) > 0 && mock.removeServerCalls[0] != "stale_peer" {
		t.Errorf("expected RemoveServer for 'stale_peer', got '%s'", mock.removeServerCalls[0])
	}
}

func TestReconcileMembers_NotLeader_NoAction(t *testing.T) {
	node := newTestNode()

	mock := &mockRaft{
		state: raft.Follower,
		configuration: []raft.Server{
			{ID: "peer1", Address: "127.0.0.1:5000"},
		},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "peer1", RaftAddr: "127.0.0.1:6000", Bootstrapped: true},
		},
	}

	node.reconcileMembersWithRaft(mock, "self", mockG)

	if len(mock.removeServerCalls) != 0 {
		t.Errorf("expected no RemoveServer calls when not leader, got %d", len(mock.removeServerCalls))
	}
	if len(mock.addVoterCalls) != 0 {
		t.Errorf("expected no AddVoter calls when not leader, got %d", len(mock.addVoterCalls))
	}
}

func TestReconcileMembers_SkipsNonBootstrappedPeers(t *testing.T) {
	node := newTestNode()

	mock := &mockRaft{
		state:         raft.Leader,
		configuration: []raft.Server{},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "self", RaftAddr: "127.0.0.1:4000", Bootstrapped: true},
			{ID: "peer1", RaftAddr: "127.0.0.1:5000", Bootstrapped: false},
		},
	}

	node.reconcileMembersWithRaft(mock, "self", mockG)

	if len(mock.addVoterCalls) != 0 {
		t.Errorf("expected no AddVoter calls for non-bootstrapped peer, got %d", len(mock.addVoterCalls))
	}
}

func TestReconcileMembers_SkipsSelf(t *testing.T) {
	node := newTestNode()

	mock := &mockRaft{
		state:         raft.Leader,
		configuration: []raft.Server{},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "self", RaftAddr: "127.0.0.1:4000", Bootstrapped: true},
		},
	}

	node.reconcileMembersWithRaft(mock, "self", mockG)

	if len(mock.addVoterCalls) != 0 {
		t.Errorf("expected no AddVoter calls for self, got %d", len(mock.addVoterCalls))
	}
}

func TestReconcileMembers_SkipsPeersWithEmptyRaftAddr(t *testing.T) {
	node := newTestNode()

	mock := &mockRaft{
		state:         raft.Leader,
		configuration: []raft.Server{},
	}

	mockG := &mockGossip{
		members: []gossip.NodeInfo{
			{ID: "self", RaftAddr: "127.0.0.1:4000", Bootstrapped: true},
			{ID: "peer1", RaftAddr: "", Bootstrapped: true},
		},
	}

	node.reconcileMembersWithRaft(mock, "self", mockG)

	if len(mock.addVoterCalls) != 0 {
		t.Errorf("expected no AddVoter calls for peer with empty raft addr, got %d", len(mock.addVoterCalls))
	}
}
