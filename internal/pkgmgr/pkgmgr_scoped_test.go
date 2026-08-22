package pkgmgr

import (
	"testing"
	"time"
)

// TestScopedCommandTimeoutIs180s pins the package-manager command bound:
// corepack prepare / npm install -g routinely exceed the old 60s limit on
// slow networks or registries, leaving half-installed package managers.
func TestScopedCommandTimeoutIs180s(t *testing.T) {
	if scopedCommandTimeout != 180*time.Second {
		t.Errorf("scopedCommandTimeout = %v; want %v", scopedCommandTimeout, 180*time.Second)
	}
}

// TestNewScopedCommandContextDeadline verifies runScopedCommand's context
// actually carries the timeout as a deadline (a context without a deadline
// would hang forever on a stuck package manager).
func TestNewScopedCommandContextDeadline(t *testing.T) {
	ctx, cancel := newScopedCommandContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("newScopedCommandContext returned a context without a deadline")
	}
	remaining := time.Until(deadline)
	// Allow scheduling slack, but the deadline must reflect the ~180s bound.
	if remaining < 150*time.Second || remaining > 181*time.Second {
		t.Errorf("context deadline is %v from now; want ~%v", remaining, scopedCommandTimeout)
	}
}
