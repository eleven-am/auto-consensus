package discovery

import (
	"context"
	"net"
	"testing"
)

type mockResolver struct {
	records []*net.SRV
	err     error
}

func (m *mockResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	return "", m.records, m.err
}

func TestDNSDiscoverer_Discover(t *testing.T) {
	tests := []struct {
		name     string
		records  []*net.SRV
		err      error
		expected []string
		wantErr  bool
	}{
		{
			name: "returns addresses from SRV records",
			records: []*net.SRV{
				{Target: "myapp-0.myapp.svc.cluster.local.", Port: 7946},
				{Target: "myapp-1.myapp.svc.cluster.local.", Port: 7946},
				{Target: "myapp-2.myapp.svc.cluster.local.", Port: 7946},
			},
			expected: []string{
				"myapp-0.myapp.svc.cluster.local:7946",
				"myapp-1.myapp.svc.cluster.local:7946",
				"myapp-2.myapp.svc.cluster.local:7946",
			},
		},
		{
			name:     "returns empty slice when no records",
			records:  []*net.SRV{},
			expected: nil,
		},
		{
			name:     "returns empty slice when records nil",
			records:  nil,
			expected: nil,
		},
		{
			name:     "returns nil on not found error",
			records:  nil,
			err:      &net.DNSError{IsNotFound: true},
			expected: nil,
			wantErr:  false,
		},
		{
			name:    "returns error on other dns errors",
			records: nil,
			err:     &net.DNSError{Err: "connection refused"},
			wantErr: true,
		},
		{
			name: "handles different ports",
			records: []*net.SRV{
				{Target: "node1.local.", Port: 8080},
				{Target: "node2.local.", Port: 9090},
			},
			expected: []string{
				"node1.local:8080",
				"node2.local:9090",
			},
		},
		{
			name: "trims trailing dots from targets",
			records: []*net.SRV{
				{Target: "node.example.com.", Port: 7946},
				{Target: "node2.example.com", Port: 7946},
			},
			expected: []string{
				"node.example.com:7946",
				"node2.example.com:7946",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &mockResolver{records: tt.records, err: tt.err}
			d := NewDNSWithResolver(DNSConfig{
				Service:  "gossip",
				Protocol: "tcp",
				Domain:   "myapp.svc.cluster.local",
			}, resolver)

			got, err := d.Discover(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(got) != len(tt.expected) {
				t.Errorf("expected %d addresses, got %d", len(tt.expected), len(got))
				return
			}

			for i, addr := range got {
				if addr != tt.expected[i] {
					t.Errorf("address %d: expected %s, got %s", i, tt.expected[i], addr)
				}
			}
		})
	}
}

func TestDNSDiscoverer_DefaultProtocol(t *testing.T) {
	d := NewDNS(DNSConfig{
		Service: "gossip",
		Domain:  "myapp.svc.cluster.local",
	})

	if d.protocol != "tcp" {
		t.Errorf("expected default protocol 'tcp', got '%s'", d.protocol)
	}
}

func TestDNSDiscoverer_CustomProtocol(t *testing.T) {
	d := NewDNS(DNSConfig{
		Service:  "gossip",
		Protocol: "udp",
		Domain:   "myapp.svc.cluster.local",
	})

	if d.protocol != "udp" {
		t.Errorf("expected protocol 'udp', got '%s'", d.protocol)
	}
}

func TestDNSDiscoverer_RespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resolver := &mockResolver{
		err: ctx.Err(),
	}

	d := NewDNSWithResolver(DNSConfig{
		Service: "gossip",
		Domain:  "myapp.svc.cluster.local",
	}, resolver)

	_, err := d.Discover(ctx)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
