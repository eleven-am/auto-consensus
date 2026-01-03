package node

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/hashicorp/raft"
)

type Config struct {
	NodeID         string
	GossipPort     int
	SecretKey      []byte
	StorageFactory StorageFactory
	FSM            raft.FSM
	Discoverer     Discoverer
	AdvertiseAddr  string
	Logger         *slog.Logger
}

func (c *Config) validate() error {
	if c.NodeID == "" {
		return ErrMissingNodeID
	}
	if c.GossipPort == 0 {
		return fmt.Errorf("gossip port is required")
	}
	if c.StorageFactory == nil {
		return ErrMissingFactory
	}
	if c.FSM == nil {
		return ErrMissingFSM
	}
	return nil
}

func (c *Config) setDefaults() {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

func (c *Config) gossipAddr() string {
	return fmt.Sprintf("0.0.0.0:%d", c.GossipPort)
}

func (c *Config) raftAddr() string {
	return "0.0.0.0:0"
}

func (c *Config) gossipAdvertiseAddr() string {
	if c.AdvertiseAddr != "" {
		return fmt.Sprintf("%s:%d", c.AdvertiseAddr, c.GossipPort)
	}
	return c.gossipAddr()
}

func (c *Config) raftAdvertiseAddr() string {
	if c.AdvertiseAddr != "" {
		return c.AdvertiseAddr + ":0"
	}
	return "0.0.0.0:0"
}

func (c *Config) grpcAddr() string {
	return "0.0.0.0:0"
}

func (c *Config) grpcAdvertiseAddr(actualAddr string) string {
	if c.AdvertiseAddr != "" && actualAddr != "" {
		_, port, _ := net.SplitHostPort(actualAddr)
		return fmt.Sprintf("%s:%s", c.AdvertiseAddr, port)
	}
	return actualAddr
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
