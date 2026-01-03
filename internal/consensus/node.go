package consensus

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/eleven-am/auto-consensus/internal/bootstrap"
	"github.com/eleven-am/auto-consensus/internal/discovery"
	"github.com/eleven-am/auto-consensus/internal/gossip"
)

var (
	ErrAlreadyRunning = errors.New("node already running")
	ErrNotRunning     = errors.New("node not running")
)

type Config struct {
	Bootstrap *bootstrap.Config
	Callbacks RaftCallbacks
}

type Node struct {
	config       *Config
	bootstrapper *bootstrap.Bootstrapper
	discoverer   discovery.Discoverer
	mu           sync.RWMutex
	running      bool
	localInfo    NodeInfo
	stopCh       chan struct{}
}

func New(cfg *Config, disc discovery.Discoverer) (*Node, error) {
	if cfg.Callbacks == nil {
		cfg.Callbacks = NoopCallbacks()
	}

	n := &Node{
		config:     cfg,
		discoverer: disc,
	}

	cfg.Bootstrap.EventHandler = n

	b, err := bootstrap.New(cfg.Bootstrap, disc)
	if err != nil {
		return nil, err
	}

	n.bootstrapper = b
	n.localInfo = NodeInfo{
		ID:       cfg.Bootstrap.NodeID,
		RaftAddr: cfg.Bootstrap.RaftAdvertiseAddr,
	}

	return n, nil
}

func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	if n.running {
		n.mu.Unlock()
		return ErrAlreadyRunning
	}
	n.running = true
	n.stopCh = make(chan struct{})
	n.mu.Unlock()

	if err := n.bootstrapper.Start(ctx); err != nil {
		n.mu.Lock()
		n.running = false
		n.mu.Unlock()
		return err
	}

	go n.waitForBootstrap(ctx)

	return nil
}

func (n *Node) waitForBootstrap(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-n.stopCh:
			return
		case <-ticker.C:
			state := n.bootstrapper.State()
			if state == bootstrap.StateRunning {
				n.onBootstrapComplete()
				return
			}
			if state == bootstrap.StateFailed || state == bootstrap.StateInit {
				return
			}
		}
	}
}

func (n *Node) onBootstrapComplete() {
	if n.bootstrapper.DidBootstrap() {
		n.config.Callbacks.OnBootstrap(n.localInfo)
		return
	}

	g := n.bootstrapper.Gossip()
	if g == nil {
		return
	}

	members := g.Members()
	peers := make([]NodeInfo, 0, len(members))
	for _, m := range members {
		if m.ID == n.localInfo.ID {
			continue
		}
		peers = append(peers, NodeInfo{
			ID:       m.ID,
			RaftAddr: m.RaftAddr,
		})
	}

	n.config.Callbacks.OnJoin(n.localInfo, peers)
}

func (n *Node) Stop() error {
	n.mu.Lock()
	if !n.running {
		n.mu.Unlock()
		return ErrNotRunning
	}
	close(n.stopCh)
	n.mu.Unlock()

	err := n.bootstrapper.Stop()

	n.mu.Lock()
	n.running = false
	n.mu.Unlock()

	return err
}

func (n *Node) State() bootstrap.State {
	return n.bootstrapper.State()
}

func (n *Node) LocalInfo() NodeInfo {
	return n.localInfo
}

func (n *Node) Gossip() *gossip.Gossip {
	return n.bootstrapper.Gossip()
}

func (n *Node) Subscribe() <-chan bootstrap.StateChange {
	return n.bootstrapper.Subscribe()
}

func (n *Node) OnJoin(node gossip.NodeInfo) {
	if node.ID == n.localInfo.ID {
		return
	}
	n.config.Callbacks.OnPeerJoin(NodeInfo{
		ID:       node.ID,
		RaftAddr: node.RaftAddr,
	})
}

func (n *Node) OnLeave(node gossip.NodeInfo) {
	if node.ID == n.localInfo.ID {
		return
	}
	n.config.Callbacks.OnPeerLeave(NodeInfo{
		ID:       node.ID,
		RaftAddr: node.RaftAddr,
	})
}

func (n *Node) OnUpdate(node gossip.NodeInfo) {
	if node.ID == n.localInfo.ID {
		return
	}
	n.config.Callbacks.OnPeerJoin(NodeInfo{
		ID:       node.ID,
		RaftAddr: node.RaftAddr,
	})
}
