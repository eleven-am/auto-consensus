# auto-consensus Architecture

## Overview

`auto-consensus` is a Go library that provides automatic Raft cluster formation with zero-config discovery. It combines gossip-based membership with cascading discovery to work in any environment - Kubernetes, bare metal, or local development.

**Core Principles:**
- Universal: Works in and out of Kubernetes
- Zero-config on LAN: Nodes find each other automatically via broadcast
- Explicit config supported: DNS SRV and static peers as fallbacks
- Gossip handles membership: Raft handles consensus

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                      Application                             │
└──────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────┐
│                     auto-consensus                           │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                 Discovery Cascade                      │  │
│  │                                                        │  │
│  │   UDP Broadcast → DNS SRV → Static → I'm Alone        │  │
│  │                                                        │  │
│  └────────────────────────┬───────────────────────────────┘  │
│                           │                                  │
│                           ▼                                  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                  Gossip Layer                          │  │
│  │               (hashicorp/memberlist)                   │  │
│  │                                                        │  │
│  │   - Membership discovery and propagation               │  │
│  │   - Failure detection                                  │  │
│  │   - Node metadata sharing                              │  │
│  │                                                        │  │
│  └────────────────────────┬───────────────────────────────┘  │
│                           │                                  │
│                           ▼                                  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                 Bootstrap Logic                        │  │
│  │                                                        │  │
│  │   - Wait for expected peers in gossip                  │  │
│  │   - Determine role: bootstrap vs join vs restart       │  │
│  │   - Lowest ordinal without state bootstraps            │  │
│  │                                                        │  │
│  └────────────────────────┬───────────────────────────────┘  │
│                           │                                  │
│                           ▼                                  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                    Raft Layer                          │  │
│  │                 (hashicorp/raft)                       │  │
│  │                                                        │  │
│  │   - Consensus on data                                  │  │
│  │   - Leader election                                    │  │
│  │   - Log replication                                    │  │
│  │                                                        │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

---

## Discovery Cascade

Nodes find peers through a cascading fallback mechanism. First method that finds peers wins.

```
┌─────────────────────────────────────────────────────────────┐
│                    Discovery Cascade                        │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 1. UDP Broadcast                                      │  │
│  │                                                       │  │
│  │    → Send probe to broadcast address (255.255.255.255)│  │
│  │    → Wait for replies (default: 2 seconds)            │  │
│  │    → Works: LAN, local dev, same-node K8s pods        │  │
│  │    → If peers found → done                            │  │
│  │                                                       │  │
│  └───────────────────────┬───────────────────────────────┘  │
│                          │ no peers found                   │
│                          ▼                                  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 2. DNS SRV (if configured)                            │  │
│  │                                                       │  │
│  │    → Query SRV records for service                    │  │
│  │    → Works: Kubernetes headless service, enterprise   │  │
│  │    → If peers found → done                            │  │
│  │                                                       │  │
│  └───────────────────────┬───────────────────────────────┘  │
│                          │ no peers / not configured        │
│                          ▼                                  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 3. Static Peers (if configured)                       │  │
│  │                                                       │  │
│  │    → Use explicit addresses from config               │  │
│  │    → Works: everywhere, requires manual config        │  │
│  │    → If peers found → done                            │  │
│  │                                                       │  │
│  └───────────────────────┬───────────────────────────────┘  │
│                          │ no peers / not configured        │
│                          ▼                                  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 4. I'm Alone                                          │  │
│  │                                                       │  │
│  │    → No peers discovered                              │  │
│  │    → Start gossip and listen for others               │  │
│  │    → Will bootstrap if I'm the seed node              │  │
│  │                                                       │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### Why UDP Broadcast First?

- Zero config on LAN - nodes just find each other
- Fast - no DNS lookups needed
- Works in development without any setup
- Falls through quickly if it doesn't work (2 second timeout)

### Why DNS SRV Second?

- Native Kubernetes pattern (headless services)
- Works across nodes in K8s (unlike broadcast)
- Enterprise environments often have internal DNS

### Why Static Last?

- Always works as escape hatch
- Required for WAN deployments
- Explicit is better than broken

---

## Gossip Layer

Uses `hashicorp/memberlist` (SWIM protocol) for membership.

### What Gossip Does

| Function | How It Works |
|----------|--------------|
| **Membership** | Nodes share who's in the cluster |
| **Failure Detection** | Protocol detects dead nodes |
| **Metadata** | Nodes share their Raft address, state, ordinal |

### Node Metadata

Each node shares via gossip:

```go
type NodeMeta struct {
    NodeID       string // Stable identifier (e.g., "myapp-0")
    RaftAddress  string // Address for Raft communication
    GossipAddress string // Address for gossip (usually same as memberlist)
    Ordinal      int    // Extracted from NodeID for seed election
    HasRaftState bool   // Whether node has existing Raft data
}
```

### Gossip Events

```go
// Node joined gossip cluster
OnJoin(node NodeMeta)
    → Update peer list
    → Maybe trigger Raft bootstrap check

