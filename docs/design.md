# auto-consensus Design Document

## Overview

`auto-consensus` is a Go library that handles Raft cluster formation and membership management, specifically designed for Kubernetes StatefulSet deployments. It wraps the complexity of bootstrap decisions, peer discovery, and cluster joining into a simple, deterministic API.

**Core Principle:** No runtime probing before bootstrap. Roles are determined by configuration and state, not by querying peers.

---

## Goals

1. **Deterministic startup** - Same inputs always produce same behavior
2. **State-aware** - Automatically detect fresh vs existing node
3. **K8s-native** - First-class support for StatefulSet patterns
4. **Pluggable** - Work with different Raft implementations
5. **Simple API** - One function to start, library handles the rest

## Non-Goals

1. Not a Raft implementation - wraps existing libraries
2. Not a service mesh - just cluster formation
3. Not multi-cluster - single cluster focus

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Application                          │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                   auto-consensus                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │
│  │  Resolver   │  │  Bootstrap  │  │    Membership   │  │
│  │  (Discovery)│  │   Manager   │  │     Manager     │  │
│  └─────────────┘  └─────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│              Raft Implementation Adapter                │
│         (hashicorp/raft, etcd/raft, dragonboat)         │
└─────────────────────────────────────────────────────────┘
```

---

## Core Components

### 1. Resolver (Peer Discovery)

Responsible for determining **who** the peers are. Not whether they're healthy - just their addresses.

```go
type Resolver interface {
    // Resolve returns the list of peer addresses for the cluster.
    // This is configuration/DNS based, not health-based.
    Resolve(ctx context.Context) ([]Peer, error)
}

type Peer struct {
    ID      string // Stable identifier (e.g., "node-0", "node-1")
    Address string // Network address (e.g., "node-0.svc:9191")
}
```

**Implementations:**

| Resolver | Use Case |
|----------|----------|
| `StaticResolver` | Fixed list of peers |
| `DNSResolver` | Kubernetes headless service SRV lookup |
| `K8sResolver` | Kubernetes API with label selector |

### 2. Bootstrap Manager

Responsible for the **startup decision tree**:

```
                    ┌─────────────┐
                    │   Start()   │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ Has State?  │
                    └──────┬──────┘
                           │
              ┌────────────┴────────────┐
              │ YES                     │ NO
              ▼                         ▼
       ┌─────────────┐          ┌─────────────┐
       │   Restart   │          │ Is Seed?    │
       │ (from disk) │          └──────┬──────┘
       └─────────────┘                 │
                            ┌──────────┴──────────┐
                            │ YES                 │ NO
                            ▼                     ▼
                     ┌─────────────┐       ┌─────────────┐
                     │  Bootstrap  │       │    Join     │
                     │   (self)    │       │   (seed)    │
                     └─────────────┘       └─────────────┘
```

```go
type BootstrapManager struct {
    config      Config
    resolver    Resolver
    stateStore  StateStore
    raft        RaftAdapter
}

type StartResult struct {
    Mode      StartMode // Bootstrap, Join, Restart
    LeaderID  string
    Peers     []Peer
}

type StartMode int

