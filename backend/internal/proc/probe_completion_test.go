package proc

import (
	"testing"
	"time"
)

// A cancellation is not complete until its process-tree kill completes.
// Hold that transition explicitly, independently of process startup speed.
func TestEndProbesWaitsForKillCompletion(t *testing.T) {
	begun := make(chan struct{})
	p := &probe{killing: make(chan struct{}), ended: make(chan struct{})}
	p.cancel = func() { p.kill(); close(begun) }
	probes.Lock()
	probes.m[p] = struct{}{}
	probes.Unlock()
	done := make(chan struct{})
	go func() { EndProbes(); close(done) }()
	t.Cleanup(func() {
		p.end()
		<-done
		probes.Lock()
		delete(probes.m, p)
		probes.Unlock()
	})
	select {
	case <-begun:
	case <-time.After(5 * time.Second):
		t.Fatal("the probe was not canceled")
	}
	select {
	case <-done:
		t.Fatal("EndProbes returned before the process-tree kill completed")
	case <-time.After(100 * time.Millisecond):
	}
	p.end()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("EndProbes did not return after the process-tree kill completed")
	}
}
