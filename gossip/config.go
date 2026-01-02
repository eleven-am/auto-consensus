package gossip

import "log/slog"

type NetworkMode int

const (
	LAN NetworkMode = iota
	WAN
)

type Config struct {
	NodeID        string
	BindAddr      string
	AdvertiseAddr string
	RaftAddr      string
	SecretKey     []byte
	Mode          NetworkMode
	Logger        *slog.Logger
}

func (c *Config) validate() error {
	if c.NodeID == "" {
		return ErrMissingNodeID
	}
	if c.BindAddr == "" {
		return ErrMissingBindAddr
	}
	if c.AdvertiseAddr == "" {
		c.AdvertiseAddr = c.BindAddr
	}
	return nil
}