const (
    ModeBootstrap StartMode = iota // Fresh seed node
    ModeJoin                       // Fresh non-seed node
    ModeRestart                    // Existing node with state
)
```

### 3. Membership Manager

Handles **runtime** cluster changes after bootstrap:

```go
type MembershipManager interface {
    // AddVoter adds a new voting member to the cluster.
    // Only callable on leader.
    AddVoter(ctx context.Context, id string, address string) error

    // RemoveServer removes a member from the cluster.
    RemoveServer(ctx context.Context, id string) error

    // GetConfiguration returns current cluster membership.
    GetConfiguration(ctx context.Context) ([]Peer, error)

    // IsLeader returns true if this node is the current leader.
    IsLeader() bool

    // LeaderAddress returns the current leader's address.
    LeaderAddress() (string, error)
}
```

### 4. Raft Adapter

Abstract interface to support multiple Raft implementations:

```go
type RaftAdapter interface {
    // Bootstrap initializes a new cluster with self as only member.
    Bootstrap(config BootstrapConfig) error

    // Join attempts to join an existing cluster via the given address.
    Join(ctx context.Context, leaderAddr string, self Peer) error

    // Restart recovers from existing state on disk.
    Restart() error

    // HasExistingState returns true if there's persisted Raft state.
    HasExistingState() (bool, error)

    // Shutdown gracefully stops the Raft node.
    Shutdown(ctx context.Context) error

    // Membership operations
    AddVoter(id string, address string) error
    RemoveServer(id string) error
    GetConfiguration() ([]Peer, error)
    IsLeader() bool
    LeaderAddress() (string, error)
}
```

---

## Configuration

```go
type Config struct {
    // NodeID is a unique, stable identifier for this node.
    // In K8s, typically the pod name (e.g., "myapp-0").
    NodeID string

    // BindAddress is the address this node listens on.
    BindAddress string

    // AdvertiseAddress is the address other nodes use to reach this node.
    // In K8s, typically "<pod>.<headless-service>:<port>".
    AdvertiseAddress string

    // DataDir is where Raft state is persisted.
    DataDir string

    // Bootstrap configuration
    Bootstrap BootstrapConfig
}

type BootstrapConfig struct {
    // SeedStrategy determines how the seed node is identified.
    SeedStrategy SeedStrategy

    // ExpectedPeers is the number of peers expected in the cluster.
    // Used with SeedStrategyLowestOrdinal and SeedStrategyExpect.
    ExpectedPeers int

    // SeedNodeID explicitly sets which node is the seed.
    // Used with SeedStrategyExplicit.
    SeedNodeID string

    // JoinTimeout is how long non-seed nodes wait to join.
    JoinTimeout time.Duration

    // JoinRetryInterval is the backoff between join attempts.
    JoinRetryInterval time.Duration
}

type SeedStrategy int

const (
    // SeedStrategyLowestOrdinal uses the lowest ordinal (pod-0) as seed.
    // Extracts ordinal from NodeID suffix.
    SeedStrategyLowestOrdinal SeedStrategy = iota

    // SeedStrategyExplicit uses a specific node ID as seed.
    SeedStrategyExplicit

    // SeedStrategyExpect waits for N nodes, lowest ordinal bootstraps.
    // Similar to Consul's bootstrap-expect.
    SeedStrategyExpect
)
```

---

## Kubernetes Integration

### DNS Resolver

```go
type DNSResolverConfig struct {
    // HeadlessService is the full DNS name of the headless service.
    // e.g., "myapp-headless.namespace.svc.cluster.local"
    HeadlessService string

    // PortName is the named port in the service (for SRV lookup).
    // e.g., "raft"
    PortName string

    // ExpectedPeers is how many peers to expect.
    // Resolver waits until this many are resolvable.
    ExpectedPeers int
}

func NewDNSResolver(config DNSResolverConfig) Resolver
```

### Ordinal Extraction

```go
// ExtractOrdinal parses the ordinal from a StatefulSet pod name.
// "myapp-0" -> 0, "myapp-12" -> 12
func ExtractOrdinal(podName string) (int, error)

