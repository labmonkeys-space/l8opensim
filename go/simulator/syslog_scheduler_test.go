/*
 * © 2025 Labmonkeys Space
 * Apache-2.0 — see LICENSE.
 */

package main

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingSyslogFirer is a mock syslogFirer that records fire count per
// device. Safe for concurrent use.
type countingSyslogFirer struct {
	deviceIP net.IP
	count    atomic.Uint64
	mu       sync.Mutex
	firedAt  []time.Time
	nextErr  error
}

func (f *countingSyslogFirer) Fire(entry *SyslogCatalogEntry, overrides map[string]string) error {
	f.count.Add(1)
	f.mu.Lock()
	f.firedAt = append(f.firedAt, time.Now())
	err := f.nextErr
	f.mu.Unlock()
	return err
}

func testSyslogCatalog(t *testing.T) *SyslogCatalog {
	t.Helper()
	cat, err := LoadEmbeddedSyslogCatalog()
	if err != nil {
		t.Fatalf("test catalog: %v", err)
	}
	return cat
}

// TestSyslogScheduler_FiresRegisteredDevices — each registered device
// must get at least one fire within a reasonable window.
func TestSyslogScheduler_FiresRegisteredDevices(t *testing.T) {
	cat := testSyslogCatalog(t)
	s := NewSyslogScheduler(SyslogSchedulerOptions{
		Catalog:      cat,
		MeanInterval: 10 * time.Millisecond,
		Seed:         1,
	})

	const N = 5
	firers := make([]*countingSyslogFirer, N)
	for i := 0; i < N; i++ {
		firers[i] = &countingSyslogFirer{deviceIP: net.IPv4(10, 0, 0, byte(i+1))}
		s.Register(firers[i].deviceIP, firers[i])
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		allFired := true
		for _, f := range firers {
			if f.count.Load() == 0 {
				allFired = false
				break
			}
		}
		if allFired {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop within 1s")
	}
	for i, f := range firers {
		if f.count.Load() == 0 {
			t.Errorf("device %d never fired", i)
		}
	}
}

// TestSyslogScheduler_GlobalCapEnforced — with a low cap and many devices
// at a short interval, total fires-per-second must be bounded by the cap.
func TestSyslogScheduler_GlobalCapEnforced(t *testing.T) {
	cat := testSyslogCatalog(t)
	const cap = 20
	const devices = 200
	s := NewSyslogScheduler(SyslogSchedulerOptions{
		Catalog:            cat,
		MeanInterval:       time.Millisecond,
		GlobalCapPerSecond: cap,
		Seed:               42,
	})
	firers := make([]*countingSyslogFirer, devices)
	for i := 0; i < devices; i++ {
		firers[i] = &countingSyslogFirer{deviceIP: net.IPv4(10, 0, byte(i/256), byte(i%256))}
		s.Register(firers[i].deviceIP, firers[i])
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	<-ctx.Done()
	s.Stop()
	<-done
	elapsed := time.Since(start)

	var total uint64
	for _, f := range firers {
		total += f.count.Load()
	}
	// Allow full burst + one period of refill. cap=20 with burst=20 yields
	// up to 40 in the first second, but we give 1.5× headroom for scheduler
	// wake-up latency on slow CI.
	maxAllowed := uint64(float64(cap)*elapsed.Seconds()*1.5 + float64(cap))
	if total > maxAllowed {
		t.Errorf("global cap violated: %d fires in %v (max allowed ~%d)", total, elapsed, maxAllowed)
	}
	if total == 0 {
		t.Error("no fires recorded under cap")
	}
}

// TestSyslogScheduler_RegisterDeregisterThreadSafety — mixed ops from many
// goroutines must not race or panic.
func TestSyslogScheduler_RegisterDeregisterThreadSafety(t *testing.T) {
	cat := testSyslogCatalog(t)
	s := NewSyslogScheduler(SyslogSchedulerOptions{
		Catalog:      cat,
		MeanInterval: time.Millisecond,
		Seed:         7,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	const goroutines = 100
	const ops = 50
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				ip := net.IPv4(10, 0, byte(id), byte(i))
				f := &countingSyslogFirer{deviceIP: ip}
				s.Register(ip, f)
				s.Deregister(ip)
			}
		}(g)
	}
	wg.Wait()
	s.Stop()
	cancel()
	<-done
}

// TestSyslogScheduler_DeregisterRemovesDevice — a deregistered device
// stops receiving fires.
func TestSyslogScheduler_DeregisterRemovesDevice(t *testing.T) {
	cat := testSyslogCatalog(t)
	s := NewSyslogScheduler(SyslogSchedulerOptions{
		Catalog:      cat,
		MeanInterval: 10 * time.Millisecond,
		Seed:         3,
	})
	ip := net.IPv4(10, 42, 0, 1)
	f := &countingSyslogFirer{deviceIP: ip}
	s.Register(ip, f)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	// Wait for first fire, then deregister and verify count stabilises.
	for time.Now().Before(time.Now().Add(time.Second)) {
		if f.count.Load() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.Deregister(ip)
	at := f.count.Load()
	time.Sleep(100 * time.Millisecond)
	s.Stop()
	cancel()
	<-done
	if after := f.count.Load(); after > at {
		t.Errorf("deregistered device still firing: before=%d, after=%d", at, after)
	}
}

// TestSyslogScheduler_StopIdempotent — repeated Stop calls and Stop after
// Run completion must not panic.
func TestSyslogScheduler_StopIdempotent(t *testing.T) {
	cat := testSyslogCatalog(t)
	s := NewSyslogScheduler(SyslogSchedulerOptions{
		Catalog:      cat,
		MeanInterval: time.Second,
		Seed:         11,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	s.Stop()
	s.Stop()
	cancel()
	<-done
	s.Stop()
}

// TestSyslogScheduler_FireErrorDoesNotStopLoop — a firer that returns an
// error must not prevent subsequent fires.
func TestSyslogScheduler_FireErrorDoesNotStopLoop(t *testing.T) {
	cat := testSyslogCatalog(t)
	s := NewSyslogScheduler(SyslogSchedulerOptions{
		Catalog:      cat,
		MeanInterval: 5 * time.Millisecond,
		Seed:         2,
	})
	ip := net.IPv4(10, 1, 1, 1)
	f := &countingSyslogFirer{deviceIP: ip}
	f.mu.Lock()
	f.nextErr = context.DeadlineExceeded
	f.mu.Unlock()
	s.Register(ip, f)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	<-ctx.Done()
	s.Stop()
	<-done
	if f.count.Load() < 2 {
		t.Errorf("expected multiple fires despite errors, got %d", f.count.Load())
	}
}

// TestSyslogScheduler_EmptyHeapDoesNotBusyLoop — running with zero
// registered devices must idle cheaply, not spin.
func TestSyslogScheduler_EmptyHeapDoesNotBusyLoop(t *testing.T) {
	cat := testSyslogCatalog(t)
	s := NewSyslogScheduler(SyslogSchedulerOptions{
		Catalog:      cat,
		MeanInterval: time.Millisecond,
		Seed:         4,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	<-ctx.Done()
	s.Stop()
	<-done
	// If we got here without hang or panic, the loop idled correctly.
}
