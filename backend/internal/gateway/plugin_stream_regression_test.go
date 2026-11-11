package gateway

import (
	"fmt"
	"io"

	"strings"

	"time"
)

const pluginReplyChunk = 8 << 10

var pluginReplyText = strings.Repeat("0123456789abcdef", pluginReplyChunk/16)

func pluginReplyFrame() string {
	return `data: {"id":"large","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"` +
		pluginReplyText + `"},"finish_reason":null}]}` + "\n\n"
}

func pluginReplyRequest(model string, stream bool) string {
	return fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[{"role":"user","content":"local fixture"}]}`, model, stream)
}

type pluginPacedReader struct {
	r       io.Reader
	rate    int64
	read    int64
	started time.Time
}

func (p *pluginPacedReader) Read(b []byte) (int, error) {
	if p.started.IsZero() {
		p.started = time.Now()
	}
	n, err := p.r.Read(b)
	p.read += int64(n)
	if n > 0 && p.rate > 0 {
		due := p.started.Add(time.Duration(p.read * int64(time.Second) / p.rate))
		if wait := time.Until(due); wait > 0 {
			time.Sleep(wait)
		}
	}
	return n, err
}