// IsLowestOrdinal returns true if this node has the lowest ordinal
// among the resolved peers.
func IsLowestOrdinal(self string, peers []Peer) bool
```

---

## Usage Example

### Basic Kubernetes StatefulSet

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/eleven-am/auto-consensus"
    "github.com/eleven-am/auto-consensus/resolver"
    "github.com/eleven-am/auto-consensus/adapter/hashicorp"
)

func main() {
    podName := os.Getenv("POD_NAME")           // e.g., "myapp-0"
    headlessSvc := os.Getenv("HEADLESS_SVC")   // e.g., "myapp-headless.ns.svc.cluster.local"

    // Create DNS resolver for peer discovery
    res := resolver.NewDNS(resolver.DNSConfig{
        HeadlessService: headlessSvc,
        PortName:        "raft",
        ExpectedPeers:   3,
    })

    // Create the consensus manager
    mgr, err := autoconsensus.New(autoconsensus.Config{
        NodeID:           podName,
        BindAddress:      "0.0.0.0:9191",
        AdvertiseAddress: fmt.Sprintf("%s.%s:9191", podName, headlessSvc),
        DataDir:          "/data/raft",
        Bootstrap: autoconsensus.BootstrapConfig{
            SeedStrategy:      autoconsensus.SeedStrategyLowestOrdinal,
            ExpectedPeers:     3,
            JoinTimeout:       5 * time.Minute,
            JoinRetryInterval: 2 * time.Second,
        },
    }, res, hashicorp.NewAdapter())

    // Start - this handles bootstrap/join/restart automatically
    result, err := mgr.Start(context.Background())
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Started in %s mode, leader: %s", result.Mode, result.LeaderID)

    // Use the cluster...
    if mgr.IsLeader() {
        // Do leader things
    }

    // Graceful shutdown
    mgr.Shutdown(context.Background())
}
```

---

## Startup Sequence Detail

### Seed Node (pod-0) - Fresh Start

```
1. Check DataDir for existing state -> None found
2. Determine role: ordinal=0, SeedStrategy=LowestOrdinal -> I am seed
3. Bootstrap cluster with self as only voter
4. Start Raft, become leader
5. Wait for join requests from other nodes
6. Process AddVoter for each joining node
```

### Non-Seed Node (pod-1, pod-2) - Fresh Start

```
1. Check DataDir for existing state -> None found
2. Determine role: ordinal>0 -> I am not seed
3. Resolve seed address via DNS -> "myapp-0.headless:9191"
4. Loop:
   a. Attempt to join seed
   b. If success -> break
   c. If failure -> wait JoinRetryInterval, retry
   d. If timeout -> return error
5. Start Raft as follower
```

### Any Node - Restart

```
1. Check DataDir for existing state -> State found
2. Load Raft state from disk
3. Restart Raft (membership comes from log)
4. Rejoin cluster automatically via persisted peer info
```

---

## Error Handling

### Join Failures

```go
var (
    ErrJoinTimeout     = errors.New("timed out waiting to join cluster")
    ErrSeedUnavailable = errors.New("seed node not reachable")
    ErrAlreadyMember   = errors.New("node already member of cluster")
    ErrNotLeader       = errors.New("not the leader")
)
```

### Recovery Scenarios

| Scenario | Behavior |
|----------|----------|
| Seed crashes during bootstrap | Other nodes wait, seed restarts and completes |
| Non-seed crashes during join | Restarts, retries join |
| Leader crashes after bootstrap | Raft elects new leader, crashed node rejoins |
| All nodes crash | All restart, reload from disk, re-elect |
| Data corruption on one node | Must be removed and re-added as new node |

---

## File Structure

```
auto-consensus/
├── consensus.go          # Main Manager type and Start()
├── config.go             # Configuration types
├── bootstrap.go          # Bootstrap decision logic
├── membership.go         # Runtime membership operations
├── errors.go             # Error definitions
├── resolver/
│   ├── resolver.go       # Resolver interface
│   ├── static.go         # StaticResolver
│   ├── dns.go            # DNSResolver
│   └── k8s.go            # K8sResolver (API-based)
├── adapter/
│   ├── adapter.go        # RaftAdapter interface
│   ├── hashicorp/        # hashicorp/raft adapter
│   │   └── adapter.go
│   └── etcd/             # etcd/raft adapter (future)
│       └── adapter.go
└── docs/
    ├── design.md
    └── raft-initialization-research.md
```

---

## Open Questions

1. **Should the library manage transport?** Or let the adapter handle it?

2. **How to handle split-brain during network partition?** Rely on Raft's built-in handling?

3. **Should we support bootstrap-expect pattern?** (Wait for N nodes before any bootstraps) Or stick with seed-node pattern?

4. **Metrics/observability?** Expose Prometheus metrics for join attempts, failures, etc.?

5. **Leader forwarding?** Should non-leaders forward write requests to leader, or return error?
