package web

import (
	"strings"
	"testing"
	"time"

	"github.com/wyw14/solo-038/internal/sensor"
)

func testReadings(now time.Time) []sensor.Reading {
	return []sensor.Reading{
		{ID: "r1", Kind: sensor.ReadingForce, Source: "load-cell-a", Origin: "load-cell-a", Value: 90, Unit: "N", ObservedAt: now},
		{ID: "r2", Kind: sensor.ReadingForce, Source: "load-cell-b", Origin: "load-cell-b", Value: 91, Unit: "N", ObservedAt: now},
	}
}

func newFencedCoordinator(t *testing.T) (*Coordinator, *State) {
	t.Helper()
	path, err := NewPath([]Segment{
		{Name: "unwind", Length: 4, Tension: 8, Available: true},
		{Name: "press", Length: 12, Tension: 8, Available: true},
		{Name: "dryer", Length: 18, Tension: 7, Available: true},
	})
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	state := NewState()
	coordinator := NewCoordinator(state, path)
	if err := coordinator.Prepare("job-1", 1, 1); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return coordinator, state
}

func TestPermitAllowsShortAdvanceOnSameRoll(t *testing.T) {
	coordinator, _ := newFencedCoordinator(t)
	now := time.Now()
	permit, err := coordinator.Permit(testReadings(now), 2, now)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	if !permit.Allowed {
		t.Fatalf("permit should be allowed: %s", permit.Reason)
	}
	// Repeated short advances within the permit window must not be blocked.
	for i := 0; i < 3; i++ {
		if err := coordinator.Advance("job-1", 1, permit, now.Add(500*time.Millisecond)); err != nil {
			t.Fatalf("advance %d on same roll should be permitted: %v", i, err)
		}
	}
}

func TestPermitExpires(t *testing.T) {
	coordinator, _ := newFencedCoordinator(t)
	now := time.Now()
	permit, err := coordinator.Permit(testReadings(now), 2, now)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	if err := coordinator.Advance("job-1", 1, permit, now.Add(3*time.Second)); err == nil {
		t.Fatal("expired permit must be rejected")
	}
}

func TestRollChangeInvalidatesPermit(t *testing.T) {
	coordinator, _ := newFencedCoordinator(t)
	now := time.Now()
	permit, err := coordinator.Permit(testReadings(now), 2, now)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	// Load a new roll on the same job: roll generation changes.
	if err := coordinator.Prepare("job-1", 1, 2); err != nil {
		t.Fatalf("re-prepare: %v", err)
	}
	err = coordinator.Advance("job-1", 1, permit, now.Add(500*time.Millisecond))
	if err == nil {
		t.Fatal("permit from the previous roll must not advance the new roll")
	}
	var permitErr *PermitError
	if !isPermitError(err, &permitErr) {
		t.Fatalf("expected PermitError, got %T: %v", err, err)
	}
}

func TestJobChangeInvalidatesPermit(t *testing.T) {
	coordinator, _ := newFencedCoordinator(t)
	now := time.Now()
	permit, err := coordinator.Permit(testReadings(now), 2, now)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	// Switch to another job (new owner and job generation), same roll number.
	if err := coordinator.Prepare("job-2", 1, 1); err != nil {
		t.Fatalf("re-prepare: %v", err)
	}
	if err := coordinator.Advance("job-2", 1, permit, now.Add(500*time.Millisecond)); err == nil {
		t.Fatal("permit from the previous job must not advance another job's web")
	}
}

func TestReThreadInvalidatesPermit(t *testing.T) {
	coordinator, _ := newFencedCoordinator(t)
	now := time.Now()
	permit, err := coordinator.Permit(testReadings(now), 2, now)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	// Re-thread the very same roll and job: owner and generations unchanged,
	// but the fence is re-established, opening a new thread epoch.
	if err := coordinator.Prepare("job-1", 1, 1); err != nil {
		t.Fatalf("re-thread: %v", err)
	}
	if err := coordinator.Advance("job-1", 1, permit, now.Add(500*time.Millisecond)); err == nil {
		t.Fatal("permit from before the re-thread must not advance the web")
	}
	// A fresh permit acquired after the re-thread must work.
	fresh, err := coordinator.Permit(testReadings(now), 2, now)
	if err != nil {
		t.Fatalf("fresh permit: %v", err)
	}
	if err := coordinator.Advance("job-1", 1, fresh, now.Add(500*time.Millisecond)); err != nil {
		t.Fatalf("fresh permit after re-thread should advance: %v", err)
	}
}

func TestPermitRequiresFence(t *testing.T) {
	path, err := NewPath([]Segment{
		{Name: "unwind", Length: 4, Tension: 8, Available: true},
		{Name: "press", Length: 12, Tension: 8, Available: true},
		{Name: "dryer", Length: 18, Tension: 7, Available: true},
	})
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	coordinator := NewCoordinator(NewState(), path)
	if _, err := coordinator.Permit(testReadings(time.Now()), 2, time.Now()); err == nil {
		t.Fatal("permit must not be issued before the web path is fenced")
	}
}

func TestRecoveryPreservesThreadEpoch(t *testing.T) {
	coordinator, state := newFencedCoordinator(t)
	now := time.Now()
	permit, err := coordinator.Permit(testReadings(now), 2, now)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	record := Capture(state)
	if record.ThreadEpoch == 0 {
		t.Fatal("captured recovery must carry the thread epoch")
	}
	restored := NewState()
	if err := Restore(restored, record); err != nil {
		t.Fatalf("restore: %v", err)
	}
	restoredCoordinator := NewCoordinator(restored, coordinator.path)
	// The permit was issued against the same fence, so it survives recovery...
	if err := restoredCoordinator.Advance("job-1", 1, permit, now.Add(500*time.Millisecond)); err != nil {
		t.Fatalf("permit matching the recovered fence should advance: %v", err)
	}
	// ...but re-fencing after recovery still invalidates it.
	if err := restoredCoordinator.Prepare("job-1", 1, 1); err != nil {
		t.Fatalf("re-thread after recovery: %v", err)
	}
	if err := restoredCoordinator.Advance("job-1", 1, permit, now.Add(500*time.Millisecond)); err == nil {
		t.Fatal("permit from before the recovered re-thread must not advance")
	}
}

func TestRestoreLegacyRecordGetsOwnEpoch(t *testing.T) {
	state := NewState()
	legacy := Recovery{Owner: "job-1", JobGeneration: 1, RollGeneration: 1, Threaded: true}
	if err := Restore(state, legacy); err != nil {
		t.Fatalf("restore legacy: %v", err)
	}
	if _, ok := state.FenceIdentity(); !ok {
		t.Fatal("legacy recovery must yield a complete fence identity")
	}
}

func isPermitError(err error, target **PermitError) bool {
	for err != nil {
		if permitErr, ok := err.(*PermitError); ok {
			*target = permitErr
			return true
		}
		type unwrapper interface{ Unwrap() error }
		unwrapped, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}

func TestPermitErrorMessage(t *testing.T) {
	coordinator, _ := newFencedCoordinator(t)
	now := time.Now()
	permit, err := coordinator.Permit(testReadings(now), 2, now)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	if err := coordinator.Prepare("job-1", 1, 2); err != nil {
		t.Fatalf("re-prepare: %v", err)
	}
	err = coordinator.Advance("job-1", 1, permit, now.Add(500*time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "current paper path") {
		t.Fatalf("stale-fence rejection should name the paper path, got: %v", err)
	}
}
