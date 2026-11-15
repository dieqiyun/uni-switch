package imagemcp

import (
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The gateway gives a drawing up to Settings' most minutes, and a video
// several calls; this process, which may not read those settings (WSL),
// waits on a call longer than either, so it never cuts one the gateway is
// still waiting on.
func TestCallOutwaitsTheLongestDrawing(t *testing.T) {
	if got, want := callLongest(), 2*settings.ImageTimeoutMost*time.Minute; got <= want || got < ToolTimeout() {
		t.Fatalf("a call is given %v, a drawing up to %v, a tool %v", got, want, ToolTimeout())
	}
}
