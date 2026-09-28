package web

import "fmt"

type Recovery struct {
	Owner          string  `json:"owner"`
	JobGeneration  uint64  `json:"job_generation"`
	RollGeneration uint64  `json:"roll_generation"`
	ThreadEpoch    uint64  `json:"thread_epoch"`
	Threaded       bool    `json:"threaded"`
	Meter          float64 `json:"meter"`
}

func Restore(state *State, recovery Recovery) error {
	if recovery.Owner == "" || recovery.JobGeneration == 0 || recovery.RollGeneration == 0 {
		return fmt.Errorf("web recovery fence is incomplete")
	}
	threadEpoch := recovery.ThreadEpoch
	if threadEpoch == 0 {
		// Snapshots written before thread epochs existed: treat the recovered
		// path as its own epoch so no stale in-memory fence can match it.
		threadEpoch = 1
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.owner = recovery.Owner
	state.jobGeneration = recovery.JobGeneration
	state.rollGeneration = recovery.RollGeneration
	state.threadEpoch = threadEpoch
	state.threaded = recovery.Threaded
	state.meter = recovery.Meter
	return nil
}

func Capture(state *State) Recovery {
	owner, jobGeneration, rollGeneration, threadEpoch, threaded, meter := state.Snapshot()
	return Recovery{Owner: owner, JobGeneration: jobGeneration, RollGeneration: rollGeneration, ThreadEpoch: threadEpoch, Threaded: threaded, Meter: meter}
}
