package bootstrap

import (
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/eleven-am/auto-consensus/internal/discovery"
	"github.com/hashicorp/mdns"
)

type MDNSAdvertiser struct {
	nodeID       string
	service      string
	domain       string
	port         int
	clusterID    string
	gossipAddr   string
	server       *mdns.Server
	mu           sync.RWMutex
	started      bool
	bootstrapped bool
}

type MDNSAdvertiserConfig struct {
	NodeID     string
	Service    string
	Domain     string
	Port       int
	SecretKey  []byte
	GossipAddr string
}

func NewMDNSAdvertiser(cfg MDNSAdvertiserConfig) *MDNSAdvertiser {
	service := cfg.Service
	if service == "" {
		service = discovery.DefaultMDNSService
	}

	domain := cfg.Domain
	if domain == "" {
		domain = discovery.DefaultMDNSDomain
	}

	return &MDNSAdvertiser{
		nodeID:     cfg.NodeID,
		service:    service,
		domain:     domain,
		port:       cfg.Port,
		clusterID:  discovery.DeriveClusterID(cfg.SecretKey),
		gossipAddr: cfg.GossipAddr,
	}
}

func (a *MDNSAdvertiser) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started {
		return nil
	}

	if err := a.startServerLocked(); err != nil {
		return err
	}

	a.started = true
	return nil
}

func (a *MDNSAdvertiser) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.started {
		return nil
	}

	if a.server != nil {
		a.server.Shutdown()
		a.server = nil
	}

	a.started = false
	return nil
}

func (a *MDNSAdvertiser) SetBootstrapped(bootstrapped bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.bootstrapped == bootstrapped {
		return
	}

	a.bootstrapped = bootstrapped

	if a.started && a.server != nil {
		a.server.Shutdown()
		a.startServerLocked()
	}
}

func (a *MDNSAdvertiser) IsBootstrapped() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.bootstrapped
}

func (a *MDNSAdvertiser) startServerLocked() error {
	txtRecords := a.buildTXTRecords()

	host, err := os.Hostname()
	if err != nil {
		host = a.nodeID
	}

	ips := a.getLocalIPs()

	domain := a.domain
	if domain != "" && domain[len(domain)-1] != '.' {
		domain = domain + "."
	}

	svc, err := mdns.NewMDNSService(
		a.nodeID,
		a.service,
		domain,
		host+".",
		a.port,
		ips,
		txtRecords,
	)
	if err != nil {
		return fmt.Errorf("failed to create mDNS service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: svc})
	if err != nil {
		return fmt.Errorf("failed to start mDNS server: %w", err)
	}

	a.server = server
	return nil
}

func (a *MDNSAdvertiser) buildTXTRecords() []string {
	records := make([]string, 0, 3)

	if a.clusterID != "" {
		records = append(records, "cluster="+a.clusterID)
	}

	if a.gossipAddr != "" {
		records = append(records, "gossip="+a.gossipAddr)
	}

	if a.bootstrapped {
		records = append(records, "bootstrapped=1")
	} else {
		records = append(records, "bootstrapped=0")
	}

	return records
}

func (a *MDNSAdvertiser) getLocalIPs() []net.IP {
	var ips []net.IP

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			if ipNet.IP.To4() != nil && !ipNet.IP.IsLoopback() {
				ips = append(ips, ipNet.IP)
			}
		}
	}

	return ips
}
