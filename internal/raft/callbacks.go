package raft

import (
	"fmt"
	"time"

	"github.com/eleven-am/auto-consensus/internal/consensus"
	"github.com/hashicorp/raft"
)

func (n *Node) OnBootstrap(self consensus.NodeInfo) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.selfInfo = self
	n.logger.Info("bootstrapping cluster", "node", self.ID)

	if err := n.startRaft(self); err != nil {
		return err
	}

	hasState, err := raft.HasExistingState(
		n.storages.LogStore,
		n.storages.StableStore,
		n.storages.SnapshotStore,
	)
	if err != nil {
		return fmt.Errorf("check existing state: %w", err)
	}

	if hasState {
		go n.startReconciliationLoop()
		return nil
	}

	cfg := raft.Configuration{
		Servers: []raft.Server{
			{
				ID:      raft.ServerID(self.ID),
				Address: raft.ServerAddress(self.RaftAddr),
			},
		},
	}

	f := n.raft.BootstrapCluster(cfg)
	if err := f.Error(); err != nil && err != raft.ErrCantBootstrap {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	n.logger.Info("cluster bootstrapped", "node", self.ID)
	go n.startReconciliationLoop()

	return nil
}

func (n *Node) OnJoin(self consensus.NodeInfo, peers []consensus.NodeInfo) error {
	n.mu.Lock()
	n.selfInfo = self
	n.mu.Unlock()

	n.logger.Info("joining cluster", "node", self.ID, "peers", len(peers))

	if err := n.startRaftLocked(self); err != nil {
		return err
	}

	hasState, _ := raft.HasExistingState(
		n.storages.LogStore,
		n.storages.StableStore,
		n.storages.SnapshotStore,
	)

	didReset := false
	if hasState {
		accepted, _ := n.waitForAcceptance()
		if !accepted {
			n.logger.Warn("not accepted by cluster, resetting storage")
			if err := n.resetAndRestart(self); err != nil {
				return fmt.Errorf("failed to reset and restart: %w", err)
			}
			didReset = true
		}
	}

	if !didReset {
		go n.startReconciliationLoop()
	}

	return nil
}

func (n *Node) OnPeerJoin(peer consensus.NodeInfo) {
	go n.handlePeerJoin(peer)
}

func (n *Node) handlePeerJoin(peer consensus.NodeInfo) {
	n.mu.RLock()
	raftNode := n.raft
	selfID := n.selfInfo.ID
	n.mu.RUnlock()

	if raftNode == nil || raftNode.State() != raft.Leader || peer.ID == selfID {
		return
	}

	if peer.RaftAddr == "" || !n.isPeerBootstrapped(peer.ID) {
		return
	}

	n.logger.Info("adding peer", "peer", peer.ID)

	f := raftNode.AddVoter(
		raft.ServerID(peer.ID),
		raft.ServerAddress(peer.RaftAddr),
		0,
		defaultOperationTimeout,
	)

	if err := f.Error(); err != nil {
		n.logger.Warn("failed to add peer", "peer", peer.ID, "error", err)
	}
}

func (n *Node) OnPeerLeave(peer consensus.NodeInfo) {
	go n.handlePeerLeave(peer)
}

func (n *Node) handlePeerLeave(peer consensus.NodeInfo) {
	n.mu.RLock()
	raftNode := n.raft
	selfID := n.selfInfo.ID
	n.mu.RUnlock()

	if raftNode == nil || raftNode.State() != raft.Leader || peer.ID == selfID {
		return
	}

	n.logger.Info("removing peer", "peer", peer.ID)

	f := raftNode.RemoveServer(
		raft.ServerID(peer.ID),
		0,
		defaultOperationTimeout,
	)

	if err := f.Error(); err != nil {
		n.logger.Warn("failed to remove peer", "peer", peer.ID, "error", err)
	}
}

func (n *Node) startRaftLocked(self consensus.NodeInfo) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.startRaft(self)
}

func (n *Node) isPeerBootstrapped(peerID string) bool {
	if n.gossipFn == nil {
		return true
	}

	g := n.gossipFn()
	if g == nil {
		return true
	}

	for _, member := range g.Members() {
		if member.ID == peerID {
			return member.Bootstrapped
		}
	}

	return false
}

func (n *Node) resetAndRestart(self consensus.NodeInfo) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.raft != nil {
		f := n.raft.Shutdown()
		if err := f.Error(); err != nil {
			n.logger.Warn("error during raft shutdown", "error", err)
		}
		n.raft = nil
	}

	if n.transport != nil {
		n.transport.Close()
		n.transport = nil
	}

	n.running = false

	oldStopCh := n.stopCh
	n.stopCh = make(chan struct{})
	close(oldStopCh)

	jitter := hashBasedJitter(self.ID)

	n.mu.Unlock()
	select {
	case <-n.stopCh:
		n.mu.Lock()
		return fmt.Errorf("shutting down")
	case <-timeAfter(jitter):
	}
	n.mu.Lock()

	if err := n.storageFactory.Reset(); err != nil {
		return fmt.Errorf("failed to reset storage: %w", err)
	}

	storages, err := n.storageFactory.Create()
	if err != nil {
		return fmt.Errorf("failed to recreate storages: %w", err)
	}
	n.storages = storages

	n.logger.Info("restarting raft with fresh state")
	if err := n.startRaft(self); err != nil {
		return err
	}

	go n.startReconciliationLoop()

	return nil
}

var timeAfter = time.After
