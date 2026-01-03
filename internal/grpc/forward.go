package grpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	pb "github.com/eleven-am/auto-consensus/internal/grpc/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const maxRedirectHops = 2

var (
	ErrNoLeader         = errors.New("no leader available")
	ErrForwardFailed    = errors.New("forward failed")
	ErrMaxHopsExceeded  = errors.New("max redirect hops exceeded")
)

type Forwarder struct {
	logger *slog.Logger
}

func NewForwarder(logger *slog.Logger) *Forwarder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Forwarder{
		logger: logger.With("component", "grpc.forwarder"),
	}
}

func (f *Forwarder) Forward(ctx context.Context, leaderAddr string, cmd []byte, timeout time.Duration) ([]byte, error) {
	return f.forwardWithHops(ctx, leaderAddr, cmd, timeout, 0)
}

func (f *Forwarder) forwardWithHops(ctx context.Context, leaderAddr string, cmd []byte, timeout time.Duration, hops int) ([]byte, error) {
	if hops >= maxRedirectHops {
		return nil, ErrMaxHopsExceeded
	}

	if leaderAddr == "" {
		return nil, ErrNoLeader
	}

	conn, err := grpc.NewClient(leaderAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial leader %s: %w", leaderAddr, err)
	}
	defer conn.Close()

	client := pb.NewConsensusClient(conn)

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := client.ForwardCommand(callCtx, &pb.ForwardRequest{
		Command:   cmd,
		TimeoutMs: timeout.Milliseconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("forward command: %w", err)
	}

	if resp.LeaderAddr != "" && !resp.Success {
		f.logger.Debug("leader redirected", "new_leader", resp.LeaderAddr, "hop", hops+1)
		return f.forwardWithHops(ctx, resp.LeaderAddr, cmd, timeout, hops+1)
	}

	if !resp.Success {
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		}
		return nil, ErrForwardFailed
	}

	return resp.Result, nil
}