// Node left or failed
OnLeave(node NodeMeta)
    → Update peer list
    → If Raft leader, maybe remove from Raft

// Node metadata changed
OnUpdate(node NodeMeta)
    → Update peer list
```

---

## Bootstrap Logic

Once gossip has membership, we decide how to start Raft.

### Decision Tree

```
                         ┌─────────────────┐
                         │  Gossip Ready   │
                         │  (N nodes seen) │
                         └────────┬────────┘
                                  │
                         ┌────────▼────────┐
                         │ Do I have Raft  │
                         │ state on disk?  │
                         └────────┬────────┘
                                  │
                    ┌─────────────┴─────────────┐
                    │ YES                       │ NO
                    ▼                           ▼
             ┌─────────────┐           ┌─────────────────┐
             │   RESTART   │           │ Am I lowest     │
             │ (from disk) │           │ ordinal without │
             └─────────────┘           │ Raft state?     │
                                       └────────┬────────┘
                                                │
                                  ┌─────────────┴─────────────┐
                                  │ YES                       │ NO
                                  ▼                           ▼
                           ┌─────────────┐           ┌─────────────┐
                           │  BOOTSTRAP  │           │    JOIN     │
                           │ (I'm seed)  │           │ (wait for   │
                           └─────────────┘           │   leader)   │
                                                     └─────────────┘
```

### Seed Node Election

The "seed" node that bootstraps Raft is determined by:

1. Filter nodes without existing Raft state
2. Among those, find lowest ordinal
3. That node bootstraps

```go
func electSeed(nodes []NodeMeta) string {
    // Only consider nodes without existing state
    candidates := filter(nodes, func(n NodeMeta) bool {
        return !n.HasRaftState
    })

    if len(candidates) == 0 {
        return "" // All nodes have state, no bootstrap needed
    }

    // Lowest ordinal wins
    sort.Slice(candidates, func(i, j int) bool {
        return candidates[i].Ordinal < candidates[j].Ordinal
    })

    return candidates[0].NodeID
}
```

### Why Lowest Ordinal?

- Deterministic - no race conditions
- Works with StatefulSet naming (pod-0, pod-1, pod-2)
- Works with any naming scheme that ends in numbers

---

## Configuration

```go
type Config struct {
    // Node identity
    NodeID           string // Stable unique ID (e.g., "myapp-0")
    AdvertiseAddress string // DNS name others use to reach me
    BindAddress      string // What I listen on (e.g., "0.0.0.0:9191")
    DataDir          string // Where Raft state is persisted

    // Discovery configuration
    Discovery DiscoveryConfig

    // Cluster settings
    ExpectedPeers int // How many nodes to wait for before bootstrapping
}

type DiscoveryConfig struct {
    // UDP Broadcast (enabled by default)
    Broadcast BroadcastConfig

    // DNS SRV (optional)
    DNS *DNSConfig

    // Static peers (optional)
    Static []string
}

type BroadcastConfig struct {
    Enabled bool          // Default: true
    Port    int           // Default: 7946
    Timeout time.Duration // Default: 2s
}

type DNSConfig struct {
    // SRV record to query
    // e.g., "_raft._tcp.myapp-headless.namespace.svc.cluster.local"
    Service string
}
```

---

## Address Handling

### Critical Rule: Always Use DNS Names

Addresses stored in Raft and gossip must be DNS names, not IPs.

```
✅ CORRECT: "myapp-0.myapp-headless.ns.svc.cluster.local:9191"
❌ WRONG:   "10.244.1.5:9191"
```

**Why?**

Pod restarts in Kubernetes get new IPs. DNS names stay the same.

```
Before restart:
  DNS: myapp-0.headless.svc → 10.244.1.5

After restart:
  DNS: myapp-0.headless.svc → 10.244.2.99  (new IP)

Raft log has: myapp-0.headless.svc:9191
  → Resolves to current IP
  → Connection works
