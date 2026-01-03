package raft

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/eleven-am/auto-consensus/internal/consensus"
	"github.com/eleven-am/auto-consensus/internal/gossip"
	"github.com/eleven-am/auto-consensus/internal/logging"
	"github.com/hashicorp/raft"
)

type Node struct {
	raft           *raft.Raft
	config         Config
	storages       Storages
	storageFactory StorageFactory
	fsm            raft.FSM
	transport      *raft.NetworkTransport
	gossipFn       func() *gossip.Gossip
	logger         *slog.Logger
	mu             sync.RWMutex
	selfInfo       consensus.NodeInfo
	running        bool
	started        bool
	stopCh         chan struct{}
}

func New(cfg Config, factory StorageFactory, fsm raft.FSM) (*Node, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if factory == nil {
		return nil, ErrMissingFactory
	}
	if fsm == nil {
		return nil, ErrMissingFSM
	}

	storages, err := factory.Create()
	if err != nil {
		return nil, fmt.Errorf("failed to create storages: %w", err)
	}

	return &Node{
		config:         cfg,
		storages:       storages,
		storageFactory: factory,
		fsm:            fsm,
		logger:         cfg.Logger,
		stopCh:         make(chan struct{}),
	}, nil
}

func (n *Node) SetGossipFunc(fn func() *gossip.Gossip) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.started {
		n.logger.Warn("SetGossipFunc called after node started, ignoring")
		return
	}
	n.gossipFn = fn
}

func (n *Node) Callbacks() consensus.RaftCallbacks {
	return n
}

func (n *Node) Apply(cmd []byte, timeout time.Duration) (raft.ApplyFuture, error) {
	n.mu.RLock()
	ra := n.raft
	running := n.running
	n.mu.RUnlock()

	if !running || ra == nil {
		return nil, ErrNotRunning
	}

	return ra.Apply(cmd, timeout), nil
}

func (n *Node) IsLeader() bool {
	n.mu.RLock()
	ra := n.raft
	n.mu.RUnlock()

	if ra == nil {
		return false
	}
	return ra.State() == raft.Leader
}

func (n *Node) Leader() (id string, addr string) {
	n.mu.RLock()
	ra := n.raft
	n.mu.RUnlock()

	if ra == nil {
		return "", ""
	}

	leaderAddr, leaderID := ra.LeaderWithID()
	return string(leaderID), string(leaderAddr)
}

func (n *Node) State() string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.raft == nil {
		return "not_started"
	}
	return n.raft.State().String()
}

func (n *Node) Raft() *raft.Raft {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.raft
}

func (n *Node) Shutdown() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.running {
		return nil
	}

	close(n.stopCh)
	n.running = false

	var shutdownErr error

	if n.raft != nil {
		f := n.raft.Shutdown()
		if err := f.Error(); err != nil {
			shutdownErr = err
		}
		n.raft = nil
	}

	if n.transport != nil {
		n.transport.Close()
		n.transport = nil
	}

	return shutdownErr
}

func allocateDynamicPort(addr *net.TCPAddr) (int, error) {
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port, nil
}

func (n *Node) startRaft(self consensus.NodeInfo) error {
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(self.ID)
	config.Logger = nil
	config.LogOutput = logging.NewHashiCorpAdapter(n.logger)

	bindAddr := n.config.BindAddr
	if self.RaftAddr != "" {
		bindAddr = self.RaftAddr
	}

	addr, err := net.ResolveTCPAddr("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}

	if addr.Port == 0 {
		actualPort, err := allocateDynamicPort(addr)
		if err != nil {
			return fmt.Errorf("allocate dynamic port: %w", err)
		}
		addr.Port = actualPort
	}

	advertiseAddr := n.config.AdvertiseAddr
	if advertiseAddr != "" {
		host, _, _ := net.SplitHostPort(advertiseAddr)
		advertiseAddr = net.JoinHostPort(host, fmt.Sprintf("%d", addr.Port))
	} else {
		advertiseAddr = addr.String()
	}

	trans, err := raft.NewTCPTransport(advertiseAddr, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return fmt.Errorf("create transport: %w", err)
	}
	n.transport = trans

	ra, err := raft.NewRaft(
		config,
		n.fsm,
		n.storages.LogStore,
		n.storages.StableStore,
		n.storages.SnapshotStore,
		trans,
	)
	if err != nil {
		n.transport.Close()
		n.transport = nil
		return fmt.Errorf("create raft: %w", err)
	}

	n.raft = ra
	n.running = true
	n.started = true

	if n.gossipFn != nil {
		if g := n.gossipFn(); g != nil {
			localAddr := string(n.transport.LocalAddr())
			actualAddr := localAddr
			if n.config.AdvertiseAddr != "" {
				_, port, _ := net.SplitHostPort(localAddr)
				host, _, _ := net.SplitHostPort(n.config.AdvertiseAddr)
				actualAddr = net.JoinHostPort(host, port)
			}
			g.SetRaftAddr(actualAddr)
		}
	}

	return nil
}
