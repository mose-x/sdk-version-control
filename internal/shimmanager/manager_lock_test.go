package shimmanager

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestWithShimsConfigLock_serializes pins item 3's cross-platform contract:
// while one caller holds the lock, another caller's fn must not start; it
// runs only after the holder finishes. This is the serialization that
// prevents shims.json lost updates across processes.
func TestWithShimsConfigLock_serializes(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	var order []string
	appendOrder := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	orderLen := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(order)
	}

	release := make(chan struct{})
	aDone := make(chan struct{})
	go func() {
		defer close(aDone)
		err := withShimsConfigLock(dir, func() error {
			appendOrder("A-start")
			<-release // hold the lock until the test says otherwise
			appendOrder("A-end")
			return nil
		})
		if err != nil {
			t.Errorf("withShimsConfigLock(A) failed: %v", err)
		}
	}()

	// Wait for A to enter the critical section.
	deadline := time.Now().Add(5 * time.Second)
	for orderLen() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if orderLen() != 1 {
		t.Fatalf("A did not enter the critical section in time (order len=%d)", orderLen())
	}

	// B tries to take the lock; it must block while A holds it.
	bDone := make(chan struct{})
	go func() {
		defer close(bDone)
		err := withShimsConfigLock(dir, func() error {
			appendOrder("B")
			return nil
		})
		if err != nil {
			t.Errorf("withShimsConfigLock(B) failed: %v", err)
		}
	}()

	// Give B a moment; it must still be blocked.
	time.Sleep(150 * time.Millisecond)
	if orderLen() != 1 {
		t.Fatalf("B entered the critical section while A held the lock")
	}

	close(release)
	<-aDone
	<-bDone

	mu.Lock()
	defer mu.Unlock()
	want := []string{"A-start", "A-end", "B"}
	if len(order) != len(want) {
		t.Fatalf("order = %v; want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v; want %v", order, want)
		}
	}
}

// TestWithShimsConfigLock_noLostUpdates simulates the GUI+CLI race at thread
// level: N concurrent read-modify-write cycles on shims.json, each adding a
// unique command. With the lock, all N commands must survive; without one,
// the last writer wins and entries vanish. (The lock is cross-process, but
// each lockShimsConfig call opens a fresh handle/fd, so the same mechanism
// is exercised in-process.)
func TestWithShimsConfigLock_noLostUpdates(t *testing.T) {
	m := newTestManager(t)
	dir := m.cfg.SvcDir()
	const n = 8

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := withShimsConfigLock(dir, func() error {
				cfg, err := m.loadShimConfig()
				if err != nil {
					return err
				}
				if cfg.Commands == nil {
					cfg.Commands = make(map[string]string)
				}
				cfg.Commands[fmt.Sprintf("cmd%d", i)] = fmt.Sprintf("sdk%d", i)
				// Widen the race window so an unlocked implementation
				// would reliably lose updates.
				time.Sleep(5 * time.Millisecond)
				return m.saveShimConfig(m.cfg.ShimsConfigPath(), cfg)
			})
			if err != nil {
				t.Errorf("locked update %d failed: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	cfg, err := m.loadShimConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Commands) != n {
		t.Fatalf("shims.json has %d commands after %d concurrent updates; want %d (lost updates!)", len(cfg.Commands), n, n)
	}
	for i := 0; i < n; i++ {
		if got := cfg.Commands[fmt.Sprintf("cmd%d", i)]; got != fmt.Sprintf("sdk%d", i) {
			t.Errorf("Commands[cmd%d] = %q; want sdk%d", i, got, i)
		}
	}
}

// TestWithShimsConfigLock_propagatesError: fn's error is returned and the
// lock is still released afterwards.
func TestWithShimsConfigLock_propagatesError(t *testing.T) {
	dir := t.TempDir()
	sentinel := fmt.Errorf("boom")
	if err := withShimsConfigLock(dir, func() error { return sentinel }); err != sentinel {
		t.Fatalf("withShimsConfigLock error = %v; want %v", err, sentinel)
	}
	// Lock must be free again: a second acquisition succeeds immediately.
	done := make(chan error, 1)
	go func() {
		done <- withShimsConfigLock(dir, func() error { return nil })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second acquisition failed (lock not released?): %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock was not released after fn returned")
	}
}
