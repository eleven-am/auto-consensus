package node

import (
	"context"
	"errors"
	"time"

	"github.com/eleven-am/auto-consensus/internal/bootstrap"
	"github.com/eleven-am/auto-consensus/internal/consensus"
	"github.com/eleven-am/auto-consensus/internal/discovery"
	"github.com/eleven-am/auto-consensus/internal/gossip"
	appgrpc "github.com/eleven-am/auto-consensus/internal/grpc"
	appraft "github.com/eleven-am/auto-consensus/internal/raft"
	"github.com/hashicorp/raft"
	"google.golang.org/grpc"
)

type Node struct {
	raftNode      *appraft.Node
	consensusNode *consensus.Node
	grpcServer    *appgrpc.Server
	forwarder     *appgrpc.Forwarder
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

	grpcServer := appgrpc.NewServer(cfg.Logger)
	forwarder := appgrpc.NewForwarder(cfg.Logger)

	node := &Node{
		raftNode:      raftNode,
		consensusNode: consensusNode,
		grpcServer:    grpcServer,
		forwarder:     forwarder,
		config:        cfg,
	}

	grpcServer.SetLeaderCheck(func() bool {
		return raftNode.IsLeader()
	})

	grpcServer.SetApplyFunc(func(cmd []byte, timeout time.Duration) ([]byte, error) {
		future, err := raftNode.Apply(cmd, timeout)
		if err != nil {
			return nil, err
		}
		if err := future.Error(); err != nil {
			return nil, err
		}
		resp := future.Response()
		if resp == nil {
			return nil, nil
		}
		if b, ok := resp.([]byte); ok {
			return b, nil
		}
		return nil, nil
	})

	grpcServer.SetLeaderAddrFunc(func() string {
		return node.getLeaderGRPCAddr()
	})

	return node, nil
}

func (n *Node) Start(ctx context.Context) error {
	if err := n.grpcServer.Start(n.config.grpcAddr()); err != nil {
		return err
	}

	if err := n.consensusNode.Start(ctx); err != nil {
		n.grpcServer.Stop()
		return err
	}

	grpcAddr := n.config.grpcAdvertiseAddr(n.grpcServer.Addr())
	go n.setGRPCAddrWhenReady(grpcAddr)

	return nil
}

func (n *Node) setGRPCAddrWhenReady(addr string) {
	for {
		if g := n.consensusNode.Gossip(); g != nil {
			g.SetGRPCAddr(addr)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (n *Node) Stop() error {
	n.grpcServer.Stop()
	n.raftNode.Shutdown()
	return n.consensusNode.Stop()
}

func (n *Node) Apply(cmd []byte, timeout time.Duration) (raft.ApplyFuture, error) {
	if n.raftNode.IsLeader() {
		return n.raftNode.Apply(cmd, timeout)
	}

	leaderAddr := n.getLeaderGRPCAddr()
	if leaderAddr == "" {
		return nil, appgrpc.ErrNoLeader
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := n.forwarder.Forward(ctx, leaderAddr, cmd, timeout)
	if err != nil {
		return nil, err
	}

	return &forwardedFuture{result: result}, nil
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

func (n *Node) RegisterService(fn func(grpc.ServiceRegistrar)) {
	n.grpcServer.RegisterService(func(s *grpc.Server) {
		fn(s)
	})
}

func (n *Node) Members() []MemberInfo {
	g := n.consensusNode.Gossip()
	if g == nil {
		return nil
	}
	members := g.Members()
	result := make([]MemberInfo, len(members))
	for i, m := range members {
		result[i] = memberInfoFromGossip(m)
	}
	return result
}

func (n *Node) SetAppMetadata(meta []byte) {
	if g := n.consensusNode.Gossip(); g != nil {
		g.SetAppMetadata(meta)
	}
}

func (n *Node) Broadcast(msg []byte) {
	if g := n.consensusNode.Gossip(); g != nil {
		g.Broadcast(msg)
	}
}

func (n *Node) OnBroadcast(fn func(from string, msg []byte)) {
	if g := n.consensusNode.Gossip(); g != nil {
		g.OnAppMessage(fn)
	}
}

func (n *Node) OnMemberJoin(fn func(MemberInfo)) {
	n.consensusNode.SetAppOnJoin(func(info gossip.NodeInfo) {
		fn(memberInfoFromGossip(info))
	})
}

func (n *Node) OnMemberLeave(fn func(id string)) {
	n.consensusNode.SetAppOnLeave(func(info gossip.NodeInfo) {
		fn(info.ID)
	})
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

func (n *Node) getLeaderGRPCAddr() string {
	leaderID, _ := n.raftNode.Leader()
	if leaderID == "" {
		return ""
	}
	for _, member := range n.consensusNode.Gossip().Members() {
		if member.ID == leaderID {
			return member.GRPCAddr
		}
	}
	return ""
}
