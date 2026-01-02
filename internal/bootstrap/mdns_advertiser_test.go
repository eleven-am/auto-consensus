package bootstrap

import (
	"testing"

	"github.com/eleven-am/auto-consensus/internal/discovery"
)

func TestMDNSAdvertiser_DefaultConfig(t *testing.T) {
	a := NewMDNSAdvertiser(MDNSAdvertiserConfig{
		NodeID: "node1",
		Port:   7946,
	})

	if a.nodeID != "node1" {
		t.Errorf("expected nodeID node1, got %s", a.nodeID)
	}
	if a.service != discovery.DefaultMDNSService {
		t.Errorf("expected service %s, got %s", discovery.DefaultMDNSService, a.service)
	}
	if a.domain != discovery.DefaultMDNSDomain {
		t.Errorf("expected domain %s, got %s", discovery.DefaultMDNSDomain, a.domain)
	}
	if a.clusterID != "" {
		t.Errorf("expected empty clusterID, got %s", a.clusterID)
	}
}

func TestMDNSAdvertiser_CustomConfig(t *testing.T) {
	cfg := MDNSAdvertiserConfig{
		NodeID:     "node1",
		Service:    "_custom._tcp",
		Domain:     "example",
		Port:       8080,
		SecretKey:  []byte("secret"),
		GossipAddr: "10.0.0.1:7946",
	}
	a := NewMDNSAdvertiser(cfg)

	if a.service != "_custom._tcp" {
		t.Errorf("expected service _custom._tcp, got %s", a.service)
	}
	if a.domain != "example" {
		t.Errorf("expected domain example, got %s", a.domain)
	}
	if a.clusterID == "" {
		t.Error("expected non-empty clusterID")
	}
	if a.gossipAddr != "10.0.0.1:7946" {
		t.Errorf("expected gossipAddr 10.0.0.1:7946, got %s", a.gossipAddr)
	}
}

func TestMDNSAdvertiser_BuildTXTRecords(t *testing.T) {
	a := NewMDNSAdvertiser(MDNSAdvertiserConfig{
		NodeID:     "node1",
		Port:       7946,
		SecretKey:  []byte("secret"),
		GossipAddr: "10.0.0.1:7946",
	})

	records := a.buildTXTRecords()

	hasCluster := false
	hasGossip := false
	hasBootstrapped := false

	for _, r := range records {
		switch {
		case len(r) > 8 && r[:8] == "cluster=":
			hasCluster = true
		case len(r) > 7 && r[:7] == "gossip=":
			hasGossip = true
			if r != "gossip=10.0.0.1:7946" {
				t.Errorf("expected gossip=10.0.0.1:7946, got %s", r)
			}
		case r == "bootstrapped=0":
			hasBootstrapped = true
		case r == "bootstrapped=1":
			t.Error("expected bootstrapped=0, got bootstrapped=1")
		}
	}

	if !hasCluster {
		t.Error("missing cluster record")
	}
	if !hasGossip {
		t.Error("missing gossip record")
	}
	if !hasBootstrapped {
		t.Error("missing bootstrapped record")
	}
}

func TestMDNSAdvertiser_BuildTXTRecords_Bootstrapped(t *testing.T) {
	a := NewMDNSAdvertiser(MDNSAdvertiserConfig{
		NodeID:     "node1",
		Port:       7946,
		GossipAddr: "10.0.0.1:7946",
	})

	a.bootstrapped = true
	records := a.buildTXTRecords()

	found := false
	for _, r := range records {
		if r == "bootstrapped=1" {
			found = true
		}
		if r == "bootstrapped=0" {
			t.Error("expected bootstrapped=1, got bootstrapped=0")
		}
	}

	if !found {
		t.Error("missing bootstrapped=1 record")
	}
}

func TestMDNSAdvertiser_StartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	a := NewMDNSAdvertiser(MDNSAdvertiserConfig{
		NodeID:     "test-node",
		Port:       getFreePort(),
		GossipAddr: "127.0.0.1:7946",
	})

	if err := a.Start(); err != nil {
		t.Fatalf("failed to start advertiser: %v", err)
	}

	if !a.started {
		t.Error("expected started to be true")
	}

	if err := a.Start(); err != nil {
		t.Errorf("second start should be no-op, got: %v", err)
	}

	if err := a.Stop(); err != nil {
		t.Fatalf("failed to stop advertiser: %v", err)
	}

	if a.started {
		t.Error("expected started to be false")
	}

	if err := a.Stop(); err != nil {
		t.Errorf("second stop should be no-op, got: %v", err)
	}
}

func TestMDNSAdvertiser_SetBootstrapped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	a := NewMDNSAdvertiser(MDNSAdvertiserConfig{
		NodeID:     "test-node",
		Port:       getFreePort(),
		GossipAddr: "127.0.0.1:7946",
	})

	if err := a.Start(); err != nil {
		t.Fatalf("failed to start advertiser: %v", err)
	}
	defer a.Stop()

	if a.IsBootstrapped() {
		t.Error("expected IsBootstrapped to be false initially")
	}

	a.SetBootstrapped(true)

	if !a.IsBootstrapped() {
		t.Error("expected IsBootstrapped to be true after SetBootstrapped(true)")
	}

	a.SetBootstrapped(false)

	if a.IsBootstrapped() {
		t.Error("expected IsBootstrapped to be false after SetBootstrapped(false)")
	}
}

func TestMDNSAdvertiser_SetBootstrapped_NoRestart_WhenStopped(t *testing.T) {
	a := NewMDNSAdvertiser(MDNSAdvertiserConfig{
		NodeID:     "test-node",
		Port:       7946,
		GossipAddr: "127.0.0.1:7946",
	})

	a.SetBootstrapped(true)

	if !a.IsBootstrapped() {
		t.Error("expected IsBootstrapped to be true")
	}
}
