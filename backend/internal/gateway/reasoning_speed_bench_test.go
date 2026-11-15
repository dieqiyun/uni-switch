package gateway

import (
	"testing"
	"time"
)

func BenchmarkReasoningTiming(b *testing.B) {
	ev := []byte(`data: {"choices":[{"delta":{"reasoning_content":"The user wants a file written."}}]}` + "\n\n")
	b.ReportAllocs()
	for b.Loop() {
		f := firstToken{start: time.Now()}
		f.see(ev)
	}
}
