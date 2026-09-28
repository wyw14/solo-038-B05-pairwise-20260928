package web

import (
	"fmt"
	"time"

	"github.com/wyw14/solo-038/internal/sensor"
)

// FenceIdentity identifies one paper path: a specific roll threaded for a
// specific job generation. ThreadEpoch changes on every Fence call, so a
// re-thread (or a roll/job change) always yields a different identity even
// when the owner and generation numbers stay the same.
type FenceIdentity struct {
	Owner          string `json:"owner"`
	JobGeneration  uint64 `json:"job_generation"`
	RollGeneration uint64 `json:"roll_generation"`
	ThreadEpoch    uint64 `json:"thread_epoch"`
}

func (identity FenceIdentity) Matches(other FenceIdentity) bool {
	return identity == other
}

type TensionPermit struct {
	Allowed   bool          `json:"allowed"`
	Origins   []string      `json:"origins"`
	Fence     FenceIdentity `json:"fence"`
	IssuedAt  time.Time     `json:"issued_at"`
	ExpiresAt time.Time     `json:"expires_at"`
	Reason    string        `json:"reason"`
}

// BuildTensionPermit issues a permit bound to the fence identity that was
// current when the readings were taken. The permit can only advance the web
// while that exact fence is still in force.
func BuildTensionPermit(readings []sensor.Reading, tolerance float64, fence FenceIdentity, now time.Time) (TensionPermit, error) {
	votes, err := sensor.IndependentVotes(readings)
	if err != nil {
		return TensionPermit{}, err
	}
	agree, err := sensor.Agree(votes, tolerance)
	if err != nil {
		return TensionPermit{}, err
	}
	permit := TensionPermit{Allowed: agree && len(votes) >= 2, Fence: fence, IssuedAt: now.UTC(), ExpiresAt: now.Add(2 * time.Second).UTC()}
	for _, vote := range votes {
		permit.Origins = append(permit.Origins, vote.Origin)
	}
	if !permit.Allowed {
		permit.Reason = fmt.Sprintf("need two agreeing independent origins, received %d", len(votes))
	}
	return permit, nil
}

func (permit TensionPermit) ValidAt(now time.Time) bool {
	return permit.Allowed && !now.Before(permit.IssuedAt) && now.Before(permit.ExpiresAt)
}
