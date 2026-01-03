package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	autoconsensus "github.com/eleven-am/auto-consensus"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
)

type BoltStorageFactory struct {
	dataDir     string
	mu          sync.Mutex
	logStore    *raftboltdb.BoltStore
	stableStore *raftboltdb.BoltStore
}

func NewBoltStorageFactory(dataDir string) *BoltStorageFactory {
	return &BoltStorageFactory{
		dataDir: dataDir,
	}
}

func (f *BoltStorageFactory) Create() (autoconsensus.Storages, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	logPath := filepath.Join(f.dataDir, "raft-log.db")
	logStore, err := raftboltdb.NewBoltStore(logPath)
	if err != nil {
		return autoconsensus.Storages{}, fmt.Errorf("create log store: %w", err)
	}
	f.logStore = logStore

	stablePath := filepath.Join(f.dataDir, "raft-stable.db")
	stableStore, err := raftboltdb.NewBoltStore(stablePath)
	if err != nil {
		logStore.Close()
		return autoconsensus.Storages{}, fmt.Errorf("create stable store: %w", err)
	}
	f.stableStore = stableStore

	snapshotStore, err := raft.NewFileSnapshotStore(f.dataDir, 2, os.Stderr)
	if err != nil {
		logStore.Close()
		stableStore.Close()
		return autoconsensus.Storages{}, fmt.Errorf("create snapshot store: %w", err)
	}

	return autoconsensus.Storages{
		LogStore:      logStore,
		StableStore:   stableStore,
		SnapshotStore: snapshotStore,
	}, nil
}

func (f *BoltStorageFactory) Reset() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.logStore != nil {
		f.logStore.Close()
		f.logStore = nil
	}

	if f.stableStore != nil {
		f.stableStore.Close()
		f.stableStore = nil
	}

	logPath := filepath.Join(f.dataDir, "raft-log.db")
	if err := os.RemoveAll(logPath); err != nil {
		return fmt.Errorf("failed to remove log store: %w", err)
	}

	stablePath := filepath.Join(f.dataDir, "raft-stable.db")
	if err := os.RemoveAll(stablePath); err != nil {
		return fmt.Errorf("failed to remove stable store: %w", err)
	}

	snapshotPath := filepath.Join(f.dataDir, "snapshots")
	if err := os.RemoveAll(snapshotPath); err != nil {
		return fmt.Errorf("failed to remove snapshots: %w", err)
	}

	return nil
}
