package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

const clusterIDLength = 8

type PeerInfo struct {
	Address      string
	Bootstrapped bool
}

type Discoverer interface {
	Discover(ctx context.Context) ([]string, error)
}

type BootstrapDiscoverer interface {
	Discoverer
	DiscoverPeers(ctx context.Context) ([]PeerInfo, error)
}

func deriveClusterID(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	hash := sha256.Sum256(key)
	return hex.EncodeToString(hash[:clusterIDLength])
}

func DeriveClusterID(key []byte) string {
	return deriveClusterID(key)
}

func peerMapToSlice(m map[string]PeerInfo) []PeerInfo {
	if len(m) == 0 {
		return nil
	}
	result := make([]PeerInfo, 0, len(m))
	for _, v := range m {
		result = append(result, v)
	}
	return result
}
