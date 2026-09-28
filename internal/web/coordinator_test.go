package web

import (
	"errors"
	"testing"
	"time"

	"github.com/wyw14/solo-038/internal/sensor"
)

var testNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func testCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	path, err := NewPath([]Segment{
		{Name: "unwind", Length: 4, Tension: 8, Available: true},
		{Name: "press", Length: 12, Tension: 8, Available: true},
		{Name: "dryer", Length: 18, Tension: 7, Available: true},
	})
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	return NewCoordinator(NewState(), path)
}

func testReadings(now time.Time) []sensor.Reading {
	return []sensor.Reading{
		{ID: "reading-a", Kind: sensor.ReadingForce, Source: "load-cell-a", Origin: "load-cell-a", Value: 90, Unit: "N", ObservedAt: now},
		{ID: "reading-b", Kind: sensor.ReadingForce, Source: "load-cell-b", Origin: "load-cell-b", Value: 91, Unit: "N", ObservedAt: now},
	}
}

func prepare(t *testing.T, coordinator *Coordinator, owner string, jobGeneration, rollGeneration uint64) {
	t.Helper()
	if err := coordinator.Prepare(owner, jobGeneration, rollGeneration); err != nil {
		t.Fatalf("prepare: %v", err)
	}
}

func permit(t *testing.T, coordinator *Coordinator, now time.Time) TensionPermit {
	t.Helper()
	permit, err := coordinator.Permit(testReadings(now), 2, now)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	if !permit.Allowed {
		t.Fatalf("permit was not allowed: %s", permit.Reason)
	}
	return permit
}

func requireStaleFence(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected stale fence rejection, got nil")
	}
	var permitErr *PermitError
	if !errors.As(err, &permitErr) {
		t.Fatalf("expected PermitError, got %T: %v", err, err)
	}
}

func TestPermitAllowsNormalAdvanceOnSameRoll(t *testing.T) {
	coordinator := testCoordinator(t)
	prepare(t, coordinator, "job-1", 1, 1)
	first := permit(t, coordinator, testNow)
	if err := coordinator.Advance("job-1", 1, first, testNow.Add(time.Second)); err != nil {
		t.Fatalf("advance within permit ttl: %v", err)
	}
	second := permit(t, coordinator, testNow.Add(time.Second))
	if err := coordinator.Advance("job-1", 1, second, testNow.Add(1500*time.Millisecond)); err != nil {
		t.Fatalf("follow-up advance on same roll: %v", err)
	}
	_, _, _, _, meter := coordinator.state.Snapshot()
	if meter != 2 {
		t.Fatalf("expected meter 2, got %v", meter)
	}
}

func TestPermitRejectedAfterRollChange(t *testing.T) {
	coordinator := testCoordinator(t)
	prepare(t, coordinator, "job-1", 1, 1)
	stale := permit(t, coordinator, testNow)
	prepare(t, coordinator, "job-1", 1, 2)
	requireStaleFence(t, coordinator.Advance("job-1", 1, stale, testNow.Add(time.Second)))
	fresh := permit(t, coordinator, testNow.Add(time.Second))
	if err := coordinator.Advance("job-1", 1, fresh, testNow.Add(1500*time.Millisecond)); err != nil {
		t.Fatalf("fresh permit after roll change: %v", err)
	}
}

func TestPermitRejectedAfterJobSwitch(t *testing.T) {
	coordinator := testCoordinator(t)
	prepare(t, coordinator, "job-1", 1, 1)
	stale := permit(t, coordinator, testNow)
	prepare(t, coordinator, "job-2", 1, 1)
	requireStaleFence(t, coordinator.Advance("job-2", 1, stale, testNow.Add(time.Second)))
}

func TestPermitRejectedAfterRethread(t *testing.T) {
	coordinator := testCoordinator(t)
	prepare(t, coordinator, "job-1", 1, 1)
	stale := permit(t, coordinator, testNow)
	if err := coordinator.state.Thread("job-1"); err != nil {
		t.Fatalf("rethread: %v", err)
	}
	requireStaleFence(t, coordinator.Advance("job-1", 1, stale, testNow.Add(time.Second)))
}

func TestPermitRejectedAfterRestore(t *testing.T) {
	coordinator := testCoordinator(t)
	prepare(t, coordinator, "job-1", 1, 1)
	stale := permit(t, coordinator, testNow)
	if err := Restore(coordinator.state, Capture(coordinator.state)); err != nil {
		t.Fatalf("restore: %v", err)
	}
	requireStaleFence(t, coordinator.Advance("job-1", 1, stale, testNow.Add(time.Second)))
}

func TestPermitExpiryStillEnforced(t *testing.T) {
	coordinator := testCoordinator(t)
	prepare(t, coordinator, "job-1", 1, 1)
	expired := permit(t, coordinator, testNow)
	if err := coordinator.Advance("job-1", 1, expired, testNow.Add(3*time.Second)); err == nil {
		t.Fatalf("expected expired permit to be rejected")
	}
}

func TestPermitIsStampedWithCurrentFence(t *testing.T) {
	coordinator := testCoordinator(t)
	prepare(t, coordinator, "job-1", 3, 7)
	issued := permit(t, coordinator, testNow)
	if issued.Owner != "job-1" || issued.JobGeneration != 3 || issued.RollGeneration != 7 {
		t.Fatalf("permit fence identity = %+v", issued)
	}
	if issued.FenceEpoch != coordinator.state.CurrentFence().Epoch {
		t.Fatalf("permit epoch %d does not match state epoch %d", issued.FenceEpoch, coordinator.state.CurrentFence().Epoch)
	}
}
