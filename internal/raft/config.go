package raft

import (
	"errors"
	"log/slog"
	"time"

	"github.com/hashicorp/raft"
)

var (
	ErrMissingNodeID   = errors.New("node ID is required")
	ErrMissingBindAddr = errors.New("bind address is required")
	ErrMissingFactory  = errors.New("storage factory is required")
	ErrMissingFSM      = errors.New("FSM is required")
	ErrNotRunning      = errors.New("raft node not running")
	ErrAlreadyRunning  = errors.New("raft node already running")
	ErrGossipNotSet    = errors.New("gossip function not set")
)

const (
	defaultReconcileInterval = 10 * time.Second
	defaultAcceptanceTimeout = 10 * time.Second
	defaultAcceptanceRetries = 3
	defaultOperationTimeout  = 10 * time.Second
)

type Config struct {
	NodeID            string
	BindAddr          string
	AdvertiseAddr     string
	ReconcileInterval time.Duration
	Logger            *slog.Logger
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
	if c.ReconcileInterval == 0 {
		c.ReconcileInterval = defaultReconcileInterval
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return nil
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
