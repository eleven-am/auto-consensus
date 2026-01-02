package discovery

import (
	"net"
	"testing"
	"time"

	"github.com/hashicorp/mdns"
)

func TestMDNSDiscoverer_DefaultConfig(t *testing.T) {
	d := NewMDNS(MDNSConfig{})

	if d.service != DefaultMDNSService {
		t.Errorf("expected service %s, got %s", DefaultMDNSService, d.service)
	}
	if d.domain != DefaultMDNSDomain {
		t.Errorf("expected domain %s, got %s", DefaultMDNSDomain, d.domain)
	}
	if d.timeout != DefaultMDNSTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultMDNSTimeout, d.timeout)
	}
	if d.clusterID != "" {
		t.Errorf("expected empty clusterID, got %s", d.clusterID)
	}
}

func TestMDNSDiscoverer_CustomConfig(t *testing.T) {
	cfg := MDNSConfig{
		Service:   "_custom._tcp",
		Domain:    "example",
		Timeout:   5 * time.Second,
		SecretKey: []byte("test-secret"),
	}
	d := NewMDNS(cfg)

	if d.service != "_custom._tcp" {
		t.Errorf("expected service _custom._tcp, got %s", d.service)
	}
	if d.domain != "example" {
		t.Errorf("expected domain example, got %s", d.domain)
	}
	if d.timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", d.timeout)
	}
	if d.clusterID == "" {
		t.Error("expected non-empty clusterID")
	}
}

func TestMDNSDiscoverer_ParseEntry(t *testing.T) {
	tests := []struct {
		name      string
		clusterID string
		entry     *mdns.ServiceEntry
		wantPeer  PeerInfo
		wantValid bool
	}{
		{
			name:      "valid entry with gossip field",
			clusterID: "",
			entry: &mdns.ServiceEntry{
				Name:       "node1",
				AddrV4:     net.ParseIP("192.168.1.10"),
				Port:       7946,
				InfoFields: []string{"gossip=192.168.1.10:7946", "bootstrapped=1"},
			},
			wantPeer:  PeerInfo{Address: "192.168.1.10:7946", Bootstrapped: true},
			wantValid: true,
		},
		{
			name:      "valid entry without gossip field uses addr:port",
			clusterID: "",
			entry: &mdns.ServiceEntry{
				Name:       "node1",
				AddrV4:     net.ParseIP("192.168.1.10"),
				Port:       7946,
				InfoFields: []string{"bootstrapped=0"},
			},
			wantPeer:  PeerInfo{Address: "192.168.1.10:7946", Bootstrapped: false},
			wantValid: true,
		},
		{
			name:      "matching cluster ID",
			clusterID: DeriveClusterID([]byte("secret")),
			entry: &mdns.ServiceEntry{
				Name:       "node1",
				AddrV4:     net.ParseIP("10.0.0.5"),
				Port:       7946,
				InfoFields: []string{"cluster=" + DeriveClusterID([]byte("secret")), "gossip=10.0.0.5:7946"},
			},
			wantPeer:  PeerInfo{Address: "10.0.0.5:7946", Bootstrapped: false},
			wantValid: true,
		},
		{
			name:      "wrong cluster ID rejected",
			clusterID: DeriveClusterID([]byte("secret1")),
			entry: &mdns.ServiceEntry{
				Name:       "node1",
				AddrV4:     net.ParseIP("10.0.0.5"),
				Port:       7946,
				InfoFields: []string{"cluster=" + DeriveClusterID([]byte("secret2")), "gossip=10.0.0.5:7946"},
			},
			wantPeer:  PeerInfo{},
			wantValid: false,
		},
		{
			name:      "no address returns invalid",
			clusterID: "",
			entry: &mdns.ServiceEntry{
				Name:       "node1",
				InfoFields: []string{},
			},
			wantPeer:  PeerInfo{},
			wantValid: false,
		},
		{
			name:      "bootstrapped flag parsed correctly",
			clusterID: "",
			entry: &mdns.ServiceEntry{
				Name:       "node1",
				AddrV4:     net.ParseIP("192.168.1.10"),
				Port:       8080,
				InfoFields: []string{"bootstrapped=1", "gossip=192.168.1.10:7946"},
			},
			wantPeer:  PeerInfo{Address: "192.168.1.10:7946", Bootstrapped: true},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &MDNSDiscoverer{clusterID: tt.clusterID}
			peer, valid := d.parseEntry(tt.entry)

			if valid != tt.wantValid {
				t.Errorf("valid = %v, want %v", valid, tt.wantValid)
			}
			if peer != tt.wantPeer {
				t.Errorf("peer = %+v, want %+v", peer, tt.wantPeer)
			}
		})
	}
}

func TestMDNSDiscoverer_ClusterIsolation(t *testing.T) {
	secret1 := []byte("cluster-a")
	secret2 := []byte("cluster-b")

	d := NewMDNS(MDNSConfig{SecretKey: secret1})

	entryMatch := &mdns.ServiceEntry{
		Name:       "node1",
		AddrV4:     net.ParseIP("10.0.0.1"),
		Port:       7946,
		InfoFields: []string{"cluster=" + DeriveClusterID(secret1), "gossip=10.0.0.1:7946"},
	}

	entryNoMatch := &mdns.ServiceEntry{
		Name:       "node2",
		AddrV4:     net.ParseIP("10.0.0.2"),
		Port:       7946,
		InfoFields: []string{"cluster=" + DeriveClusterID(secret2), "gossip=10.0.0.2:7946"},
	}

	peer, valid := d.parseEntry(entryMatch)
	if !valid {
		t.Error("expected matching cluster to be valid")
	}
	if peer.Address != "10.0.0.1:7946" {
		t.Errorf("expected address 10.0.0.1:7946, got %s", peer.Address)
	}

	_, valid = d.parseEntry(entryNoMatch)
	if valid {
		t.Error("expected non-matching cluster to be invalid")
	}
}
