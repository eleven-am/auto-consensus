package autoconsensus

import (
	"log/slog"
	"time"

	"github.com/hashicorp/raft"
)

type NetworkMode int

const (
	LAN NetworkMode = iota
	WAN
)

type Config struct {
	NodeID              string
	GossipAddr          string
	GossipAdvertiseAddr string
	RaftAddr            string
	RaftAdvertiseAddr   string
	SecretKey           []byte
	NetworkMode         NetworkMode
	CanBootstrap        bool
	DiscoveryTimeout    time.Duration
	ReconcileInterval   time.Duration
	Logger              *slog.Logger
}

func (c *Config) validate() error {
	if c.NodeID == "" {
		return ErrMissingNodeID
	}
	if c.GossipAddr == "" {
		return ErrMissingBindAddr
	}
	if c.RaftAddr == "" {
		return ErrMissingBindAddr
	}
	return nil
}

func (c *Config) setDefaults() {
	if c.GossipAdvertiseAddr == "" {
		c.GossipAdvertiseAddr = c.GossipAddr
	}
	if c.RaftAdvertiseAddr == "" {
		c.RaftAdvertiseAddr = c.RaftAddr
	}
	if c.DiscoveryTimeout == 0 {
		c.DiscoveryTimeout = 3 * time.Second
	}
	if c.ReconcileInterval == 0 {
		c.ReconcileInterval = 10 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

type Storages struct {
	LogStore      raft.LogStore
	StableStore   raft.StableStore
	SnapshotStore raft.SnapshotStore
}

type StorageFactory interface {
	Create() (Storages, error)
	Reset() error
}
