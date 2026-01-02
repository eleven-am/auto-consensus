package consensus

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleven-am/auto-consensus/internal/bootstrap"
	"github.com/eleven-am/auto-consensus/internal/gossip"
)

var portCounter atomic.Int32

func init() {
	portCounter.Store(40000)
}

func getFreePort() int {
	return int(portCounter.Add(1))
}

type mockDiscoverer struct {
	peers []string
	mu    sync.Mutex
}

func (m *mockDiscoverer) Discover(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peers, nil
}

func (m *mockDiscoverer) SetPeers(peers []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers = peers
}

type testCallbacks struct {
	mu           sync.Mutex
	bootstrapped bool
	joined       bool
	joinedPeers  []NodeInfo
	peersJoined  []NodeInfo
	peersLeft    []NodeInfo
	bootstrapCh  chan struct{}
	joinCh       chan struct{}
}

func newTestCallbacks() *testCallbacks {
	return &testCallbacks{
		bootstrapCh: make(chan struct{}, 1),
		joinCh:      make(chan struct{}, 1),
	}
}

func (c *testCallbacks) OnBootstrap(self NodeInfo) error {
	c.mu.Lock()
	c.bootstrapped = true
	c.mu.Unlock()
	select {
	case c.bootstrapCh <- struct{}{}:
	default:
	}
	return nil
}

func (c *testCallbacks) OnJoin(self NodeInfo, peers []NodeInfo) error {
	c.mu.Lock()
	c.joined = true
	c.joinedPeers = peers
	c.mu.Unlock()
	select {
	case c.joinCh <- struct{}{}:
	default:
	}
	return nil
}

func (c *testCallbacks) OnPeerJoin(peer NodeInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.peersJoined = append(c.peersJoined, peer)
}

func (c *testCallbacks) OnPeerLeave(peer NodeInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.peersLeft = append(c.peersLeft, peer)
}

func (c *testCallbacks) DidBootstrap() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bootstrapped
}

func (c *testCallbacks) DidJoin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.joined
}

func (c *testCallbacks) JoinedPeers() []NodeInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.joinedPeers
}

func TestNode_SingleNodeBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gossipPort := getFreePort()
	raftAddr := fmt.Sprintf("127.0.0.1:%d", getFreePort())

	callbacks := newTestCallbacks()
	cfg := &Config{
		Bootstrap: &bootstrap.Config{
			NodeID:            "node1",
			GossipAddr:        fmt.Sprintf("127.0.0.1:%d", gossipPort),
			RaftAdvertiseAddr: raftAddr,
			CanBootstrap:      true,
			DiscoveryTimeout:  500 * time.Millisecond,
			NetworkMode:       gossip.LAN,
		},
		Callbacks: callbacks,
	}

	disc := &mockDiscoverer{}
	node, err := New(cfg, disc)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := node.Start(ctx); err != nil {
		t.Fatalf("failed to start node: %v", err)
	}
	defer node.Stop()

	select {
	case <-callbacks.bootstrapCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for bootstrap callback")
	}

	if !callbacks.DidBootstrap() {
		t.Error("expected OnBootstrap to be called")
	}

	if callbacks.DidJoin() {
		t.Error("OnJoin should not be called for bootstrap")
	}
}

func TestNode_JoinExisting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gossipPort1 := getFreePort()
	raftAddr1 := fmt.Sprintf("127.0.0.1:%d", getFreePort())

	gossipPort2 := getFreePort()
	raftAddr2 := fmt.Sprintf("127.0.0.1:%d", getFreePort())

	callbacks1 := newTestCallbacks()
	cfg1 := &Config{
		Bootstrap: &bootstrap.Config{
			NodeID:            "node1",
			GossipAddr:        fmt.Sprintf("127.0.0.1:%d", gossipPort1),
			RaftAdvertiseAddr: raftAddr1,
			CanBootstrap:      true,
			DiscoveryTimeout:  500 * time.Millisecond,
			NetworkMode:       gossip.LAN,
		},
		Callbacks: callbacks1,
	}

	disc1 := &mockDiscoverer{}
	node1, err := New(cfg1, disc1)
	if err != nil {
		t.Fatalf("failed to create node1: %v", err)
	}

	ctx := context.Background()
	if err := node1.Start(ctx); err != nil {
		t.Fatalf("failed to start node1: %v", err)
	}
	defer node1.Stop()

	select {
	case <-callbacks1.bootstrapCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for node1 bootstrap")
	}

	callbacks2 := newTestCallbacks()
	cfg2 := &Config{
		Bootstrap: &bootstrap.Config{
			NodeID:            "node2",
			GossipAddr:        fmt.Sprintf("127.0.0.1:%d", gossipPort2),
			RaftAdvertiseAddr: raftAddr2,
			CanBootstrap:      false,
			DiscoveryTimeout:  500 * time.Millisecond,
			NetworkMode:       gossip.LAN,
		},
		Callbacks: callbacks2,
	}

	disc2 := &mockDiscoverer{peers: []string{fmt.Sprintf("127.0.0.1:%d", gossipPort1)}}
	node2, err := New(cfg2, disc2)
	if err != nil {
		t.Fatalf("failed to create node2: %v", err)
	}

	if err := node2.Start(ctx); err != nil {
		t.Fatalf("failed to start node2: %v", err)
	}
	defer node2.Stop()

	select {
	case <-callbacks2.joinCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for node2 join")
	}

	if callbacks2.DidBootstrap() {
		t.Error("node2 should not bootstrap")
	}

	if !callbacks2.DidJoin() {
		t.Error("expected OnJoin to be called for node2")
	}

	peers := callbacks2.JoinedPeers()
	if len(peers) != 1 {
		t.Errorf("expected 1 peer, got %d", len(peers))
	}

	if len(peers) > 0 && peers[0].ID != "node1" {
		t.Errorf("expected peer ID 'node1', got '%s'", peers[0].ID)
	}
}

