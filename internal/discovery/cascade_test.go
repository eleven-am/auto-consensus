package discovery

import (
	"context"
	"errors"
	"testing"
)

type mockDiscoverer struct {
	peers []string
	err   error
}

func (m *mockDiscoverer) Discover(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.peers, m.err
}

func TestCascadeDiscoverer_ReturnsFirstSuccess(t *testing.T) {
	cascade := NewCascade(
		&mockDiscoverer{peers: nil},
		&mockDiscoverer{peers: []string{"node1:7946", "node2:7946"}},
		&mockDiscoverer{peers: []string{"node3:7946"}},
	)

	peers, err := cascade.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}

	if peers[0] != "node1:7946" || peers[1] != "node2:7946" {
		t.Errorf("unexpected peers: %v", peers)
	}
}

func TestCascadeDiscoverer_SkipsErrors(t *testing.T) {
	cascade := NewCascade(
		&mockDiscoverer{err: errors.New("broadcast failed")},
		&mockDiscoverer{peers: []string{"node1:7946"}},
	)

	peers, err := cascade.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
}

func TestCascadeDiscoverer_ReturnsLastError(t *testing.T) {
	cascade := NewCascade(
		&mockDiscoverer{err: errors.New("first error")},
		&mockDiscoverer{err: errors.New("second error")},
	)

	peers, err := cascade.Discover(context.Background())
	if peers != nil {
		t.Errorf("expected nil peers, got %v", peers)
	}

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != "discoverer 1: second error" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCascadeDiscoverer_EmptyDiscoverers(t *testing.T) {
	cascade := NewCascade()

	peers, err := cascade.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if peers != nil {
		t.Errorf("expected nil peers, got %v", peers)
	}
}

func TestCascadeDiscoverer_AllEmpty(t *testing.T) {
	cascade := NewCascade(
		&mockDiscoverer{peers: nil},
		&mockDiscoverer{peers: []string{}},
		&mockDiscoverer{peers: nil},
	)

	peers, err := cascade.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if peers != nil {
		t.Errorf("expected nil peers, got %v", peers)
	}
}

func TestCascadeDiscoverer_RespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cascade := NewCascade(
		&mockDiscoverer{peers: []string{"node1:7946"}},
	)

	_, err := cascade.Discover(ctx)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestCascadeDiscoverer_StopsOnSuccess(t *testing.T) {
	callCount := 0
	thirdDiscoverer := &mockDiscoverer{peers: []string{"should-not-reach"}}

	cascade := NewCascade(
		&mockDiscoverer{peers: nil},
		&mockDiscoverer{peers: []string{"node1:7946"}},
		thirdDiscoverer,
	)

	cascade.discoverers[2] = &countingDiscoverer{
		Discoverer: thirdDiscoverer,
		count:      &callCount,
	}

	_, err := cascade.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 0 {
		t.Errorf("third discoverer should not have been called, but was called %d times", callCount)
	}
}

type countingDiscoverer struct {
	Discoverer
	count *int
}

func (c *countingDiscoverer) Discover(ctx context.Context) ([]string, error) {
	*c.count++
	return c.Discoverer.Discover(ctx)
}
