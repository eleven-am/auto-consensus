package grpc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
)

var testPortCounter atomic.Int32

func init() {
	testPortCounter.Store(50000)
}

func getTestPort() int {
	return int(testPortCounter.Add(1))
}

func TestServer_StartStop(t *testing.T) {
	s := NewServer(nil)

	if err := s.Start(":0"); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	addr := s.Addr()
	if addr == "" {
		t.Error("expected non-empty address after start")
	}

	if err := s.Start(":0"); err == nil {
		t.Error("expected error on double start")
	}

	if err := s.Stop(); err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}

	if err := s.Stop(); err != nil {
		t.Fatalf("stop on already stopped server should not error: %v", err)
	}
}

func TestServer_RegisterService(t *testing.T) {
	s := NewServer(nil)

	registered := false
	s.RegisterService(func(server *grpc.Server) {
		registered = true
	})

	if err := s.Start(":0"); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer s.Stop()

	if !registered {
		t.Error("expected service registrar to be called")
	}
}

func TestServer_RegisterServiceAfterStart(t *testing.T) {
	s := NewServer(nil)

	if err := s.Start(":0"); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer s.Stop()

	called := false
	s.RegisterService(func(server *grpc.Server) {
		called = true
	})

	if called {
		t.Error("registrar should not be called after server started")
	}
}

func TestServer_ForwardCommand_AsLeader(t *testing.T) {
	s := NewServer(nil)

	applyCalled := false
	expectedResult := []byte("result-data")
	s.SetApplyFunc(func(cmd []byte, timeout time.Duration) ([]byte, error) {
		applyCalled = true
		if string(cmd) != "test-command" {
			t.Errorf("expected command 'test-command', got '%s'", string(cmd))
		}
		return expectedResult, nil
	})
	s.SetLeaderCheck(func() bool { return true })

	if err := s.Start(":0"); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer s.Stop()

	ctx := context.Background()
	forwarder := NewForwarder(nil)

	result, err := forwarder.Forward(ctx, s.Addr(), []byte("test-command"), 5*time.Second)
	if err != nil {
		t.Fatalf("forward failed: %v", err)
	}

	if !applyCalled {
		t.Error("expected apply function to be called")
	}

	if string(result) != string(expectedResult) {
		t.Errorf("expected result '%s', got '%s'", expectedResult, result)
	}
}

func TestServer_ForwardCommand_NotLeader_WithRedirect(t *testing.T) {
	leaderServer := NewServer(nil)
	leaderServer.SetLeaderCheck(func() bool { return true })
	leaderServer.SetApplyFunc(func(cmd []byte, timeout time.Duration) ([]byte, error) {
		return []byte("leader-result"), nil
	})

	if err := leaderServer.Start(":0"); err != nil {
		t.Fatalf("failed to start leader: %v", err)
	}
	defer leaderServer.Stop()

	followerServer := NewServer(nil)
	followerServer.SetLeaderCheck(func() bool { return false })
	followerServer.SetLeaderAddrFunc(func() string { return leaderServer.Addr() })

	if err := followerServer.Start(":0"); err != nil {
		t.Fatalf("failed to start follower: %v", err)
	}
	defer followerServer.Stop()

	ctx := context.Background()
	forwarder := NewForwarder(nil)

	result, err := forwarder.Forward(ctx, followerServer.Addr(), []byte("test"), 5*time.Second)
	if err != nil {
		t.Fatalf("forward failed: %v", err)
	}

	if string(result) != "leader-result" {
		t.Errorf("expected 'leader-result', got '%s'", result)
	}
}

func TestServer_ForwardCommand_ApplyError(t *testing.T) {
	s := NewServer(nil)
	s.SetLeaderCheck(func() bool { return true })
	s.SetApplyFunc(func(cmd []byte, timeout time.Duration) ([]byte, error) {
		return nil, errors.New("apply failed")
	})

	if err := s.Start(":0"); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer s.Stop()

	ctx := context.Background()
	forwarder := NewForwarder(nil)

	_, err := forwarder.Forward(ctx, s.Addr(), []byte("test"), 5*time.Second)
	if err == nil {
		t.Error("expected error from apply failure")
	}
}

func TestForwarder_NoLeader(t *testing.T) {
	forwarder := NewForwarder(nil)

	_, err := forwarder.Forward(context.Background(), "", []byte("test"), time.Second)
	if !errors.Is(err, ErrNoLeader) {
		t.Errorf("expected ErrNoLeader, got %v", err)
	}
}

func TestForwarder_MaxHopsExceeded(t *testing.T) {
	server1 := NewServer(nil)
	server1.SetLeaderCheck(func() bool { return false })

	server2 := NewServer(nil)
	server2.SetLeaderCheck(func() bool { return false })

	server3 := NewServer(nil)
	server3.SetLeaderCheck(func() bool { return false })

	if err := server1.Start(":0"); err != nil {
		t.Fatalf("failed to start server1: %v", err)
	}
	defer server1.Stop()

	if err := server2.Start(":0"); err != nil {
		t.Fatalf("failed to start server2: %v", err)
	}
	defer server2.Stop()

	if err := server3.Start(":0"); err != nil {
		t.Fatalf("failed to start server3: %v", err)
	}
	defer server3.Stop()

	server1.SetLeaderAddrFunc(func() string { return server2.Addr() })
	server2.SetLeaderAddrFunc(func() string { return server3.Addr() })
	server3.SetLeaderAddrFunc(func() string { return server1.Addr() })

	forwarder := NewForwarder(nil)
	_, err := forwarder.Forward(context.Background(), server1.Addr(), []byte("test"), 5*time.Second)

	if !errors.Is(err, ErrMaxHopsExceeded) {
		t.Errorf("expected ErrMaxHopsExceeded, got %v", err)
	}
}

func TestServer_ForwardCommand_NoApplyFunc(t *testing.T) {
	s := NewServer(nil)
	s.SetLeaderCheck(func() bool { return true })

	if err := s.Start(":0"); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer s.Stop()

	forwarder := NewForwarder(nil)
	_, err := forwarder.Forward(context.Background(), s.Addr(), []byte("test"), time.Second)

	if err == nil {
		t.Error("expected error when apply function not set")
	}
}

func TestForwarder_ConnectionError(t *testing.T) {
	forwarder := NewForwarder(nil)

	_, err := forwarder.Forward(context.Background(), "127.0.0.1:1", []byte("test"), time.Second)

	if err == nil {
		t.Error("expected connection error")
	}
}
