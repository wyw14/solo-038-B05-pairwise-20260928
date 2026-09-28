package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wyw14/solo-038/internal/sensor"
	"github.com/wyw14/solo-038/internal/web"
)

func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(Config{
		DataDir:           t.TempDir(),
		FestoonCapacity:   50,
		RegisterGap:       time.Millisecond,
		RegisterTolerance: 2.0,
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	return runtime
}

func readingsFor(now time.Time) []sensor.Reading {
	return []sensor.Reading{
		{ID: "a", Kind: sensor.ReadingForce, Source: "load-cell-a", Origin: "load-cell-a", Value: 90, Unit: "N", ObservedAt: now},
		{ID: "b", Kind: sensor.ReadingForce, Source: "load-cell-b", Origin: "load-cell-b", Value: 91, Unit: "N", ObservedAt: now},
	}
}

func TestFeedWebShortAdvancesOnSameRoll(t *testing.T) {
	runtime := newTestRuntime(t)
	ctx := context.Background()
	if err := runtime.ExecuteOperation(ctx, "load_roll"); err != nil {
		t.Fatalf("load_roll: %v", err)
	}
	// Normal short advances on the same roll must keep succeeding.
	for i := 0; i < 2; i++ {
		if err := runtime.ExecuteOperation(ctx, "feed_web"); err != nil {
			t.Fatalf("feed_web %d: %v", i, err)
		}
	}
}

func TestStalePermitRejectedAfterRollChange(t *testing.T) {
	runtime := newTestRuntime(t)
	ctx := context.Background()
	if err := runtime.ExecuteOperation(ctx, "load_roll"); err != nil {
		t.Fatalf("load_roll: %v", err)
	}
	now := time.Now()
	permit, err := runtime.webControl.Permit(readingsFor(now), runtime.config.RegisterTolerance, now)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	current, _ := runtime.CurrentJob()
	if err := runtime.webControl.Advance(string(current.ID), 1, permit, now.Add(100*time.Millisecond)); err != nil {
		t.Fatalf("advance before roll change: %v", err)
	}
	// Change the roll: the web fence is re-established for the new roll.
	if err := runtime.ExecuteOperation(ctx, "load_roll"); err != nil {
		t.Fatalf("second load_roll: %v", err)
	}
	err = runtime.webControl.Advance(string(current.ID), 1, permit, now.Add(200*time.Millisecond))
	var permitErr *web.PermitError
	if !errors.As(err, &permitErr) {
		t.Fatalf("permit checked out on the old roll must be rejected after a roll change, got %v", err)
	}
	// Re-acquiring with measurements on the current path restores feeding.
	refreshed, err := runtime.webControl.Permit(readingsFor(now.Add(200*time.Millisecond)), runtime.config.RegisterTolerance, now.Add(200*time.Millisecond))
	if err != nil {
		t.Fatalf("refreshed permit: %v", err)
	}
	if err := runtime.webControl.Advance(string(current.ID), 1, refreshed, now.Add(300*time.Millisecond)); err != nil {
		t.Fatalf("advance with a permit for the new roll: %v", err)
	}
}

func TestStalePermitRejectedAfterJobSwitch(t *testing.T) {
	runtime := newTestRuntime(t)
	ctx := context.Background()
	if err := runtime.ExecuteOperation(ctx, "load_roll"); err != nil {
		t.Fatalf("load_roll: %v", err)
	}
	now := time.Now()
	first, _ := runtime.CurrentJob()
	permit, err := runtime.webControl.Permit(readingsFor(now), runtime.config.RegisterTolerance, now)
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	// Switch to another job and fence its paper path via a roll load.
	second := runtime.jobs.Create(now)
	second.RollGeneration = 1
	second.PlateGeneration = 1
	if err := runtime.jobs.Put(second); err != nil {
		t.Fatalf("put second job: %v", err)
	}
	if err := runtime.ExecuteOperation(ctx, "load_roll"); err != nil {
		t.Fatalf("load_roll for second job: %v", err)
	}
	if err := runtime.webControl.Advance(string(first.ID), 1, permit, now.Add(200*time.Millisecond)); err == nil {
		t.Fatal("permit acquired for the previous job must not move the new job's web")
	}
}
