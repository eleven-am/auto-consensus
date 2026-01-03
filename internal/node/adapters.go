package node

import (
	"context"

	"github.com/eleven-am/auto-consensus/internal/discovery"
	appraft "github.com/eleven-am/auto-consensus/internal/raft"
)

type forwardedFuture struct {
	result []byte
}

func (f *forwardedFuture) Error() error  { return nil }
func (f *forwardedFuture) Response() any { return f.result }
func (f *forwardedFuture) Index() uint64 { return 0 }

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
