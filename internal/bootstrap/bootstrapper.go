package bootstrap

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/eleven-am/auto-consensus/internal/discovery"
	"github.com/eleven-am/auto-consensus/internal/gossip"
)

var (
	ErrAlreadyRunning = errors.New("bootstrapper already running")
	ErrNotRunning     = errors.New("bootstrapper not running")
)

const (
	initialBackoff = 250 * time.Millisecond
	maxBackoff     = 15 * time.Second
	jitterFactor   = 0.5
)

type StateChange struct {
	Old State
	New State
}

type Bootstrapper struct {
	config         *Config
	state          State
	mdnsAdvertiser *MDNSAdvertiser
	gossip         *gossip.Gossip
	discoverer     discovery.Discoverer
	didBootstrap   bool
	mu             sync.RWMutex
	stopCh         chan struct{}
	stoppedCh      chan struct{}
	subscribers    []chan StateChange
	subMu          sync.RWMutex
}

func New(cfg *Config, disc discovery.Discoverer) (*Bootstrapper, error) {
	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &Bootstrapper{
		config:     cfg,
		state:      StateInit,
		discoverer: disc,
	}, nil
}

func (b *Bootstrapper) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.state != StateInit && b.state != StateFailed {
		b.mu.Unlock()
		return ErrAlreadyRunning
	}
	b.state = StateInit
	b.stopCh = make(chan struct{})
	b.stoppedCh = make(chan struct{})
	b.mu.Unlock()

	go func() {
		defer close(b.stoppedCh)
		b.run(ctx)
	}()

	return nil
}

func (b *Bootstrapper) run(ctx context.Context) {
	if err := b.startMDNSAdvertiser(); err != nil {
		b.transitionTo(StateFailed)
		b.log("failed to start mDNS advertiser: %v", err)
		return
	}

	if err := b.startGossip(); err != nil {
		b.transitionTo(StateFailed)
		b.log("failed to start gossip: %v", err)
		return
	}

	attempt := 0
	backoff := initialBackoff

	for {
		select {
		case <-ctx.Done():
			b.transitionTo(StateFailed)
			return
		case <-b.stopCh:
			return
		default:
		}

		b.transitionTo(StateDiscovering)
		peers, hasBootstrappedPeer, err := b.runDiscovery(ctx)

		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			b.log("discovery error: %v", err)
		}

		if hasBootstrappedPeer && len(peers) > 0 {
			b.transitionTo(StateJoining)
			if err := b.joinCluster(peers); err != nil {
				b.log("join failed: %v, retrying", err)
				b.transitionTo(StateRetrying)
				attempt++
				if b.shouldFail(attempt) {
					b.transitionTo(StateFailed)
					return
				}
				b.waitBackoff(ctx, backoff)
				backoff = b.nextBackoff(backoff)
				continue
			}
			b.setBootstrapped(true)
			b.transitionTo(StateRunning)
			return
		}

		if b.config.CanBootstrap {
			if b.shouldBootstrap(ctx) {
				b.transitionTo(StateBootstrapping)
				b.mu.Lock()
				b.didBootstrap = true
				b.mu.Unlock()
				b.setBootstrapped(true)
				b.transitionTo(StateRunning)
				return
			}
		}

		b.transitionTo(StateRetrying)
		attempt++

		if b.shouldFail(attempt) {
			b.transitionTo(StateFailed)
			return
		}

		b.log("no bootstrapped peers found, retry %d", attempt)
		b.waitBackoff(ctx, backoff)
		backoff = b.nextBackoff(backoff)
	}
}

func (b *Bootstrapper) Stop() error {
	b.mu.Lock()
	state := b.state
	b.mu.Unlock()

	if state == StateInit {
		return ErrNotRunning
	}

	select {
	case <-b.stopCh:
	default:
		close(b.stopCh)
	}

	<-b.stoppedCh

	if b.gossip != nil {
		b.gossip.Stop()
	}
	if b.mdnsAdvertiser != nil {
		b.mdnsAdvertiser.Stop()
	}

	b.mu.Lock()
	b.state = StateInit
	b.mu.Unlock()

	return nil
}

func (b *Bootstrapper) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

func (b *Bootstrapper) RaftAddress() string {
	return b.config.RaftAdvertiseAddr
}

func (b *Bootstrapper) Gossip() *gossip.Gossip {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.gossip
}

func (b *Bootstrapper) DidBootstrap() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state == StateRunning && b.didBootstrap
}

func (b *Bootstrapper) startMDNSAdvertiser() error {
	_, portStr, _ := net.SplitHostPort(b.config.GossipAdvertiseAddr)
	port, _ := strconv.Atoi(portStr)

	b.mdnsAdvertiser = NewMDNSAdvertiser(MDNSAdvertiserConfig{
		NodeID:     b.config.NodeID,
		Service:    b.config.MDNSService,
		Domain:     b.config.MDNSDomain,
		Port:       port,
		SecretKey:  b.config.SecretKey,
		GossipAddr: b.config.GossipAdvertiseAddr,
	})

	if err := b.mdnsAdvertiser.Start(); err != nil {
		return err
	}

	b.transitionTo(StateMDNSAdvertiserUp)
	return nil
}

