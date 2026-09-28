package web

import (
	"fmt"
	"sync"
	"time"
)

type State struct {
	mu             sync.RWMutex
	owner          string
	jobGeneration  uint64
	rollGeneration uint64
	threadEpoch    uint64
	threaded       bool
	meter          float64
}

func NewState() *State {
	return &State{}
}

// Fence establishes the current paper-path identity. Every call opens a new
// thread epoch, so a roll change, a job change, or a re-thread each
// invalidate tension permits issued against the previous path — even when the
// owner and generation numbers happen to be unchanged.
func (state *State) Fence(owner string, jobGeneration, rollGeneration uint64) error {
	if owner == "" || jobGeneration == 0 || rollGeneration == 0 {
		return fmt.Errorf("web fence identity is incomplete")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.owner = owner
	state.jobGeneration = jobGeneration
	state.rollGeneration = rollGeneration
	state.threadEpoch++
	state.threaded = false
	state.meter = 0
	return nil
}

func (state *State) Thread(owner string) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if owner != state.owner {
		return fmt.Errorf("web owner fence rejected")
	}
	state.threaded = true
	return nil
}

func (state *State) Advance(owner string, meters float64, permit TensionPermit, now time.Time) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if owner != state.owner || !state.threaded || meters < 0 {
		return fmt.Errorf("web advance is not permitted")
	}
	current := state.fenceIdentityLocked()
	if !permit.Fence.Matches(current) {
		return &PermitError{Reason: "permit was not acquired for the current paper path"}
	}
	if !permit.ValidAt(now) {
		return &PermitError{Reason: permit.Reason}
	}
	state.meter += meters
	return nil
}

// FenceIdentity returns the identity of the fenced paper path. The boolean is
// false until Fence has been called.
func (state *State) FenceIdentity() (FenceIdentity, bool) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	if state.owner == "" || state.jobGeneration == 0 || state.rollGeneration == 0 || state.threadEpoch == 0 {
		return FenceIdentity{}, false
	}
	return state.fenceIdentityLocked(), true
}

func (state *State) fenceIdentityLocked() FenceIdentity {
	return FenceIdentity{Owner: state.owner, JobGeneration: state.jobGeneration, RollGeneration: state.rollGeneration, ThreadEpoch: state.threadEpoch}
}

func (state *State) Snapshot() (string, uint64, uint64, uint64, bool, float64) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.owner, state.jobGeneration, state.rollGeneration, state.threadEpoch, state.threaded, state.meter
}
