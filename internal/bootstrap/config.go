package bootstrap

import (
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/eleven-am/auto-consensus/internal/gossip"
)

const DefaultDiscoveryTimeout = 5 * time.Second

var (
	ErrMissingNodeID        = errors.New("node ID is required")
	ErrMissingGossipAddr    = errors.New("gossip address is required")
	ErrInvalidGossipAddr    = errors.New("gossip address must be host:port format")
	ErrInvalidMaxRetries    = errors.New("max discovery retries cannot be negative")
	ErrInvalidDiscoveryTime = errors.New("discovery timeout must be positive")
)

type Config struct {
	NodeID              string
	GossipAddr          string
	GossipAdvertiseAddr string
	RaftAdvertiseAddr   string
	SecretKey           []byte
	NetworkMode         gossip.NetworkMode
	CanBootstrap        bool
	DiscoveryTimeout    time.Duration
	MaxDiscoveryRetries int
	Logger              *slog.Logger
	EventHandler        gossip.EventHandler
	MDNSService         string
	MDNSDomain          string
}

func (c *Config) applyDefaults() {
	if c.DiscoveryTimeout == 0 {
		c.DiscoveryTimeout = DefaultDiscoveryTimeout
	}
	if c.GossipAdvertiseAddr == "" {
		c.GossipAdvertiseAddr = c.GossipAddr
	}
	if c.RaftAdvertiseAddr == "" {
		c.RaftAdvertiseAddr = c.GossipAdvertiseAddr
	}
}

func (c *Config) validate() error {
	if c.NodeID == "" {
		return ErrMissingNodeID
	}
	if c.GossipAddr == "" {
		return ErrMissingGossipAddr
	}
	if _, _, err := net.SplitHostPort(c.GossipAddr); err != nil {
		return ErrInvalidGossipAddr
	}
	if c.MaxDiscoveryRetries < 0 {
		return ErrInvalidMaxRetries
	}
	if c.DiscoveryTimeout <= 0 {
		return ErrInvalidDiscoveryTime
	}
	return nil
}
