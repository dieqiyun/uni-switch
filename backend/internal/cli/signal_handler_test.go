package cli

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Hooks belong to the handler that starts with them. A completed handler
// must be joined before another command or test changes its configuration.
func TestSignalHandlerKeepsStartedHooks(t *testing.T) {
	oldSave, oldWait, oldDie := sessionsSaved, saveOnSignal, die
	t.Cleanup(func() {
		stopSignals()
		sessionsSaved, saveOnSignal, die = oldSave, oldWait, oldDie
	})
	var first, second atomic.Int32
	died := make(chan os.Signal, 1)
	sessionsSaved = func() { first.Add(1) }
	saveOnSignal = time.Second
	die = func(s os.Signal) { died <- s }
	endProbesOnSignal()
	if onSignal == nil {
		t.Skip("all supported signals are ignored")
	}
	sessionsSaved = func() { second.Add(1) }
	onSignal <- syscall.SIGTERM
	select {
	case <-died:
	case <-time.After(5 * time.Second):
		t.Fatal("the signal handler did not finish")
	}
	stopSignals()
	if first.Load() != 1 || second.Load() != 0 {
		t.Fatalf("handler used changed hooks: started=%d changed=%d", first.Load(), second.Load())
	}
	if onSignal != nil || signalDone != nil {
		t.Fatal("the signal handler was left registered after handoff")
	}
}