func (b *Bootstrapper) startGossip() error {
	g, err := gossip.New(&gossip.Config{
		NodeID:        b.config.NodeID,
		BindAddr:      b.config.GossipAddr,
		AdvertiseAddr: b.config.GossipAdvertiseAddr,
		RaftAddr:      b.config.RaftAdvertiseAddr,
		SecretKey:     b.config.SecretKey,
		Mode:          b.config.NetworkMode,
		Logger:        b.config.Logger,
	}, b.config.EventHandler)
	if err != nil {
		return err
	}

	if err := g.Start(); err != nil {
		return err
	}

	b.mu.Lock()
	b.gossip = g
	b.mu.Unlock()

	b.transitionTo(StateGossipUp)
	return nil
}

func (b *Bootstrapper) runDiscovery(ctx context.Context) ([]string, bool, error) {
	if b.discoverer == nil {
		return nil, false, nil
	}

	discCtx, cancel := context.WithTimeout(ctx, b.config.DiscoveryTimeout)
	defer cancel()

	if bd, ok := b.discoverer.(discovery.BootstrapDiscoverer); ok {
		peers, err := bd.DiscoverPeers(discCtx)
		if err != nil {
			return nil, false, err
		}

		addrs := make([]string, len(peers))
		hasBootstrapped := false
		for i, p := range peers {
			addrs[i] = p.Address
			if p.Bootstrapped {
				hasBootstrapped = true
			}
		}
		return addrs, hasBootstrapped, nil
	}

	addrs, err := b.discoverer.Discover(discCtx)
	if err != nil {
		return nil, false, err
	}

	if len(addrs) > 0 {
		b.mu.RLock()
		g := b.gossip
		b.mu.RUnlock()

		if g != nil {
			if _, err := g.Join(addrs); err == nil {
				if g.NumMembers() > 1 {
					for _, m := range g.Members() {
						if m.ID != b.config.NodeID && m.Bootstrapped {
							return addrs, true, nil
						}
					}
				}
			}
		}
	}

	return addrs, false, nil
}

func (b *Bootstrapper) joinCluster(peers []string) error {
	b.mu.RLock()
	g := b.gossip
	b.mu.RUnlock()

	if g == nil {
		return errors.New("gossip not initialized")
	}

	n, err := g.Join(peers)
	if err != nil {
		return err
	}

	if n == 0 {
		return errors.New("failed to join any peers")
	}

	return nil
}

func (b *Bootstrapper) shouldBootstrap(ctx context.Context) bool {
	if !b.waitForBootstrapSlot(ctx) {
		return false
	}

	_, hasBootstrapped, _ := b.runDiscovery(ctx)
	if hasBootstrapped {
		return false
	}

	b.mu.RLock()
	g := b.gossip
	b.mu.RUnlock()

	if g != nil && g.NumMembers() > 1 {
		return false
	}

	return true
}

func (b *Bootstrapper) waitForBootstrapSlot(ctx context.Context) bool {
	hashDelay := b.hashBasedDelay()
	b.log("waiting %v before bootstrap decision (hash-based)", hashDelay)

	select {
	case <-ctx.Done():
		return false
	case <-b.stopCh:
		return false
	case <-time.After(hashDelay):
		return true
	}
}

func (b *Bootstrapper) hashBasedDelay() time.Duration {
	hash := sha256.Sum256([]byte(b.config.NodeID))

	var hashValue uint64
	for i := 0; i < 8; i++ {
		hashValue = (hashValue << 8) | uint64(hash[i])
	}

	fraction := float64(hashValue) / float64(^uint64(0))

	baseDelay := b.config.DiscoveryTimeout
	maxJitter := time.Second
	jitteredDelay := baseDelay + time.Duration(fraction*float64(maxJitter))

	return jitteredDelay
}

func (b *Bootstrapper) shouldFail(attempt int) bool {
	if b.config.MaxDiscoveryRetries == 0 {
		return false
	}
	return attempt >= b.config.MaxDiscoveryRetries
}

func (b *Bootstrapper) waitBackoff(ctx context.Context, d time.Duration) {
	jitter := time.Duration(float64(d) * jitterFactor * rand.Float64())
	select {
	case <-ctx.Done():
	case <-b.stopCh:
	case <-time.After(d + jitter):
	}
}

func (b *Bootstrapper) nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}

func (b *Bootstrapper) transitionTo(state State) {
	b.mu.Lock()
	old := b.state
	b.state = state
	b.mu.Unlock()

	b.log("state -> %s", state)

	if old != state {
		b.broadcast(StateChange{Old: old, New: state})
	}
}

func (b *Bootstrapper) broadcast(change StateChange) {
	b.subMu.RLock()
	defer b.subMu.RUnlock()

	for _, ch := range b.subscribers {
		select {
		case ch <- change:
		default:
		}
	}
}

func (b *Bootstrapper) Subscribe() <-chan StateChange {
	ch := make(chan StateChange, 16)

	b.subMu.Lock()
	b.subscribers = append(b.subscribers, ch)
	b.subMu.Unlock()

	return ch
}

func (b *Bootstrapper) setBootstrapped(bootstrapped bool) {
	if b.mdnsAdvertiser != nil {
		b.mdnsAdvertiser.SetBootstrapped(bootstrapped)
	}
	b.mu.RLock()
	g := b.gossip
	b.mu.RUnlock()
	if g != nil {
		g.SetBootstrapped(bootstrapped)
	}
}

func (b *Bootstrapper) log(format string, args ...any) {
	if b.config.Logger != nil {
		b.config.Logger.Debug(fmt.Sprintf(format, args...))
	}
}
