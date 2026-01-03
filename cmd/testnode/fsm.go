package main

import (
	"io"

	"github.com/hashicorp/raft"
)

type noopFSM struct{}

func (f *noopFSM) Apply(*raft.Log) interface{} {
	return nil
}

func (f *noopFSM) Snapshot() (raft.FSMSnapshot, error) {
	return &noopSnapshot{}, nil
}

func (f *noopFSM) Restore(io.ReadCloser) error {
	return nil
}

type noopSnapshot struct{}

func (s *noopSnapshot) Persist(sink raft.SnapshotSink) error {
	return sink.Close()
}

func (s *noopSnapshot) Release() {}
