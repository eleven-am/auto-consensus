package autoconsensus

import "errors"

var (
	ErrMissingNodeID   = errors.New("node ID is required")
	ErrMissingBindAddr = errors.New("bind address is required")
	ErrMissingFactory  = errors.New("storage factory is required")
	ErrMissingFSM      = errors.New("FSM is required")
	ErrNotRunning      = errors.New("node not running")
	ErrAlreadyRunning  = errors.New("node already running")
)
