package web

import (
	"errors"
	"fmt"
	"sync"
)

// ErrStaleFence is returned when a tension permit was issued under a
// different web path fence than the one currently threaded.
var ErrStaleFence = errors.New("web permit fence does not match the current web path")

// PathFence identifies one threading of the web path. The epoch advances on
// every fence (roll or job change) and every (re-)thread, so evidence
// gathered under a previous path cannot authorize motion on a later one.
type PathFence struct {
	Owner          string `json:"owner"`
	JobGeneration  uint64 `json:"job_generation"`
	RollGeneration uint64 `json:"roll_generation"`
	Epoch          uint64 `json:"epoch"`
}

type State struct {
	mu             sync.RWMutex
	owner          string
	jobGeneration  uint64
	rollGeneration uint64
	fenceEpoch     uint64
	threaded       bool
	meter          float64
}

func NewState() *State {
	return &State{}
}

func (state *State) Fence(owner string, jobGeneration, rollGeneration uint64) error {
	if owner == "" || jobGeneration == 0 || rollGeneration == 0 {
		return fmt.Errorf("web fence identity is incomplete")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.owner = owner
	state.jobGeneration = jobGeneration
	state.rollGeneration = rollGeneration
	state.fenceEpoch++
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
	state.fenceEpoch++
	state.threaded = true
	return nil
}

func (state *State) CurrentFence() PathFence {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.fenceLocked()
}

func (state *State) fenceLocked() PathFence {
	return PathFence{Owner: state.owner, JobGeneration: state.jobGeneration, RollGeneration: state.rollGeneration, Epoch: state.fenceEpoch}
}

func (state *State) Advance(owner string, meters float64, fence PathFence) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if owner != state.owner || !state.threaded || meters < 0 {
		return fmt.Errorf("web advance is not permitted")
	}
	if fence != state.fenceLocked() {
		return ErrStaleFence
	}
	state.meter += meters
	return nil
}

func (state *State) Snapshot() (string, uint64, uint64, bool, float64) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.owner, state.jobGeneration, state.rollGeneration, state.threaded, state.meter
}
