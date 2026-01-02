package discovery

import "context"

type StaticDiscoverer struct {
	peers []string
}

func NewStatic(peers []string) *StaticDiscoverer {
	return &StaticDiscoverer{peers: peers}
}

func (s *StaticDiscoverer) Discover(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(s.peers) == 0 {
		return nil, nil
	}

	result := make([]string, len(s.peers))
	copy(result, s.peers)
	return result, nil
}
