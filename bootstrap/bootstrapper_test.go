package bootstrap

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleven-am/auto-consensus/gossip"
)

var portCounter atomic.Int32

func init() {
	portCounter.Store(30000)
}

func getFreePort() int {
	return int(portCounter.Add(1))
}

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr error
	}{
		{
			name:    "missing node ID",
			config:  &Config{GossipAddr: "127.0.0.1:7946"},
			wantErr: ErrMissingNodeID,
		},
		{
			name:    "missing gossip addr",
			config:  &Config{NodeID: "node1"},
			wantErr: ErrMissingGossipAddr,
		},
		{
			name:    "invalid gossip addr format",
			config:  &Config{NodeID: "node1", GossipAddr: "invalid"},
			wantErr: ErrInvalidGossipAddr,
		},
		{
			name: "negative max retries",
			config: &Config{
				NodeID:              "node1",
				GossipAddr:          "127.0.0.1:7946",
				MaxDiscoveryRetries: -1,
			},
			wantErr: ErrInvalidMaxRetries,
		},
		{
			name: "valid config",
			config: &Config{
				NodeID:     "node1",
				GossipAddr: "127.0.0.1:7946",
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.applyDefaults()
			err := tt.config.validate()
			if err != tt.wantErr {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := &Config{
		NodeID:     "node1",
		GossipAddr: "127.0.0.1:7946",
	}

	cfg.applyDefaults()

	if cfg.DiscoveryTimeout != DefaultDiscoveryTimeout {
		t.Errorf("expected discovery timeout %v, got %v", DefaultDiscoveryTimeout, cfg.DiscoveryTimeout)
	}

	if cfg.GossipAdvertiseAddr != cfg.GossipAddr {
		t.Errorf("expected gossip advertise addr to default to gossip addr")
	}

	if cfg.RaftAdvertiseAddr != cfg.GossipAdvertiseAddr {
		t.Errorf("expected raft advertise addr to default to gossip advertise addr")
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateInit, "Init"},
		{StateMDNSAdvertiserUp, "MDNSAdvertiserUp"},
		{StateGossipUp, "GossipUp"},
		{StateDiscovering, "Discovering"},
		{StateJoining, "Joining"},
		{StateBootstrapping, "Bootstrapping"},
		{StateRetrying, "Retrying"},
		{StateRunning, "Running"},
		{StateFailed, "Failed"},
		{State(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

type mockDiscoverer struct {
	peers []string
	err   error
	calls int
	mu    sync.Mutex
}

func (m *mockDiscoverer) Discover(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.peers, m.err
}

func TestBootstrapper_SingleNodeBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gossipPort := getFreePort()

	cfg := &Config{
		NodeID:           "node1",
		GossipAddr:       fmt.Sprintf("127.0.0.1:%d", gossipPort),
		CanBootstrap:     true,
		DiscoveryTimeout: 500 * time.Millisecond,
		NetworkMode:      gossip.LAN,
	}

	disc := &mockDiscoverer{peers: nil}
	b, err := New(cfg, disc)
	if err != nil {
		t.Fatalf("failed to create bootstrapper: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("failed to start bootstrapper: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b.State() == StateRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if b.State() != StateRunning {
		t.Errorf("expected state Running, got %s", b.State())
	}

	if err := b.Stop(); err != nil {
		t.Fatalf("failed to stop bootstrapper: %v", err)
	}
}

func TestBootstrapper_JoinExisting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gossipPort1 := getFreePort()
	gossipPort2 := getFreePort()

	cfg1 := &Config{
		NodeID:           "node1",
		GossipAddr:       fmt.Sprintf("127.0.0.1:%d", gossipPort1),
		CanBootstrap:     true,
		DiscoveryTimeout: 500 * time.Millisecond,
		NetworkMode:      gossip.LAN,
	}

	disc1 := &mockDiscoverer{peers: nil}
	b1, err := New(cfg1, disc1)
	if err != nil {
		t.Fatalf("failed to create bootstrapper1: %v", err)
	}

	ctx := context.Background()
	if err := b1.Start(ctx); err != nil {
		t.Fatalf("failed to start bootstrapper1: %v", err)
	}
	defer b1.Stop()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b1.State() == StateRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	cfg2 := &Config{
		NodeID:           "node2",
		GossipAddr:       fmt.Sprintf("127.0.0.1:%d", gossipPort2),
		CanBootstrap:     false,
		DiscoveryTimeout: 500 * time.Millisecond,
		NetworkMode:      gossip.LAN,
	}

	disc2 := &mockDiscoverer{peers: []string{fmt.Sprintf("127.0.0.1:%d", gossipPort1)}}
	b2, err := New(cfg2, disc2)
	if err != nil {
		t.Fatalf("failed to create bootstrapper2: %v", err)
	}

	if err := b2.Start(ctx); err != nil {
		t.Fatalf("failed to start bootstrapper2: %v", err)
	}
	defer b2.Stop()

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b2.State() == StateRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if b2.State() != StateRunning {
		t.Errorf("expected state Running, got %s", b2.State())
	}

	time.Sleep(200 * time.Millisecond)

	g1 := b1.Gossip()
	g2 := b2.Gossip()

	if g1.NumMembers() != 2 {
		t.Errorf("g1 expected 2 members, got %d", g1.NumMembers())
	}
	if g2.NumMembers() != 2 {
		t.Errorf("g2 expected 2 members, got %d", g2.NumMembers())
	}
}

func TestBootstrapper_FailFast(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gossipPort := getFreePort()

	cfg := &Config{
		NodeID:              "node1",
		GossipAddr:          fmt.Sprintf("127.0.0.1:%d", gossipPort),
		CanBootstrap:        false,
		DiscoveryTimeout:    100 * time.Millisecond,
		MaxDiscoveryRetries: 3,
		NetworkMode:         gossip.LAN,
	}

	disc := &mockDiscoverer{peers: nil}
	b, err := New(cfg, disc)
	if err != nil {
		t.Fatalf("failed to create bootstrapper: %v", err)
	}

	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("failed to start bootstrapper: %v", err)
	}
	defer b.Stop()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state := b.State()
		if state == StateFailed || state == StateRunning {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if b.State() != StateFailed {
		t.Errorf("expected state Failed after max retries, got %s", b.State())
	}

	disc.mu.Lock()
	calls := disc.calls
	disc.mu.Unlock()

	if calls < 3 {
		t.Errorf("expected at least 3 discovery calls, got %d", calls)
	}
}

func TestBootstrapper_RetryBackoff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gossipPort := getFreePort()

	cfg := &Config{
		NodeID:              "node1",
		GossipAddr:          fmt.Sprintf("127.0.0.1:%d", gossipPort),
		CanBootstrap:        false,
		DiscoveryTimeout:    50 * time.Millisecond,
		MaxDiscoveryRetries: 5,
		NetworkMode:         gossip.LAN,
	}

	disc := &mockDiscoverer{peers: nil}
	b, err := New(cfg, disc)
	if err != nil {
		t.Fatalf("failed to create bootstrapper: %v", err)
	}

	ctx := context.Background()
	start := time.Now()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("failed to start bootstrapper: %v", err)
	}
	defer b.Stop()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if b.State() == StateFailed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	elapsed := time.Since(start)

	if elapsed < 500*time.Millisecond {
		t.Errorf("backoff too fast, elapsed: %v", elapsed)
	}
}

func TestBootstrapper_HashBasedDelay(t *testing.T) {
	delays := make(map[string]time.Duration)

	for _, nodeID := range []string{"node-a", "node-b", "node-c", "node-xyz"} {
		b := &Bootstrapper{
			config: &Config{
				NodeID:           nodeID,
				DiscoveryTimeout: 5 * time.Second,
			},
		}
		delays[nodeID] = b.hashBasedDelay()
	}

	for id1, d1 := range delays {
		for id2, d2 := range delays {
			if id1 != id2 && d1 == d2 {
				t.Errorf("different nodes %s and %s got same delay %v", id1, id2, d1)
			}
		}
	}

	for nodeID, delay := range delays {
		if delay < 5*time.Second || delay > 6*time.Second {
			t.Errorf("node %s delay %v outside expected range [5s, 6s]", nodeID, delay)
		}
	}
}
