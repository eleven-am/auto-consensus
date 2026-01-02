package discovery

import (
	"context"
	"fmt"
)

type CascadeDiscoverer struct {
	discoverers []Discoverer
}

func NewCascade(discoverers ...Discoverer) *CascadeDiscoverer {
	return &CascadeDiscoverer{discoverers: discoverers}
}

func (c *CascadeDiscoverer) Discover(ctx context.Context) ([]string, error) {
	if len(c.discoverers) == 0 {
		return nil, nil
	}

	var lastErr error
	for i, d := range c.discoverers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		peers, err := d.Discover(ctx)
		if err != nil {
			lastErr = fmt.Errorf("discoverer %d: %w", i, err)
			continue
		}

		if len(peers) > 0 {
			return peers, nil
		}
	}

	return nil, lastErr
}
