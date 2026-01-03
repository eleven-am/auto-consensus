package node

import "errors"

var (
	ErrMissingNodeID  = errors.New("node ID is required")
	ErrMissingFactory = errors.New("storage factory is required")
	ErrMissingFSM     = errors.New("FSM is required")
)
