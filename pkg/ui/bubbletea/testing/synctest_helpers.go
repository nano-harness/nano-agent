package bubtesting

import (
	stdtesting "testing"
	"testing/synctest"
	"time"
)

// RunSyncTest runs fn inside a synctest bubble with a virtual clock.
func RunSyncTest(t *stdtesting.T, fn func(t *stdtesting.T)) {
	t.Helper()
	synctest.Test(t, fn)
}

// AdvanceTime advances virtual time inside a synctest bubble.
func AdvanceTime(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}
