package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// KeepCodexDaemonCurrent looks at the daemon when uni-switch writes a file
// (what Codex reads at start among them), every 15s only while the daemon
// is behind, and otherwise only now and then (#3185: a look every 15s woke
// an idle uni-switch).
func TestCodexDaemonLooksOnWrites(t *testing.T) {
	codexHome(t, "", `model = "gpt-5"`)
	was := daemonCheck
	var mu sync.Mutex
	looks := 0
	looked := func() int { mu.Lock(); defer mu.Unlock(); return looks }
	daemonCheck = func(string) provider.CodexDaemonCheck {
		mu.Lock()
		looks++
		mu.Unlock()
		return provider.CodexDaemonCheck{}
	}
	t.Cleanup(func() { daemonCheck = was })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { KeepCodexDaemonCurrent(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	// written again until it is seen: the loop may not be waiting yet, and
	// a look made only on its 15s beat is what this guards against
	for n := 1; n <= 2; n++ {
		for end := time.Now().Add(5 * time.Second); looked() < n; time.Sleep(10 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("uni-switch's writes had the daemon looked at %d times in 5s, want %d", looked(), n)
			}
			filememo.Forget()
		}
	}
	if d := codexDaemonNext(provider.CodexDaemonCheck{}); d != filememo.OthersEvery() {
		t.Errorf("with the daemon current the next look is in %v, want %v", d, filememo.OthersEvery())
	}
	if d := codexDaemonNext(provider.CodexDaemonCheck{Behind: true, Attached: 1}); d != codexDaemonEvery {
		t.Errorf("with the daemon behind the next look is in %v, want %v", d, codexDaemonEvery)
	}
	if d := codexDaemonNext(provider.CodexDaemonCheck{Behind: true, Restarted: true}); d != filememo.OthersEvery() {
		t.Errorf("with the daemon restarted the next look is in %v, want %v", d, filememo.OthersEvery())
	}
}
