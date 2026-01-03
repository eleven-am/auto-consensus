package autoconsensus

import (
	"context"
	"errors"
	"time"

	"github.com/eleven-am/auto-consensus/internal/bootstrap"
	"github.com/eleven-am/auto-consensus/internal/consensus"
	"github.com/eleven-am/auto-consensus/internal/discovery"
	"github.com/eleven-am/auto-consensus/internal/gossip"
	appraft "github.com/eleven-am/auto-consensus/internal/raft"
	"github.com/hashicorp/raft"
)

type Node struct {
	raftNode      *appraft.Node
	consensusNode *consensus.Node
	config        Config
}

func New(cfg Config) (*Node, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	cfg.setDefaults()

	internalFactory := &storageFactoryAdapter{factory: cfg.StorageFactory}

	raftNode, err := appraft.New(
		appraft.Config{
			NodeID:        cfg.NodeID,
			BindAddr:      cfg.raftAddr(),
			AdvertiseAddr: cfg.raftAdvertiseAddr(),
			Logger:        cfg.Logger,
		},
		internalFactory,
		cfg.FSM,
	)
	if err != nil {
		return nil, err
	}

	var disc discovery.Discoverer
	if cfg.Discoverer != nil {
		disc = &discovererAdapter{disc: cfg.Discoverer}
	}

	consensusCfg := &consensus.Config{
		Bootstrap: &bootstrap.Config{
			NodeID:              cfg.NodeID,
			GossipAddr:          cfg.gossipAddr(),
			GossipAdvertiseAddr: cfg.gossipAdvertiseAddr(),
			RaftAdvertiseAddr:   cfg.raftAdvertiseAddr(),
			SecretKey:           cfg.SecretKey,
			NetworkMode:         gossip.LAN,
			CanBootstrap:        true,
			DiscoveryTimeout:    3 * time.Second,
			Logger:              cfg.Logger,
		},
		Callbacks: raftNode.Callbacks(),
	}

	consensusNode, err := consensus.New(consensusCfg, disc)
	if err != nil {
		return nil, err
	}

	raftNode.SetGossipFunc(func() *gossip.Gossip {
		return consensusNode.Gossip()
	})

	return &Node{
		raftNode:      raftNode,
		consensusNode: consensusNode,
		config:        cfg,
	}, nil
}

func (n *Node) Start(ctx context.Context) error {
	return n.consensusNode.Start(ctx)
}

func (n *Node) Stop() error {
	n.raftNode.Shutdown()
	return n.consensusNode.Stop()
}

func (n *Node) Apply(cmd []byte, timeout time.Duration) (raft.ApplyFuture, error) {
	return n.raftNode.Apply(cmd, timeout)
}

func (n *Node) IsLeader() bool {
	return n.raftNode.IsLeader()
}

func (n *Node) Leader() (id, addr string) {
	return n.raftNode.Leader()
}

func (n *Node) State() State {
	return n.consensusNode.State()
}

func (n *Node) RaftState() string {
	return n.raftNode.State()
}

func (n *Node) Raft() *raft.Raft {
	return n.raftNode.Raft()
}

func (n *Node) WaitForReady(ctx context.Context) error {
	state := n.State()
	if state == StateRunning {
		return nil
	}
	if state == StateFailed {
		return errors.New("node failed to start")
	}

	ch := n.consensusNode.Subscribe()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case change := <-ch:
			if change.New == StateRunning {
				return nil
			}
			if change.New == StateFailed {
				return errors.New("node failed to start")
			}
		}
	}
}

func (n *Node) Subscribe() <-chan StateChange {
	internal := n.consensusNode.Subscribe()
	out := make(chan StateChange, 16)

	go func() {
		for change := range internal {
			out <- StateChange{
				Old: change.Old,
				New: change.New,
			}
		}
		close(out)
	}()

	return out
}

type storageFactoryAdapter struct {
	factory StorageFactory
}

func (a *storageFactoryAdapter) Create() (appraft.Storages, error) {
	s, err := a.factory.Create()
	if err != nil {
		return appraft.Storages{}, err
	}
	return appraft.Storages{
		LogStore:      s.LogStore,
		StableStore:   s.StableStore,
		SnapshotStore: s.SnapshotStore,
	}, nil
}

func (a *storageFactoryAdapter) Reset() error {
	return a.factory.Reset()
}

type discovererAdapter struct {
	disc Discoverer
}

func (a *discovererAdapter) Discover(ctx context.Context) ([]string, error) {
	return a.disc.Discover(ctx)
}

var _ discovery.Discoverer = (*discovererAdapter)(nil)
var _ appraft.StorageFactory = (*storageFactoryAdapter)(nil)
