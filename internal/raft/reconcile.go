package raft

import (
	"time"

	"github.com/hashicorp/raft"
)

func (n *Node) startReconciliationLoop() {
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

	if ra == nil || ra.State() != raft.Leader {
		return
	}

	if n.gossipFn == nil {
		return
	}

	g := n.gossipFn()
	if g == nil {
		return
	}

	configFuture := ra.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		n.logger.Debug("failed to get raft config for reconciliation", "error", err)
		return
	}

	raftMembers := make(map[string]bool)
	for _, server := range configFuture.Configuration().Servers {
		raftMembers[string(server.ID)] = true
	}

	for _, member := range g.Members() {
		if member.ID == selfID {
			continue
		}

		if !member.Bootstrapped {
			continue
		}

		if member.RaftAddr == "" {
			continue
		}

		if raftMembers[member.ID] {
			continue
		}

		n.logger.Info("reconciliation: adding missing peer",
			"peer", member.ID,
			"addr", member.RaftAddr,
		)

		f := ra.AddVoter(
			raft.ServerID(member.ID),
			raft.ServerAddress(member.RaftAddr),
			0,
			defaultOperationTimeout,
		)

		if err := f.Error(); err != nil {
			n.logger.Warn("reconciliation: failed to add peer",
				"peer", member.ID,
				"error", err,
			)
		} else {
			n.logger.Info("reconciliation: peer added", "peer", member.ID)
		}
	}
}
