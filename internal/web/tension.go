package web

import (
	"fmt"
	"time"

	"github.com/wyw14/solo-038/internal/sensor"
)

type TensionPermit struct {
	Allowed        bool      `json:"allowed"`
	Origins        []string  `json:"origins"`
	IssuedAt       time.Time `json:"issued_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	Reason         string    `json:"reason"`
	Owner          string    `json:"owner"`
	JobGeneration  uint64    `json:"job_generation"`
	RollGeneration uint64    `json:"roll_generation"`
	FenceEpoch     uint64    `json:"fence_epoch"`
}

func BuildTensionPermit(readings []sensor.Reading, tolerance float64, now time.Time) (TensionPermit, error) {
	votes, err := sensor.IndependentVotes(readings)
	if err != nil {
		return TensionPermit{}, err
	}
	agree, err := sensor.Agree(votes, tolerance)
	if err != nil {
		return TensionPermit{}, err
	}
	permit := TensionPermit{Allowed: agree && len(votes) >= 2, IssuedAt: now.UTC(), ExpiresAt: now.Add(2 * time.Second).UTC()}
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

// Fence returns the web path fence this permit was issued under. A permit
// only authorizes advance while the state still carries the same fence.
func (permit TensionPermit) Fence() PathFence {
	return PathFence{Owner: permit.Owner, JobGeneration: permit.JobGeneration, RollGeneration: permit.RollGeneration, Epoch: permit.FenceEpoch}
}
