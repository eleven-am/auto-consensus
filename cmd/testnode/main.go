package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	autoconsensus "github.com/eleven-am/auto-consensus"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	nodeID := getEnv("NODE_ID", "node1")
	gossipPort := getEnvInt("GOSSIP_PORT", 7946)
	raftPort := getEnvInt("RAFT_PORT", 8300)
	httpAddr := getEnv("HTTP_ADDR", ":8080")
	dataDir := getEnv("DATA_DIR", fmt.Sprintf("/tmp/raft-%s", nodeID))
	secretKey := getEnv("SECRET_KEY", "test-cluster-key")
	advertiseAddr := getEnv("ADVERTISE_ADDR", "")

	logger.Info("starting test node",
		"node_id", nodeID,
		"gossip_port", gossipPort,
		"raft_port", raftPort,
		"http", httpAddr,
		"advertise", advertiseAddr,
	)

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Error("failed to create data dir", "error", err)
		os.Exit(1)
	}

	var disc autoconsensus.Discoverer

	dnsService := getEnv("DNS_SERVICE", "")
	dnsDomain := getEnv("DNS_DOMAIN", "")
	staticPeers := getEnv("STATIC_PEERS", "")

	switch {
	case dnsService != "" && dnsDomain != "":
		disc = autoconsensus.NewDNSDiscovery(autoconsensus.DNSConfig{
			Service: dnsService,
			Domain:  dnsDomain,
		})
		logger.Info("using DNS SRV discovery", "service", dnsService, "domain", dnsDomain)

	case staticPeers != "":
		peers := strings.Split(staticPeers, ",")
		for i := range peers {
			peers[i] = strings.TrimSpace(peers[i])
		}
		disc = autoconsensus.NewStaticDiscovery(peers)
		logger.Info("using static discovery", "peers", peers)

	default:
		disc = autoconsensus.NewMDNSDiscovery(autoconsensus.MDNSConfig{
			SecretKey: []byte(secretKey),
			Timeout:   2 * time.Second,
		})
		logger.Info("using mDNS discovery")
	}

	node, err := autoconsensus.New(autoconsensus.Config{
		NodeID:         nodeID,
		GossipPort:     gossipPort,
		RaftPort:       raftPort,
		SecretKey:      []byte(secretKey),
		StorageFactory: NewBoltStorageFactory(dataDir),
		FSM:            &noopFSM{},
		Discoverer:     disc,
		AdvertiseAddr:  advertiseAddr,
		Logger:         logger,
	})
	if err != nil {
		logger.Error("failed to create node", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := node.Start(ctx); err != nil {
		logger.Error("failed to start node", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		state := node.State()
		if state == autoconsensus.StateRunning {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(state.String()))
		}
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		leaderID, leaderAddr := node.Leader()
		status := map[string]interface{}{
			"node_id":         nodeID,
			"bootstrap_state": node.State().String(),
			"raft_state":      node.RaftState(),
			"raft_leader_id":  leaderID,
			"raft_leader":     leaderAddr,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	server := &http.Server{Addr: httpAddr, Handler: mux}
	go func() {
		logger.Info("http server starting", "addr", httpAddr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutting down")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	server.Shutdown(shutdownCtx)

	node.Stop()

	logger.Info("shutdown complete")
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		fmt.Sscanf(v, "%d", &i)
		return i
	}
	return defaultVal
}
