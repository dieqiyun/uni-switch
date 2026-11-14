package provider

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"
)

// Codex's own client sends a ChatGPT account's turns to the backend
// zstd-compressed, and nothing else: read in openai/codex at a9b08151,
// codex-rs/core/src/client.rs responses_request_compression (the
// enable_request_compression feature, on by default in
// codex-rs/features/src/lib.rs, a ChatGPT sign-in, the OpenAI provider)
// and codex-rs/http-client/src/request.rs, which compresses the body at
// level 3 and sets Content-Encoding: zstd. Only POST /responses is: an API
// key, another provider, /responses/compact and /models go plain.
//
// ylorn (X, v0.1.1202): a text-only gpt-6-luna turn of 1100224 bytes sent
// plain was answered 503, the same turn zstd-compressed (259 bytes) 200,
// and through uni-switch, which decoded Codex's zstd and sent the backend
// plain JSON, 503 again.

// codexZstd is level 3 (klauspost's SpeedDefault, "roughly equivalent to
// the default Zstandard mode (level 3)"), without the frame checksum,
// which libzstd leaves off by default. EncodeAll is safe to share.
// Codex's codex-rs/http-client/src/request.rs uses
// zstd::stream::encode_all(.., 3), whose unknown-size stream has a 2 MiB
// window. Keep turns within that client envelope too.
//
// The encoder keeps one coder per concurrent EncodeAll, GOMAXPROCS by
// default, and each keeps its history (4 MiB at a 2 MiB window) for the
// life of the process: 74 MB of a live gateway's 288 MB heap on a 14-core
// Mac. A 1 MiB turn encodes in about half a millisecond, so two coders in
// low-memory mode keep up with dozens of agents and hold ~9 MB. Neither
// option changes the bytes sent.
var codexZstd = newCodexZstd()

func newCodexZstd() *zstd.Encoder {
	z, _ := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedDefault),
		zstd.WithEncoderCRC(false),
		zstd.WithWindowSize(2<<20),
		zstd.WithEncoderConcurrency(2),
		zstd.WithLowerEncoderMem(true),
	)
	return z
}

// codexZstdOff leaves turns plain, for tests alone: the fake backends of
// most of the gateway's and provider's tests read the body as it comes, so
// those packages' TestMain sets it (CodexZstdForTests(false)), and the
// tests of the compression clear it again.
var codexZstdOff atomic.Bool

// CodexZstdForTests sets whether turns to the ChatGPT backend go
// compressed, for a test, and returns what puts it back.
func CodexZstdForTests(on bool) (restore func()) {
	was := codexZstdOff.Swap(!on)
	return func() { codexZstdOff.Store(was) }
}

// CodexCompressed sends req's body, body, zstd-compressed as Codex's own
// client sends it, when req is a turn to the ChatGPT backend
// (CodexBase/responses), and as it is otherwise. It is called once the
// body is final and signed, and returns the bytes that go on the wire. The
// body is still the plain body everywhere uni-switch keeps or shows it.
func CodexCompressed(req *http.Request, body []byte) int {
	if req.Method != http.MethodPost || codexZstdOff.Load() || !codexTurnURL(req.URL) {
		return len(body)
	}
	if enc := req.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		return len(body) // already encoded: as Codex, never twice
	}
	z := codexZstd.EncodeAll(body, make([]byte, 0, len(body)/8+64))
	SetBody(req, z)
	req.Header.Del("Content-Length")
	req.Header.Set("Content-Encoding", "zstd")
	return len(z)
}

// codexTurnURL is CodexBase's /responses, where Codex sends a turn.
func codexTurnURL(u *url.URL) bool {
	base, err := url.Parse(CodexBase)
	return err == nil && u != nil && strings.EqualFold(u.Host, base.Host) && u.Path == base.Path+"/responses"
}

// CodexTurnURL says whether u is CodexBase's /responses, where Codex sends
// a turn.
func CodexTurnURL(u *url.URL) bool { return codexTurnURL(u) }