```

### Application Responsibility

The application must:
1. Set up DNS advertisement (K8s does this automatically via headless service)
2. Provide the DNS name as `AdvertiseAddress`
3. Ensure the name is resolvable before and after restarts

---

## Startup Sequence

### Node A (First Node)

```
1. Start discovery cascade
   → Broadcast: no replies (I'm first)
   → DNS: no records yet (or not configured)
   → Static: not configured
   → Result: no peers

2. Start gossip, listening for others
   → Memberlist running on port 7946

3. Wait for expected peers
   → ExpectedPeers = 3, currently 1
   → Keep waiting...

4. (Later) Nodes B, C join gossip
   → Now 3 nodes in gossip

5. Check bootstrap role
   → I have no Raft state
   → I'm lowest ordinal (0)
   → I am the seed

6. Bootstrap Raft with self as only voter

7. Other nodes join Raft via me
```

### Node B (Joins Later)

```
1. Start discovery cascade
   → Broadcast: Node A replies!
   → Result: peer found

2. Join gossip via Node A
   → Learn about Node A (and C if present)

3. Wait for expected peers
   → Wait until 3 nodes in gossip

4. Check bootstrap role
   → I have no Raft state
   → I'm ordinal 1, Node A is ordinal 0
   → I am NOT the seed

5. Wait for Raft leader
   → Node A bootstrapped and is leader

6. Join Raft cluster
   → Request to join via leader
   → Leader adds me as voter
```

### Node A (Restart After Crash)

```
1. Start discovery cascade
   → Broadcast: Node B, C reply
   → Result: peers found

2. Join gossip via any peer
   → Learn full membership

3. Check bootstrap role
   → I HAVE Raft state on disk
   → Mode: RESTART

4. Restart Raft from disk
   → Load log, snapshots
   → Rejoin cluster automatically
```

---

## Ports

| Port | Protocol | Purpose |
|------|----------|---------|
| 7946 | TCP/UDP | Gossip (memberlist) |
| 9191 | TCP | Raft consensus |

Both are configurable.

---

## File Structure

```
auto-consensus/
├── consensus.go           # Main entry point, Manager type
├── config.go              # Configuration types
├── discovery/
│   ├── discovery.go       # Discovery cascade logic
│   ├── broadcast.go       # UDP broadcast discovery
│   ├── dns.go             # DNS SRV discovery
│   └── static.go          # Static peer list
├── gossip/
│   ├── gossip.go          # Memberlist wrapper
│   ├── delegate.go        # Memberlist delegate (metadata)
│   └── events.go          # Join/leave event handling
├── bootstrap/
│   ├── bootstrap.go       # Bootstrap decision logic
│   └── seed.go            # Seed election
├── raft/
│   ├── raft.go            # Hashicorp raft wrapper
│   ├── fsm.go             # State machine interface
│   └── transport.go       # Raft transport
├── errors.go              # Error definitions
└── docs/
    ├── design.md          # Original design notes
    ├── raft-initialization-research.md
    └── architecture.md    # This file
```

---

## Usage Example

### Kubernetes StatefulSet

```go
package main

import (
    "fmt"
    "os"

    "github.com/eleven-am/auto-consensus"
)

func main() {
    podName := os.Getenv("POD_NAME")
    namespace := os.Getenv("POD_NAMESPACE")
    headlessService := os.Getenv("HEADLESS_SERVICE")

    cfg := autoconsensus.Config{
        NodeID:           podName,
        AdvertiseAddress: fmt.Sprintf("%s.%s.%s.svc.cluster.local:9191",
                                       podName, headlessService, namespace),
        BindAddress:      "0.0.0.0:9191",
        DataDir:          "/data/raft",
        ExpectedPeers:    3,
        Discovery: autoconsensus.DiscoveryConfig{
            DNS: &autoconsensus.DNSConfig{
                Service: fmt.Sprintf("_raft._tcp.%s.%s.svc.cluster.local",
                                     headlessService, namespace),
            },
        },
    }

    node, err := autoconsensus.New(cfg, myFSM)
    if err != nil {
        panic(err)
    }

    if err := node.Start(); err != nil {
        panic(err)
    }

    // Use the cluster...
    select {}
}
```

### Local Development (Zero Config)

```go
package main

import (
    "github.com/eleven-am/auto-consensus"
)

func main() {
    cfg := autoconsensus.Config{
        NodeID:           "dev-node-1",
        AdvertiseAddress: "dev-node-1.local:9191",
        BindAddress:      "0.0.0.0:9191",
        DataDir:          "./data",
        ExpectedPeers:    3,
        // No discovery config needed - broadcast works on LAN
    }

    node, err := autoconsensus.New(cfg, myFSM)
    if err != nil {
        panic(err)
    }

    if err := node.Start(); err != nil {
        panic(err)
    }

    select {}
}
```

---

## Error Handling

```go
var (
    ErrNoLeader          = errors.New("no leader elected")
    ErrNotLeader         = errors.New("not the leader")
    ErrBootstrapTimeout  = errors.New("timed out waiting for peers")
    ErrJoinFailed        = errors.New("failed to join cluster")
    ErrAlreadyRunning    = errors.New("node already running")
    ErrNotRunning        = errors.New("node not running")
)
```

---

## Recovery Scenarios

| Scenario | What Happens |
|----------|--------------|
| **Single node crashes** | Other nodes detect via gossip, continue. Crashed node restarts and rejoins. |
| **Leader crashes** | Raft elects new leader. Old leader restarts and rejoins as follower. |
| **Minority crashes** | Cluster continues (quorum maintained). Crashed nodes rejoin on restart. |
| **Majority crashes** | Cluster stalls (no quorum). Nodes restart, detect state, re-elect leader. |
| **All nodes crash** | All restart, all have state, no bootstrap needed, re-elect leader. |
| **Fresh cluster start** | Gossip forms first, lowest ordinal bootstraps, others join. |
