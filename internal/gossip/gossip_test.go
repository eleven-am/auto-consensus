package gossip

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func getFreePort(base int) int {
	return base + int(time.Now().UnixNano()%1000)
}

func TestGossip_NewValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr error
	}{
		{
			name:    "missing node ID",
			config:  &Config{BindAddr: "127.0.0.1:7946"},
			wantErr: ErrMissingNodeID,
		},
		{
			name:    "missing bind addr",
			config:  &Config{NodeID: "node1"},
			wantErr: ErrMissingBindAddr,
		},
		{
			name: "valid config",
			config: &Config{
				NodeID:   "node1",
				BindAddr: "127.0.0.1:7946",
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.config, nil)
			if err != tt.wantErr {
				t.Errorf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestGossip_StartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	port := getFreePort(17000)
	g, err := New(&Config{
		NodeID:   "node1",
		BindAddr: fmt.Sprintf("127.0.0.1:%d", port),
		Mode:     LAN,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create gossip: %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("failed to start gossip: %v", err)
	}

	if err := g.Start(); err != ErrAlreadyStarted {
		t.Errorf("expected ErrAlreadyStarted, got %v", err)
	}

	if g.NumMembers() != 1 {
		t.Errorf("expected 1 member, got %d", g.NumMembers())
	}

	if err := g.Stop(); err != nil {
		t.Fatalf("failed to stop gossip: %v", err)
	}

	if err := g.Stop(); err != ErrNotStarted {
		t.Errorf("expected ErrNotStarted, got %v", err)
	}
}

func TestGossip_SingleNode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	port := getFreePort(18000)
	g, err := New(&Config{
		NodeID:        "node1",
		BindAddr:      fmt.Sprintf("127.0.0.1:%d", port),
		AdvertiseAddr: "10.0.0.1:7946",
		Mode:          LAN,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create gossip: %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("failed to start gossip: %v", err)
	}
	defer g.Stop()

	local := g.LocalNode()
	if local.ID != "node1" {
		t.Errorf("expected node ID 'node1', got '%s'", local.ID)
	}

	if local.Address != "10.0.0.1:7946" {
		t.Errorf("expected address '10.0.0.1:7946', got '%s'", local.Address)
	}

	members := g.Members()
	if len(members) != 1 {
		t.Errorf("expected 1 member, got %d", len(members))
	}
}

func TestGossip_TwoNodesJoin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	port1 := getFreePort(19000)
	port2 := getFreePort(20000)

	var joinEvents sync.WaitGroup
	joinEvents.Add(1)

	handler := &testEventHandler{
		onJoin: func(node NodeInfo) {
			if node.ID == "node2" {
				joinEvents.Done()
			}
		},
	}

	g1, err := New(&Config{
		NodeID:        "node1",
		BindAddr:      fmt.Sprintf("127.0.0.1:%d", port1),
		AdvertiseAddr: fmt.Sprintf("127.0.0.1:%d", port1),
		Mode:          LAN,
	}, handler)
	if err != nil {
		t.Fatalf("failed to create gossip1: %v", err)
	}

	if err := g1.Start(); err != nil {
		t.Fatalf("failed to start gossip1: %v", err)
	}
	defer g1.Stop()

	g2, err := New(&Config{
		NodeID:        "node2",
		BindAddr:      fmt.Sprintf("127.0.0.1:%d", port2),
		AdvertiseAddr: fmt.Sprintf("127.0.0.1:%d", port2),
		Mode:          LAN,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create gossip2: %v", err)
	}

	if err := g2.Start(); err != nil {
		t.Fatalf("failed to start gossip2: %v", err)
	}
	defer g2.Stop()

	n, err := g2.Join([]string{fmt.Sprintf("127.0.0.1:%d", port1)})
	if err != nil {
		t.Fatalf("failed to join: %v", err)
	}

	if n != 1 {
		t.Errorf("expected 1 successful join, got %d", n)
	}

	done := make(chan struct{})
	go func() {
		joinEvents.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for join event")
	}

	time.Sleep(100 * time.Millisecond)

	if g1.NumMembers() != 2 {
		t.Errorf("g1 expected 2 members, got %d", g1.NumMembers())
	}

	if g2.NumMembers() != 2 {
		t.Errorf("g2 expected 2 members, got %d", g2.NumMembers())
	}
}

func TestGossip_MemberLeave(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	port1 := getFreePort(21000)
	port2 := getFreePort(22000)

	var leaveEvents sync.WaitGroup
	leaveEvents.Add(1)

	handler := &testEventHandler{
		onLeave: func(node NodeInfo) {
			if node.ID == "node2" {
				leaveEvents.Done()
			}
		},
	}

	g1, err := New(&Config{
		NodeID:        "node1",
		BindAddr:      fmt.Sprintf("127.0.0.1:%d", port1),
		AdvertiseAddr: fmt.Sprintf("127.0.0.1:%d", port1),
		Mode:          LAN,
	}, handler)
	if err != nil {
		t.Fatalf("failed to create gossip1: %v", err)
	}

	if err := g1.Start(); err != nil {
		t.Fatalf("failed to start gossip1: %v", err)
	}
	defer g1.Stop()

	g2, err := New(&Config{
		NodeID:        "node2",
		BindAddr:      fmt.Sprintf("127.0.0.1:%d", port2),
		AdvertiseAddr: fmt.Sprintf("127.0.0.1:%d", port2),
		Mode:          LAN,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create gossip2: %v", err)
	}

	if err := g2.Start(); err != nil {
		t.Fatalf("failed to start gossip2: %v", err)
	}

	_, err = g2.Join([]string{fmt.Sprintf("127.0.0.1:%d", port1)})
	if err != nil {
		t.Fatalf("failed to join: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := g2.Stop(); err != nil {
		t.Fatalf("failed to stop g2: %v", err)
	}

	done := make(chan struct{})
	go func() {
		leaveEvents.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for leave event")
	}

	time.Sleep(100 * time.Millisecond)

	if g1.NumMembers() != 1 {
		t.Errorf("g1 expected 1 member after leave, got %d", g1.NumMembers())
	}
}

func TestGossip_ClusterIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	port1 := getFreePort(23000)
	port2 := getFreePort(24000)

	key1 := []byte("secret-key-cluster-1")
	key2 := []byte("secret-key-cluster-2")

	g1, err := New(&Config{
		NodeID:        "node1",
		BindAddr:      fmt.Sprintf("127.0.0.1:%d", port1),
		AdvertiseAddr: fmt.Sprintf("127.0.0.1:%d", port1),
		SecretKey:     key1,
		Mode:          LAN,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create gossip1: %v", err)
	}

	if err := g1.Start(); err != nil {
		t.Fatalf("failed to start gossip1: %v", err)
	}
	defer g1.Stop()

	g2, err := New(&Config{
		NodeID:        "node2",
		BindAddr:      fmt.Sprintf("127.0.0.1:%d", port2),
		AdvertiseAddr: fmt.Sprintf("127.0.0.1:%d", port2),
		SecretKey:     key2,
		Mode:          LAN,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create gossip2: %v", err)
	}

	if err := g2.Start(); err != nil {
		t.Fatalf("failed to start gossip2: %v", err)
	}
	defer g2.Stop()

	n, joinErr := g2.Join([]string{fmt.Sprintf("127.0.0.1:%d", port1)})

	time.Sleep(500 * time.Millisecond)

	if g1.NumMembers() != 1 {
		t.Errorf("g1 should have 1 member (itself), got %d", g1.NumMembers())
	}

	if g2.NumMembers() != 1 {
		t.Errorf("g2 should have 1 member (itself), got %d", g2.NumMembers())
	}

	if joinErr == nil && n > 0 {
		t.Logf("join returned success but clusters should remain isolated")
	}
}

func TestGossip_WANMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	port := getFreePort(25000)
	g, err := New(&Config{
		NodeID:   "node1",
		BindAddr: fmt.Sprintf("127.0.0.1:%d", port),
		Mode:     WAN,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create gossip: %v", err)
	}

	if err := g.Start(); err != nil {
		t.Fatalf("failed to start gossip: %v", err)
	}
	defer g.Stop()

	if g.NumMembers() != 1 {
		t.Errorf("expected 1 member, got %d", g.NumMembers())
	}
}

func TestGossip_JoinBeforeStart(t *testing.T) {
	g, err := New(&Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1:7946",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create gossip: %v", err)
	}

	_, err = g.Join([]string{"127.0.0.1:7947"})
	if err != ErrNotStarted {
		t.Errorf("expected ErrNotStarted, got %v", err)
	}
}

func TestGossip_MembersBeforeStart(t *testing.T) {
	g, err := New(&Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1:7946",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create gossip: %v", err)
	}

	members := g.Members()
	if members != nil {
		t.Errorf("expected nil members before start, got %v", members)
	}
}

func TestGossip_AdvertiseAddrDefault(t *testing.T) {
	cfg := &Config{
		NodeID:   "node1",
		BindAddr: "127.0.0.1:7946",
	}

	_, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create gossip: %v", err)
	}

	if cfg.AdvertiseAddr != cfg.BindAddr {
		t.Errorf("expected advertise addr to default to bind addr")
	}
}

type testEventHandler struct {
	onJoin   func(NodeInfo)
	onLeave  func(NodeInfo)
	onUpdate func(NodeInfo)
}

func (h *testEventHandler) OnJoin(node NodeInfo) {
	if h.onJoin != nil {
		h.onJoin(node)
	}
}

func (h *testEventHandler) OnLeave(node NodeInfo) {
	if h.onLeave != nil {
		h.onLeave(node)
	}
}

func (h *testEventHandler) OnUpdate(node NodeInfo) {
	if h.onUpdate != nil {
		h.onUpdate(node)
	}
}
