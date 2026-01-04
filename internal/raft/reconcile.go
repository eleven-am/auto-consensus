package raft

import (
	"time"

	"github.com/hashicorp/raft"
)

func (n *Node) startReconciliationLoop() {
	n.logger.Info("starting reconciliation loop", "interval", n.config.ReconcileInterval)
	ticker := time.NewTicker(n.config.ReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.reconcileMembers()
		}
	}
}

func (n *Node) reconcileMembers() {
	n.mu.RLock()
	ra := n.raft
	selfID := n.selfInfo.ID
	n.mu.RUnlock()

	if ra == nil {
		return
	}

	if n.gossipFn == nil {
		return
	}

	g := n.gossipFn()
	if g == nil {
		return
	}

	n.reconcileMembersWithRaft(ra, selfID, g)
}

func (n *Node) reconcileMembersWithRaft(ra raftInterface, selfID string, g gossipMembersProvider) {
	if ra.State() != raft.Leader {
		return
	}

	members := g.Members()

	configFuture := ra.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		n.logger.Warn("reconciliation: failed to get raft config", "error", err)
		return
	}

	raftServers := make(map[string]raft.ServerAddress)
	for _, server := range configFuture.Configuration().Servers {
		raftServers[string(server.ID)] = server.Address
	}

	gossipMembers := make(map[string]bool)

	for _, member := range members {
		gossipMembers[member.ID] = true

		if member.ID == selfID || !member.Bootstrapped || member.RaftAddr == "" {
			continue
		}

		existingAddr, exists := raftServers[member.ID]
		if exists && string(existingAddr) == member.RaftAddr {
			continue
		}

		if exists && string(existingAddr) != member.RaftAddr {
			n.logger.Info("reconciliation: peer address changed, removing stale entry",
				"peer", member.ID,
				"old_addr", string(existingAddr),
				"new_addr", member.RaftAddr)
			rf := ra.RemoveServer(raft.ServerID(member.ID), 0, defaultOperationTimeout)
			if err := rf.Error(); err != nil {
				n.logger.Warn("reconciliation: failed to remove stale peer", "peer", member.ID, "error", err)
				continue
			}
		}

		n.logger.Info("reconciliation: adding peer", "peer", member.ID, "addr", member.RaftAddr)

		f := ra.AddVoter(
			raft.ServerID(member.ID),
			raft.ServerAddress(member.RaftAddr),
			0,
			defaultOperationTimeout,
		)

		if err := f.Error(); err != nil {
			n.logger.Warn("reconciliation: failed to add peer", "peer", member.ID, "error", err)
		}
	}

	for _, server := range configFuture.Configuration().Servers {
		serverID := string(server.ID)

		if serverID == selfID || gossipMembers[serverID] {
			continue
		}

		n.logger.Info("reconciliation: removing stale peer", "peer", serverID)

		f := ra.RemoveServer(server.ID, 0, defaultOperationTimeout)

		if err := f.Error(); err != nil {
			n.logger.Warn("reconciliation: failed to remove peer", "peer", serverID, "error", err)
		}
	}
}
