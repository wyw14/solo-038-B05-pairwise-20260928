package web

import (
	"fmt"
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

// Permit acquires a tension permit from fresh readings. The permit is bound
// to the fence currently in force, so it cannot be replayed after a roll
// change, a job change, or a re-thread.
func (coordinator *Coordinator) Permit(readings []sensor.Reading, tolerance float64, now time.Time) (TensionPermit, error) {
	fence, ok := coordinator.state.FenceIdentity()
	if !ok {
		return TensionPermit{}, fmt.Errorf("cannot acquire tension permit before the web path is fenced")
	}
	return BuildTensionPermit(readings, tolerance, fence, now)
}

func (coordinator *Coordinator) Advance(owner string, meters float64, permit TensionPermit, now time.Time) error {
	return coordinator.state.Advance(owner, meters, permit, now)
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