func TestNode_PeerEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gossipPort1 := getFreePort()
	raftAddr1 := fmt.Sprintf("127.0.0.1:%d", getFreePort())

	gossipPort2 := getFreePort()
	raftAddr2 := fmt.Sprintf("127.0.0.1:%d", getFreePort())

	callbacks1 := newTestCallbacks()
	cfg1 := &Config{
		Bootstrap: &bootstrap.Config{
			NodeID:            "node1",
			GossipAddr:        fmt.Sprintf("127.0.0.1:%d", gossipPort1),
			RaftAdvertiseAddr: raftAddr1,
			CanBootstrap:      true,
			DiscoveryTimeout:  500 * time.Millisecond,
			NetworkMode:       gossip.LAN,
		},
		Callbacks: callbacks1,
	}

	disc1 := &mockDiscoverer{}
	node1, err := New(cfg1, disc1)
	if err != nil {
		t.Fatalf("failed to create node1: %v", err)
	}

	ctx := context.Background()
	if err := node1.Start(ctx); err != nil {
		t.Fatalf("failed to start node1: %v", err)
	}
	defer node1.Stop()

	select {
	case <-callbacks1.bootstrapCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for node1 bootstrap")
	}

	callbacks2 := newTestCallbacks()
	cfg2 := &Config{
		Bootstrap: &bootstrap.Config{
			NodeID:            "node2",
			GossipAddr:        fmt.Sprintf("127.0.0.1:%d", gossipPort2),
			RaftAdvertiseAddr: raftAddr2,
			CanBootstrap:      false,
			DiscoveryTimeout:  500 * time.Millisecond,
			NetworkMode:       gossip.LAN,
		},
		Callbacks: callbacks2,
	}

	disc2 := &mockDiscoverer{peers: []string{fmt.Sprintf("127.0.0.1:%d", gossipPort1)}}
	node2, err := New(cfg2, disc2)
	if err != nil {
		t.Fatalf("failed to create node2: %v", err)
	}

	if err := node2.Start(ctx); err != nil {
		t.Fatalf("failed to start node2: %v", err)
	}

	select {
	case <-callbacks2.joinCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for node2 join")
	}

	time.Sleep(500 * time.Millisecond)

	callbacks1.mu.Lock()
	peersJoined := len(callbacks1.peersJoined)
	callbacks1.mu.Unlock()

	if peersJoined == 0 {
		t.Error("expected node1 to receive OnPeerJoin for node2")
	}

	node2.Stop()
	time.Sleep(500 * time.Millisecond)

	callbacks1.mu.Lock()
	peersLeft := len(callbacks1.peersLeft)
	callbacks1.mu.Unlock()

	if peersLeft == 0 {
		t.Error("expected node1 to receive OnPeerLeave for node2")
	}
}

func TestNoopCallbacks(t *testing.T) {
	cb := NoopCallbacks()

	if err := cb.OnBootstrap(NodeInfo{ID: "test"}); err != nil {
		t.Errorf("NoopCallbacks.OnBootstrap should not error: %v", err)
	}

	if err := cb.OnJoin(NodeInfo{ID: "test"}, nil); err != nil {
		t.Errorf("NoopCallbacks.OnJoin should not error: %v", err)
	}

	cb.OnPeerJoin(NodeInfo{ID: "test"})
	cb.OnPeerLeave(NodeInfo{ID: "test"})
}

func TestNode_DoubleStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gossipPort := getFreePort()

	cfg := &Config{
		Bootstrap: &bootstrap.Config{
			NodeID:           "node1",
			GossipAddr:       fmt.Sprintf("127.0.0.1:%d", gossipPort),
			CanBootstrap:     true,
			DiscoveryTimeout: 500 * time.Millisecond,
			NetworkMode:      gossip.LAN,
		},
	}

	disc := &mockDiscoverer{}
	node, err := New(cfg, disc)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	ctx := context.Background()
	if err := node.Start(ctx); err != nil {
		t.Fatalf("failed to start node: %v", err)
	}
	defer node.Stop()

	if err := node.Start(ctx); err != ErrAlreadyRunning {
		t.Errorf("expected ErrAlreadyRunning, got %v", err)
	}
}

func TestNode_StopBeforeStart(t *testing.T) {
	gossipPort := getFreePort()

	cfg := &Config{
		Bootstrap: &bootstrap.Config{
			NodeID:           "node1",
			GossipAddr:       fmt.Sprintf("127.0.0.1:%d", gossipPort),
			CanBootstrap:     true,
			DiscoveryTimeout: 500 * time.Millisecond,
			NetworkMode:      gossip.LAN,
		},
	}

	disc := &mockDiscoverer{}
	node, err := New(cfg, disc)
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}

	if err := node.Stop(); err != ErrNotRunning {
		t.Errorf("expected ErrNotRunning, got %v", err)
	}
}
