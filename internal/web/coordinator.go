package web

import (
	"errors"
	"time"

	"github.com/wyw14/solo-038/internal/sensor"
)

type Coordinator struct {
	state *State
	path  *Path
}

func NewCoordinator(state *State, path *Path) *Coordinator {
	return &Coordinator{state: state, path: path}
}

func (coordinator *Coordinator) Prepare(owner string, jobGeneration, rollGeneration uint64) error {
	if err := coordinator.state.Fence(owner, jobGeneration, rollGeneration); err != nil {
		return err
	}
	if !coordinator.path.Ready() {
		return nil
	}
	return coordinator.state.Thread(owner)
}

func (coordinator *Coordinator) Permit(readings []sensor.Reading, tolerance float64, now time.Time) (TensionPermit, error) {
	permit, err := BuildTensionPermit(readings, tolerance, now)
	if err != nil {
		return TensionPermit{}, err
	}
	fence := coordinator.state.CurrentFence()
	permit.Owner = fence.Owner
	permit.JobGeneration = fence.JobGeneration
	permit.RollGeneration = fence.RollGeneration
	permit.FenceEpoch = fence.Epoch
	return permit, nil
}

func (coordinator *Coordinator) Advance(owner string, meters float64, permit TensionPermit, now time.Time) error {
	if !permit.ValidAt(now) {
		return &PermitError{Reason: permit.Reason}
	}
	if err := coordinator.state.Advance(owner, meters, permit.Fence()); err != nil {
		if errors.Is(err, ErrStaleFence) {
			return &PermitError{Reason: ErrStaleFence.Error()}
		}
		return err
	}
	return nil
}

type PermitError struct {
	Reason string
}

func (err *PermitError) Error() string {
	if err.Reason == "" {
		return "web permit is not valid"
	}
	return "web permit is not valid: " + err.Reason
}
