package gossip

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
)

type Gossip struct {
	mu       sync.RWMutex
	config   *Config
	list     *memberlist.Memberlist
	delegate *delegate
	events   *eventDelegate
	started  bool
}

func New(cfg *Config, handler EventHandler) (*Gossip, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &Gossip{
		config:   cfg,
		delegate: newDelegate(cfg.AdvertiseAddr, cfg.RaftAddr),
		events:   newEventDelegate(handler),
	}, nil
}

func (g *Gossip) Start() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.started {
		return ErrAlreadyStarted
	}

	mlConfig := g.buildMemberlistConfig()

	list, err := memberlist.Create(mlConfig)
	if err != nil {
		return fmt.Errorf("failed to create memberlist: %w", err)
	}

	g.list = list
	g.started = true

	return nil
}

func (g *Gossip) Stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.started {
		return ErrNotStarted
	}

	if err := g.list.Leave(5 * time.Second); err != nil {
		return fmt.Errorf("failed to leave cluster: %w", err)
	}

	if err := g.list.Shutdown(); err != nil {
		return fmt.Errorf("failed to shutdown memberlist: %w", err)
	}

	g.started = false
	return nil
}

func (g *Gossip) Join(peers []string) (int, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.started {
		return 0, ErrNotStarted
	}

	if len(peers) == 0 {
		return 0, nil
	}

	n, err := g.list.Join(peers)
	if err != nil {
		return n, fmt.Errorf("failed to join cluster: %w", err)
	}

	return n, nil
}

func (g *Gossip) Members() []NodeInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.started {
		return nil
	}

	members := g.list.Members()
	result := make([]NodeInfo, 0, len(members))

	for _, m := range members {
		result = append(result, nodeToInfo(m))
	}

	return result
}

func (g *Gossip) LocalNode() NodeInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.started {
		return NodeInfo{}
	}

	return nodeToInfo(g.list.LocalNode())
}

func (g *Gossip) NumMembers() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.started {
		return 0
	}

	return g.list.NumMembers()
}

func (g *Gossip) SetBootstrapped(bootstrapped bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.delegate != nil {
		g.delegate.SetBootstrapped(bootstrapped)
	}

	if g.list != nil {
		g.list.UpdateNode(time.Second)
	}
}

func (g *Gossip) buildMemberlistConfig() *memberlist.Config {
	var mlConfig *memberlist.Config

	switch g.config.Mode {
	case LAN:
		mlConfig = memberlist.DefaultLANConfig()
	case WAN:
		mlConfig = memberlist.DefaultWANConfig()
	default:
		mlConfig = memberlist.DefaultWANConfig()
	}

	mlConfig.Name = g.config.NodeID
	mlConfig.Delegate = g.delegate
	mlConfig.Events = g.events

	if g.config.Logger != nil {
		mlConfig.Logger = nil
		mlConfig.LogOutput = &slogAdapter{logger: g.config.Logger}
	}

	host, portStr, err := net.SplitHostPort(g.config.BindAddr)
	if err == nil {
		mlConfig.BindAddr = host
		var port int
		fmt.Sscanf(portStr, "%d", &port)
		mlConfig.BindPort = port
	}

	if len(g.config.SecretKey) > 0 {
		encKey := deriveEncryptionKey(g.config.SecretKey)
		keyring, err := memberlist.NewKeyring(nil, encKey)
		if err == nil {
			mlConfig.Keyring = keyring
		}
	}

	return mlConfig
}

func deriveEncryptionKey(secret []byte) []byte {
	hash := sha256.Sum256(secret)
	return hash[:]
}

type slogAdapter struct {
	logger *slog.Logger
}

func (s *slogAdapter) Write(p []byte) (n int, err error) {
	s.logger.Debug(string(p))
	return len(p), nil
}
