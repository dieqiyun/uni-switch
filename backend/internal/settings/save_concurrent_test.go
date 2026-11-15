package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/steady"
)

func TestSaveConcurrent(t *testing.T) {
	testSaveConcurrent(t, false)
}

func TestSaveConcurrentHardLink(t *testing.T) {
	testSaveConcurrent(t, true)
}

func testSaveConcurrent(t *testing.T, linked bool) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const token = "SYNTHETIC_PRIVATE_TOKEN"
	if err := Save(Settings{Theme: "dark", Lang: "zh", Proxy: "direct", GitHubToken: token}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(Dir(), "linked-settings.json")
	if linked {
		if err := os.Link(Path(), other); err != nil {
			t.Skip("no hard links here:", err)
		}
	}
	var saveErrors, lostFields, readErrors, reads atomic.Int64
	check := func(s Settings) {
		if s.Theme != "dark" || s.Lang != "zh" || s.Proxy != "direct" || s.GitHubToken != token {
			lostFields.Add(1)
		}
	}
	stop, readDone := make(chan struct{}), make(chan struct{})
	if !linked {
		// Read outside Load's synchronization too: a normal file must be
		// published as a complete snapshot, not merely hidden by a lock.
		go func() {
			defer close(readDone)
			for {
				select {
				case <-stop:
					return
				default:
				}
				b, err := steady.ReadFile(Path())
				var s Settings
				if err != nil || json.Unmarshal(b, &s) != nil {
					readErrors.Add(1)
				} else {
					check(s)
				}
				reads.Add(1)
			}
		}()
	} else {
		close(readDone)
	}
	iterations := 300
	if linked {
		// Each hard-link save syncs the file; fewer writes keep this test quick.
		iterations = 50
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			<-start
			for i := range iterations {
				s := Load()
				check(s)
				s.NoAutoUpdate = i%2 == 0
				if err := Save(s); err != nil {
					saveErrors.Add(1)
				}
			}
		})
	}
	close(start)
	wg.Wait()
	close(stop)
	<-readDone
	check(Load())
	if saveErrors.Load() != 0 || lostFields.Load() != 0 || readErrors.Load() != 0 {
		t.Errorf("concurrent saves: %d save errors, %d lost-field reads, %d incomplete-file reads",
			saveErrors.Load(), lostFields.Load(), readErrors.Load())
	}
	if !linked && reads.Load() == 0 {
		t.Fatal("the independent reader did not observe any saves")
	}
	if linked {
		a, err := os.Stat(Path())
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.Stat(other)
		if err != nil || !os.SameFile(a, b) {
			t.Fatalf("saving split the hard link: %v", err)
		}
	}
}
