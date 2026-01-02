package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	DefaultMDNSService = "_auto-consensus._tcp"
	DefaultMDNSDomain  = "local"
	DefaultMDNSTimeout = 2 * time.Second
)

type MDNSConfig struct {
	Service   string
	Domain    string
	Timeout   time.Duration
	SecretKey []byte
}

type MDNSDiscoverer struct {
	service   string
	domain    string
	timeout   time.Duration
	clusterID string
}

func NewMDNS(cfg MDNSConfig) *MDNSDiscoverer {
	service := cfg.Service
	if service == "" {
		service = DefaultMDNSService
	}

	domain := cfg.Domain
	if domain == "" {
		domain = DefaultMDNSDomain
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultMDNSTimeout
	}

	return &MDNSDiscoverer{
		service:   service,
		domain:    domain,
		timeout:   timeout,
		clusterID: deriveClusterID(cfg.SecretKey),
	}
}

func (m *MDNSDiscoverer) Discover(ctx context.Context) ([]string, error) {
	peers, err := m.DiscoverPeers(ctx)
	if err != nil {
		return nil, err
	}

	addrs := make([]string, len(peers))
	for i, p := range peers {
		addrs[i] = p.Address
	}
	return addrs, nil
}

func (m *MDNSDiscoverer) DiscoverPeers(ctx context.Context) ([]PeerInfo, error) {
	entries := make(chan *mdns.ServiceEntry, 16)
	peers := make(map[string]PeerInfo)

	timeout := m.timeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}

	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	go func() {
		params := &mdns.QueryParam{
			Service:     m.service,
			Domain:      m.domain,
			Timeout:     timeout,
			Entries:     entries,
			DisableIPv6: true,
		}
		mdns.Query(params)
		close(entries)
	}()

	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				return peerMapToSlice(peers), nil
			}
			if entry == nil {
				continue
			}
			peer, valid := m.parseEntry(entry)
			if !valid {
				continue
			}
			if existing, exists := peers[peer.Address]; exists {
				if peer.Bootstrapped && !existing.Bootstrapped {
					peers[peer.Address] = peer
				}
			} else {
				peers[peer.Address] = peer
			}
		case <-queryCtx.Done():
			return peerMapToSlice(peers), nil
		}
	}
}

func (m *MDNSDiscoverer) parseEntry(entry *mdns.ServiceEntry) (PeerInfo, bool) {
	var clusterID, gossipAddr string
	var bootstrapped bool

	for _, field := range entry.InfoFields {
		switch {
		case strings.HasPrefix(field, "cluster="):
			clusterID = strings.TrimPrefix(field, "cluster=")
		case strings.HasPrefix(field, "gossip="):
			gossipAddr = strings.TrimPrefix(field, "gossip=")
		case field == "bootstrapped=1":
			bootstrapped = true
		}
	}

	if m.clusterID != "" && clusterID != m.clusterID {
		return PeerInfo{}, false
	}

	if gossipAddr == "" {
		if entry.AddrV4 != nil && entry.Port > 0 {
			gossipAddr = fmt.Sprintf("%s:%d", entry.AddrV4.String(), entry.Port)
		} else {
			return PeerInfo{}, false
		}
	}

	return PeerInfo{
		Address:      gossipAddr,
		Bootstrapped: bootstrapped,
	}, true
}
