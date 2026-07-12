package cswap

import (
	"sync"
	"testing"
	"time"
)

func TestApplyThresholdIsVisibleAcrossGoroutines(t *testing.T) {
	s := setupOneActiveAccountForCollectScheduled(t)
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(AutoSwitchEvent) {}, true, "", nil)

	done := make(chan struct{})
	go func() {
		e.ApplyThreshold(42.5)
		close(done)
	}()
	<-done

	if got := e.currentSettings().Threshold; got != 42.5 {
		t.Fatalf("currentSettings().Threshold = %v, want 42.5", got)
	}
}

func TestApplyThresholdUpdatesPollPolicyInputs(t *testing.T) {
	s := setupOneActiveAccountForCollectScheduled(t)
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(AutoSwitchEvent) {}, true, "", nil)

	e.ApplyThreshold(77)

	threshold, _ := s.pollPolicyInputs()
	if threshold != 77 {
		t.Fatalf("pollPolicyInputs threshold = %v, want 77", threshold)
	}
}

func TestWakeCutsSleepShort(t *testing.T) {
	s := setupOneActiveAccountForCollectScheduled(t)
	settings := DefaultAutoSwitchSettings()
	settings.IntervalSeconds = 3600 // would sleep an hour without Wake
	e := NewAutoSwitchEngine(s, settings, func(AutoSwitchEvent) {}, true, "", nil)

	started := make(chan struct{})
	stopped := make(chan int, 1)
	go func() {
		close(started)
		stopped <- e.RunLoop()
	}()
	<-started
	time.Sleep(20 * time.Millisecond) // let RunLoop reach its sleep select

	woke := make(chan struct{})
	go func() {
		e.Wake()
		close(woke)
	}()
	<-woke

	// A woken loop ticks again almost immediately; give it a moment, then
	// stop it and confirm RunLoop actually returned (didn't hang the full
	// hour), proving Wake reached the select.
	time.Sleep(20 * time.Millisecond)
	e.Stop()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLoop did not exit after Stop; Wake likely left it in a bad state")
	}
}

func TestWakeBeforeRunLoopStartsIsNoop(t *testing.T) {
	s := setupOneActiveAccountForCollectScheduled(t)
	e := NewAutoSwitchEngine(s, DefaultAutoSwitchSettings(), func(AutoSwitchEvent) {}, true, "", nil)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.Wake() // must not panic on a nil wakeCh
	}()
	wg.Wait()
}
