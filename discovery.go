package autoconsensus

import (
	"context"
	"time"

	"github.com/eleven-am/auto-consensus/internal/discovery"
)

type Discoverer interface {
	Discover(ctx context.Context) ([]string, error)
}

type MDNSConfig struct {
	Service   string
	Domain    string
	Timeout   time.Duration
	SecretKey []byte
}

func NewMDNSDiscovery(cfg MDNSConfig) Discoverer {
	return discovery.NewMDNS(discovery.MDNSConfig{
		Service:   cfg.Service,
		Domain:    cfg.Domain,
		Timeout:   cfg.Timeout,
		SecretKey: cfg.SecretKey,
	})
}

type DNSConfig struct {
	Service  string
	Protocol string
	Domain   string
}

func NewDNSDiscovery(cfg DNSConfig) Discoverer {
	return discovery.NewDNS(discovery.DNSConfig{
		Service:  cfg.Service,
		Protocol: cfg.Protocol,
		Domain:   cfg.Domain,
	})
}

func NewStaticDiscovery(peers []string) Discoverer {
	return discovery.NewStatic(peers)
}

func NewCascade(discoverers ...Discoverer) Discoverer {
	internal := make([]discovery.Discoverer, len(discoverers))
	for i, d := range discoverers {
		internal[i] = d
	}
	return discovery.NewCascade(internal...)
}
