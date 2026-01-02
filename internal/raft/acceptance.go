package raft

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"time"
)

func (n *Node) waitForAcceptance() (bool, error) {
	for attempt := 0; attempt < defaultAcceptanceRetries; attempt++ {
		if attempt > 0 {
			jitter := time.Duration(rand.Int63n(int64(2 * time.Second)))
			select {
			case <-n.stopCh:
				return false, fmt.Errorf("shutting down")
			case <-timeAfter(defaultAcceptanceTimeout/2 + jitter):
			}
		}

		accepted, err := n.checkAcceptance()
		if err != nil {
			n.logger.Debug("acceptance check attempt failed", "attempt", attempt+1, "error", err)
			continue
		}

		if accepted {
			return true, nil
		}

		n.logger.Debug("not yet accepted, will retry", "attempt", attempt+1)
	}

	return false, nil
}

func (n *Node) checkAcceptance() (bool, error) {
	n.mu.RLock()
	ra := n.raft
	self := n.selfInfo
	n.mu.RUnlock()

	if ra == nil {
		return false, fmt.Errorf("raft not started")
	}

	deadline := time.Now().Add(defaultAcceptanceTimeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-n.stopCh:
			return false, fmt.Errorf("shutting down")
		case <-ticker.C:
		}

		_, leaderID := ra.LeaderWithID()
		if leaderID == "" {
			continue
		}

		if string(leaderID) == self.ID {
			return true, nil
		}

		configFuture := ra.GetConfiguration()
		if err := configFuture.Error(); err != nil {
			continue
		}

		for _, server := range configFuture.Configuration().Servers {
			if string(server.ID) == self.ID {
				n.logger.Info("confirmed membership in cluster config")
				return true, nil
			}
		}

		n.logger.Debug("leader exists but we're not in config",
			"leader", leaderID,
			"self", self.ID,
		)
		return false, nil
	}

	return false, fmt.Errorf("timeout waiting for leader")
}

func hashBasedJitter(nodeID string) time.Duration {
	hash := sha256.Sum256([]byte(nodeID))
	var hashValue uint64
	for i := 0; i < 8; i++ {
		hashValue = (hashValue << 8) | uint64(hash[i])
	}
	fraction := float64(hashValue) / float64(^uint64(0))
	return time.Duration(fraction * float64(3*time.Second))
}
