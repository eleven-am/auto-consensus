package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

type DNSConfig struct {
	Service  string
	Protocol string
	Domain   string
}

type DNSDiscoverer struct {
	service  string
	protocol string
	domain   string
	resolver Resolver
}

type Resolver interface {
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
}

type netResolver struct{}

func (r *netResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	return net.DefaultResolver.LookupSRV(ctx, service, proto, name)
}

func NewDNS(cfg DNSConfig) *DNSDiscoverer {
	protocol := cfg.Protocol
	if protocol == "" {
		protocol = "tcp"
	}

	return &DNSDiscoverer{
		service:  cfg.Service,
		protocol: protocol,
		domain:   cfg.Domain,
		resolver: &netResolver{},
	}
}

func NewDNSWithResolver(cfg DNSConfig, resolver Resolver) *DNSDiscoverer {
	d := NewDNS(cfg)
	d.resolver = resolver
	return d
}

func (d *DNSDiscoverer) Discover(ctx context.Context) ([]string, error) {
	_, records, err := d.resolver.LookupSRV(ctx, d.service, d.protocol, d.domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("dns srv lookup failed: %w", err)
	}

	if len(records) == 0 {
		return nil, nil
	}

	addresses := make([]string, 0, len(records))
	for _, record := range records {
		target := strings.TrimSuffix(record.Target, ".")
		addr := fmt.Sprintf("%s:%d", target, record.Port)
		addresses = append(addresses, addr)
	}

	return addresses, nil
}
