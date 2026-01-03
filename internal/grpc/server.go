package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	pb "github.com/eleven-am/auto-consensus/internal/grpc/proto"
	"google.golang.org/grpc"
)

type Server struct {
	pb.UnimplementedConsensusServer

	mu       sync.Mutex
	server   *grpc.Server
	listener net.Listener
	addr     string
	started  bool

	applyFn    func([]byte, time.Duration) ([]byte, error)
	isLeaderFn func() bool
	leaderFn   func() string

	registrars []func(*grpc.Server)
	logger     *slog.Logger
}

func NewServer(logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		logger: logger.With("component", "grpc.server"),
	}
}

func (s *Server) Start(bindAddr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("server already started")
	}

	if bindAddr == "" {
		bindAddr = ":0"
	}

	listener, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.listener = listener
	s.addr = listener.Addr().String()
	s.server = grpc.NewServer()

	pb.RegisterConsensusServer(s.server, s)

	for _, registrar := range s.registrars {
		registrar(s.server)
	}

	s.started = true

	go func() {
		if err := s.server.Serve(listener); err != nil {
			s.logger.Error("grpc server error", "error", err)
		}
	}()

	s.logger.Info("grpc server started", "addr", s.addr)
	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.server.GracefulStop()
	s.started = false
	s.logger.Info("grpc server stopped")
	return nil
}

func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

func (s *Server) RegisterService(fn func(*grpc.Server)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		s.logger.Warn("RegisterService called after server started, ignoring")
		return
	}

	s.registrars = append(s.registrars, fn)
}

func (s *Server) SetApplyFunc(fn func([]byte, time.Duration) ([]byte, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyFn = fn
}

func (s *Server) SetLeaderCheck(fn func() bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isLeaderFn = fn
}

func (s *Server) SetLeaderAddrFunc(fn func() string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leaderFn = fn
}

func (s *Server) ForwardCommand(ctx context.Context, req *pb.ForwardRequest) (*pb.ForwardResponse, error) {
	if s.isLeaderFn == nil || !s.isLeaderFn() {
		leaderAddr := ""
		if s.leaderFn != nil {
			leaderAddr = s.leaderFn()
		}
		return &pb.ForwardResponse{
			Success:    false,
			LeaderAddr: leaderAddr,
		}, nil
	}

	if s.applyFn == nil {
		return &pb.ForwardResponse{
			Success: false,
			Error:   "apply function not set",
		}, nil
	}

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	result, err := s.applyFn(req.Command, timeout)
	if err != nil {
		return &pb.ForwardResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.ForwardResponse{
		Success: true,
		Result:  result,
	}, nil
}
