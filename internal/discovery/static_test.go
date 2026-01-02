package discovery

import (
	"context"
	"testing"
)

func TestStaticDiscoverer_Discover(t *testing.T) {
	tests := []struct {
		name     string
		peers    []string
		expected []string
	}{
		{
			name:     "returns configured peers",
			peers:    []string{"node1:7946", "node2:7946", "node3:7946"},
			expected: []string{"node1:7946", "node2:7946", "node3:7946"},
		},
		{
			name:     "returns nil for empty list",
			peers:    []string{},
			expected: nil,
		},
		{
			name:     "returns nil for nil list",
			peers:    nil,
			expected: nil,
		},
		{
			name:     "returns single peer",
			peers:    []string{"10.0.0.1:7946"},
			expected: []string{"10.0.0.1:7946"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewStatic(tt.peers)
			got, err := d.Discover(context.Background())

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(got) != len(tt.expected) {
				t.Errorf("expected %d peers, got %d", len(tt.expected), len(got))
				return
			}

			for i, peer := range got {
				if peer != tt.expected[i] {
					t.Errorf("peer %d: expected %s, got %s", i, tt.expected[i], peer)
				}
			}
		})
	}
}

func TestStaticDiscoverer_ReturnsCopy(t *testing.T) {
	original := []string{"node1:7946", "node2:7946"}
	d := NewStatic(original)

	got, _ := d.Discover(context.Background())
	got[0] = "modified:1234"

	got2, _ := d.Discover(context.Background())
	if got2[0] != "node1:7946" {
		t.Error("Discover should return a copy, not the original slice")
	}
}

func TestStaticDiscoverer_RespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := NewStatic([]string{"node1:7946"})
	_, err := d.Discover(ctx)

	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
