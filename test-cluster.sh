#!/bin/bash
set -e

cleanup() {
    echo "Cleaning up..."
    pkill -f "testnode" 2>/dev/null || true
    rm -rf /tmp/raft-node*
}
trap cleanup EXIT

go build -o /tmp/testnode ./cmd/testnode/

echo "=== Starting node1 (will bootstrap) ==="
NODE_ID=node1 \
GOSSIP_ADDR=127.0.0.1:7946 \
RAFT_ADDR=127.0.0.1:8300 \
HTTP_ADDR=:8081 \
SECRET_KEY=test-cluster \
DATA_DIR=/tmp/raft-node1 \
PROBE_PORT=7945 \
/tmp/testnode &

sleep 5

echo "=== Starting node2 ==="
NODE_ID=node2 \
GOSSIP_ADDR=127.0.0.1:7947 \
RAFT_ADDR=127.0.0.1:8301 \
HTTP_ADDR=:8082 \
SECRET_KEY=test-cluster \
DATA_DIR=/tmp/raft-node2 \
PROBE_PORT=7955 \
STATIC_PEERS=127.0.0.1:7946 \
/tmp/testnode &

sleep 2

echo "=== Starting node3 ==="
NODE_ID=node3 \
GOSSIP_ADDR=127.0.0.1:7948 \
RAFT_ADDR=127.0.0.1:8302 \
HTTP_ADDR=:8083 \
SECRET_KEY=test-cluster \
DATA_DIR=/tmp/raft-node3 \
PROBE_PORT=7965 \
STATIC_PEERS=127.0.0.1:7946 \
/tmp/testnode &

sleep 5

echo ""
echo "=== Cluster Status ==="
echo "Node1: $(curl -s http://127.0.0.1:8081/status)"
echo "Node2: $(curl -s http://127.0.0.1:8082/status)"
echo "Node3: $(curl -s http://127.0.0.1:8083/status)"

echo ""
echo "Press Ctrl+C to stop..."
wait
