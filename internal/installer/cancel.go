package installer

import (
	"context"
	"sync"
	"time"

	"sdk_version_control/internal/logger"
)

// installExitWaitTimeout bounds how long a new InstallSdk waits for the
// cancelled previous install of the same SDK to fully exit before proceeding.
const installExitWaitTimeout = 5 * time.Second

// cancelEntry pairs a cancel func with a monotonic install ID so the deferred
// cleanup in InstallSdk only deletes the map entry when it still belongs to
// THIS install (not a newer concurrent install of the same SDK type).
//
// done is closed by the owning install's deferred cleanup when it has FULLY
// exited. A newer InstallSdk for the same SDK cancels the old entry and then
// waits (bounded) on done: without that wait the old download goroutine could
// still be writing the shared tmp file (or its deferred cleanup could delete
// it) after the new install has re-created the same file.
type cancelEntry struct {
	cancel context.CancelFunc
	id     uint64
	done   chan struct{}
}

// cancelTracker owns the per-SDK in-flight install cancellation state. It was
// previously spread across App (cancelMu / cancelFuncs / nextCancelID); it
// lives with the installer because InstallSdk/CancelInstall are its only
// users besides shutdown.
type cancelTracker struct {
	mu     sync.Mutex
	m      map[string]cancelEntry
	nextID uint64
}

func newCancelTracker() *cancelTracker {
	return &cancelTracker{m: make(map[string]cancelEntry)}
}

// register cancels any in-flight install for sdkType, then registers a fresh
// cancellable context for the new install. It returns the install context,
// the previous install's done channel (nil if there was none), the new
// install's monotonic id, and a cleanup func the caller must defer.
func (t *cancelTracker) register(parent context.Context, sdkType string) (installCtx context.Context, prevDone <-chan struct{}, myID uint64, cleanup func()) {
	installCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})

	t.mu.Lock()
	if old, ok := t.m[sdkType]; ok {
		old.cancel()
		prevDone = old.done
	}
	myID = t.nextID
	t.nextID++
	t.m[sdkType] = cancelEntry{cancel: cancel, id: myID, done: done}
	t.mu.Unlock()

	cleanup = func() {
		cancel()
		t.mu.Lock()
		if entry, ok := t.m[sdkType]; ok && entry.id == myID {
			delete(t.m, sdkType)
		}
		t.mu.Unlock()
		close(done)
	}
	return installCtx, prevDone, myID, cleanup
}

// cancel requests cancellation of the in-flight install for sdkType WITHOUT
// removing the entry: the install goroutine is still winding down, and a
// reinstall of the same SDK must still be able to find entry.done and wait
// for it to fully exit (otherwise the old download could keep writing the
// shared tmp file while the new one re-creates it). The install's deferred
// cleanup removes the entry (id-gated) and closes done.
func (t *cancelTracker) cancel(sdkType string) {
	t.mu.Lock()
	if entry, ok := t.m[sdkType]; ok {
		entry.cancel()
	}
	t.mu.Unlock()
}

// cancelAll requests cancellation of every in-flight install (app shutdown).
func (t *cancelTracker) cancelAll() {
	t.mu.Lock()
	for sdkType, entry := range t.m {
		logger.Info("Cancelling ongoing install: %s", sdkType)
		entry.cancel()
	}
	t.mu.Unlock()
}

// waitForInstallExit blocks until done is closed (the install fully exited)
// or timeout elapses, reporting whether the install exited in time. Pure
// logic extracted from InstallSdk's reinstall-race fix for testability.
func waitForInstallExit(done <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}
