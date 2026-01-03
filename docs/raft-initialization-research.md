# Raft Cluster Initialization Research Report

## 1. Core Raft Bootstrap Concepts

### The Fundamental Problem

Raft requires a **quorum** (majority) of nodes to agree before electing a leader. On a fresh cluster:

- No node knows who the others are
- No persisted state exists
- Someone must "go first" to establish the initial configuration

### Two Primary Approaches

| Approach | Description | Used By |
|----------|-------------|---------|
| **Single-node bootstrap then join** | One node self-elects as leader, others join sequentially | Consul, Vault, CockroachDB |
| **Multi-node simultaneous start** | All nodes start together with known peer list, wait for quorum | etcd, static clusters |

---

## 2. Implementation Patterns

### Pattern A: `bootstrap-expect` (HashiCorp Style)

Used by: [Consul](https://developer.hashicorp.com/consul/docs/deploy/server/vm/bootstrap), Nomad

```hcl
bootstrap_expect = 3
retry_join = ["node1:8301", "node2:8301", "node3:8301"]
```

**How it works:**

1. Each node starts and knows it needs N peers
2. Nodes discover each other via `retry_join` addresses
3. Once N nodes are visible, leader election begins
4. **Critical:** `bootstrap_expect` is only checked when `data_dir` is empty

**Key insight from [HashiCorp docs](https://developer.hashicorp.com/consul/docs/reference/agent/configuration-file/bootstrap):**

> "The servers will not elect themselves leader until the number specified in bootstrap-expect comes up."

### Pattern B: Static `initial-cluster` (etcd Style)

Used by: [etcd](https://etcd.io/docs/v3.7/op-guide/clustering/), Kubernetes control plane

```bash
--initial-cluster=node1=http://10.0.0.1:2380,node2=http://10.0.0.2:2380,node3=http://10.0.0.3:2380
--initial-cluster-state=new
--initial-cluster-token=my-cluster
```

**How it works:**

1. All nodes start with the **complete peer list** pre-configured
2. Each node's `initial-advertise-peer-urls` must match what's in `initial-cluster`
3. `--initial-cluster-state=new` for fresh cluster, `existing` for joining
4. These flags are **ignored on restart** (state comes from disk)

### Pattern C: Seed Node / Pod-0 Bootstrap (K8s Native)

Used by: [RabbitMQ 4.1](https://www.rabbitmq.com/blog/2025/04/04/new-k8s-peer-discovery), Cassandra, many StatefulSet deployments

```go
if podOrdinal == 0 {
    // Bootstrap as initial leader
    startAsBootstrapNode()
} else {
    // Join pod-0
    joinCluster("pod-0.headless-svc:port")
}
```

**How it works:**

1. Pod-0 is the deterministic "seed" node
2. Pod-0 bootstraps the cluster with itself as the only initial member
3. Other pods (1, 2, ...) join by contacting pod-0
4. If pod-0 isn't up, other pods **wait forever** (first start only)

**From RabbitMQ's approach:**

> "The new plugin just assumes that a pod with -0 suffix will exist and treats it as the 'seed' node. Peer discovery only happens when a node starts for the first time."

### Pattern D: `retry_join` with auto-discovery (Vault Raft)

Used by: [Vault Integrated Storage](https://developer.hashicorp.com/vault/tutorials/kubernetes/kubernetes-raft-deployment-guide)

```hcl
storage "raft" {
  retry_join {
    leader_api_addr = "https://vault-0.vault-internal:8200"
  }
  retry_join {
    leader_api_addr = "https://vault-1.vault-internal:8200"
  }
  retry_join {
    leader_api_addr = "https://vault-2.vault-internal:8200"
  }
}
```

**How it works:**

1. Each node has a list of potential leaders to contact
2. Nodes continuously retry joining until successful
3. First initialized node becomes leader
4. Uses predictable StatefulSet DNS names

---

## 3. The etcd Raft Library API

From [go.etcd.io/raft/v3](https://pkg.go.dev/go.etcd.io/raft/v3):

### `StartNode` - New Cluster

```go
n := raft.StartNode(config, []raft.Peer{
    {ID: 0x01}, {ID: 0x02}, {ID: 0x03},
})
```

- Takes list of all initial peers
- **Peers must not be empty** - appends ConfChangeAddNode for each
- All nodes must call this with identical peer list

### `RestartNode` - Existing Node Restart

```go
n := raft.RestartNode(config)
```

- **No peer list** - membership restored from Storage
- Used when node already has persisted state

### `Bootstrap` (RawNode)

```go
rn.Bootstrap([]raft.Peer{{ID: 0x01}, {ID: 0x02}, {ID: 0x03}})
```

- Returns error if Storage is non-empty
- Recommended: manually set up Storage instead

### Adding New Node to Existing Cluster

```go
// On existing leader:
n.ProposeConfChange(ctx, raftpb.ConfChange{
    Type:   raftpb.ConfChangeAddNode,
    NodeID: newNodeID,
})

// On new node - start with EMPTY peer list:
n := raft.StartNode(config, nil)
```

---

## 4. HashiCorp Raft Library API

From [github.com/hashicorp/raft](https://pkg.go.dev/github.com/hashicorp/raft):

### `BootstrapCluster`

```go
func BootstrapCluster(conf *Config, logs LogStore, stable StableStore,
    snaps SnapshotStore, trans Transport, configuration Configuration) error
```

**Key rules:**

- Only call once per cluster lifetime
- Must use identical configuration on all Voter servers
- Returns `ErrCantBootstrap` if already bootstrapped (safe to ignore)
- Non-voters don't need to be bootstrapped

**Recommended approach:**

> "Bootstrap a single server with a configuration listing just itself as a Voter, then invoke AddVoter() on it to add other servers."

### `AddVoter` / `AddNonvoter`

```go
future := raft.AddVoter(serverID, serverAddr, 0, timeout)
if err := future.Error(); err != nil {
    // handle error
}
```

---

## 5. Kubernetes-Specific Considerations

### DNS Discovery

[Kubernetes DNS spec](https://github.com/kubernetes/dns/blob/master/docs/specification.md) provides:

**Headless Service A Records:**

```
<pod-name>.<service-name>.<namespace>.svc.cluster.local
```

Example: `flow-0.flow-headless.flow.svc.cluster.local`

**SRV Records for Named Ports:**

```
_<port-name>._<proto>.<service>.<namespace>.svc.cluster.local
```

### Critical Setting: `publishNotReadyAddresses`

```yaml
spec:
  publishNotReadyAddresses: true
  clusterIP: None
```

From [Akka documentation](https://doc.akka.io/docs/akka-management/current/bootstrap/kubernetes.html):

> "This will result in SRV records being published for the service that contain the nodes that are not ready. This allows bootstrap to find them and form the cluster thus making them ready."

### StatefulSet Ordering

- Pods created in order: 0, 1, 2, ...
- Pods terminated in reverse: 2, 1, 0, ...
- Each pod has stable identity: `<statefulset-name>-<ordinal>`

---

## 6. Common Pitfalls

### 1. IP Addresses as Node IDs

From [etcd docs](https://pkg.go.dev/go.etcd.io/raft/v3):

> "IP addresses make poor node IDs since they may be reused. A given ID MUST be used only once, even if the old node was removed."

### 2. Simultaneous Node Joins

From [Consul docs](https://developer.hashicorp.com/consul/docs/concept/consensus):

> "If multiple nodes join the cluster simultaneously, the cluster may exceed the expected failure tolerance, quorum may be lost, and the cluster can fail."

### 3. Chicken-and-Egg on Fresh Clusters

Problem: Probing peers requires metadata, but metadata only exists after bootstrap.

**Solution patterns:**

- Don't probe before bootstrap - just attempt to join
- Use deterministic seed node (pod-0)
- Accept that first node self-elects, others join

### 4. `initial-cluster-state` Mismatch

From [etcd docs](https://etcd.io/docs/v3.7/op-guide/clustering/):

> "If the wrong value is set, etcd will attempt to start but fail safely."

---

## 7. Recommended Pattern for Kubernetes

Based on research, the most robust pattern combines:

1. **Deterministic seed (pod-0)** - Always the bootstrap node
2. **DNS-based discovery** - Use headless service with `publishNotReadyAddresses: true`
3. **Retry-based joining** - Non-seed nodes retry joining until successful
4. **State-aware behavior** - Check for existing state before bootstrapping

```
Pod-0:
  if no_existing_state:
    BootstrapCluster(self_only_config)
  start_and_serve()

Pod-N (N > 0):
  loop:
    if pod-0.is_reachable():
      join_cluster(pod-0)
      break
    sleep(backoff)
  start_and_serve()
```

---

## 8. Key Decisions for Implementation

### Node ID Strategy

| Option | Pros | Cons |
|--------|------|------|
| UUID | Globally unique, never reused | Must persist, not human-readable |
| Hostname (pod name) | Predictable, human-readable | Could be reused if pod replaced |
| Hash of hostname | Stable, deterministic | Collision risk (low) |

### Bootstrap Trigger

| Option | Description |
|--------|-------------|
| Lowest ordinal | Pod-0 always bootstraps |
| First to start | Race condition, needs distributed lock |
| External signal | Operator/init container triggers bootstrap |

### Peer Discovery

| Method | Use Case |
|--------|----------|
| Static list | Known, fixed cluster size |
| DNS SRV | Kubernetes with headless service |
| API query | Dynamic environments, cloud providers |
| Gossip | Large clusters, eventually consistent |

---

## Sources

- [Raft Consensus Algorithm](https://raft.github.io/)
- [Raft Wikipedia](https://en.wikipedia.org/wiki/Raft_(algorithm))
- [etcd Clustering Guide](https://etcd.io/docs/v3.7/op-guide/clustering/)
- [HashiCorp Consul Bootstrap](https://developer.hashicorp.com/consul/docs/deploy/server/vm/bootstrap)
- [HashiCorp Consul Consensus](https://developer.hashicorp.com/consul/docs/architecture/consensus)
- [HashiCorp Raft Library](https://pkg.go.dev/github.com/hashicorp/raft)
- [HashiCorp Raft GitHub](https://github.com/hashicorp/raft)
- [etcd Raft Library](https://pkg.go.dev/go.etcd.io/raft/v3)
- [etcd Raft GitHub](https://github.com/etcd-io/raft)
- [Vault on Kubernetes](https://developer.hashicorp.com/vault/tutorials/kubernetes/kubernetes-raft-deployment-guide)
- [Vault Integrated Storage](https://developer.hashicorp.com/vault/docs/configuration/storage/raft)
- [CockroachDB Kubernetes Deployment](https://www.cockroachlabs.com/docs/stable/deploy-cockroachdb-with-kubernetes)
- [CockroachDB cockroach init](https://www.cockroachlabs.com/docs/stable/cockroach-init)
- [Kubernetes DNS Specification](https://github.com/kubernetes/dns/blob/master/docs/specification.md)
- [Kubernetes DNS for Services and Pods](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/)
- [RabbitMQ 4.1 K8s Peer Discovery](https://www.rabbitmq.com/blog/2025/04/04/new-k8s-peer-discovery)
- [Dragonboat Raft Library](https://github.com/lni/dragonboat)
- [go-discover Kubernetes Provider](https://github.com/hashicorp/go-discover/blob/master/provider/k8s/k8s_discover.go)
- [Akka Kubernetes Bootstrap](https://doc.akka.io/docs/akka-management/current/bootstrap/kubernetes.html)
- [TiKV Architecture](https://tikv.org/docs/6.5/reference/architecture/overview/)
