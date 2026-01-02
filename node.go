package autoconsensus

import (
	"context"
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

func New(cfg Config, factory StorageFactory, fsm raft.FSM, disc Discoverer) (*Node, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if factory == nil {
		return nil, ErrMissingFactory
	}
	if fsm == nil {
		return nil, ErrMissingFSM
	}

	cfg.setDefaults()

	internalFactory := &storageFactoryAdapter{factory: factory}

	raftNode, err := appraft.New(
		appraft.Config{
			NodeID:        cfg.NodeID,
			BindAddr:      cfg.RaftAddr,
			AdvertiseAddr: cfg.RaftAdvertiseAddr,
			Logger:        cfg.Logger,
		},
		internalFactory,
		fsm,
	)
	if err != nil {
		return nil, err
	}

	var networkMode gossip.NetworkMode
	switch cfg.NetworkMode {
	case WAN:
		networkMode = gossip.WAN
	default:
		networkMode = gossip.LAN
	}

	internalDisc := &discovererAdapter{disc: disc}

	consensusCfg := &consensus.Config{
		Bootstrap: &bootstrap.Config{
			NodeID:              cfg.NodeID,
			GossipAddr:          cfg.GossipAddr,
			GossipAdvertiseAddr: cfg.GossipAdvertiseAddr,
			RaftAdvertiseAddr:   cfg.RaftAdvertiseAddr,
			SecretKey:           cfg.SecretKey,
			NetworkMode:         networkMode,
			CanBootstrap:        cfg.CanBootstrap,
			DiscoveryTimeout:    cfg.DiscoveryTimeout,
			Logger:              cfg.Logger,
		},
		Callbacks: raftNode.Callbacks(),
	}

	consensusNode, err := consensus.New(consensusCfg, internalDisc)
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
