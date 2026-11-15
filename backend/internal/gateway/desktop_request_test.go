package gateway

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/klauspost/compress/zstd"
)

// Exercise the actual HTTP stack, with Desktop's model alias, reasoning,
// UTF-8 text and content encodings, against a local synthetic provider.
func TestClaudeDesktopRequestBody(t *testing.T) {
	for _, protocol := range []provider.Protocol{provider.Anthropic, provider.Chat, provider.Responses} {
		for _, encoding := range []string{"identity", "gzip", "zstd"} {
			t.Run(string(protocol)+"/"+encoding, func(t *testing.T) {
				f := &fake{reply: sse(
					"event: message_start\ndata: "+`{"type":"message_start","message":{"id":"msg_qa","type":"message","role":"assistant","model":"m1","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
					"event: content_block_start\ndata: "+`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
					"event: content_block_delta\ndata: "+`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"测试成功"}}`,
					"event: content_block_stop\ndata: "+`{"type":"content_block_stop","index":0}`,
					"event: message_delta\ndata: "+`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
					"event: message_stop\ndata: "+`{"type":"message_stop"}`,
				)}
				if protocol == provider.Chat {
					f.reply = sse(`data: {"id":"qa","choices":[{"delta":{"role":"assistant","content":"测试成功"},"finish_reason":null}]}`, `data: {"id":"qa","choices":[{"delta":{},"finish_reason":"stop"}]}`, `data: [DONE]`)
				}
				if protocol == provider.Responses {
					f.reply = sse(`data: {"type":"response.created","response":{"id":"qa","model":"m1"}}`, `data: {"type":"response.output_text.delta","delta":"测试成功"}`, `data: {"type":"response.completed","response":{"id":"qa","status":"completed","usage":{"input_tokens":1,"output_tokens":2}}}`)
				}
				setup(t, protocol, f)
				body := []byte(`{"model":"` + aliasFor("fake/m1") + `","max_tokens":32768,"stream":true,"thinking":{"type":"adaptive"},"output_config":{"effort":"max"},"system":[{"type":"text","text":"用中文回答。"}],"messages":[{"role":"user","content":[{"type":"text","text":"请说明余额限制和请求失败的处理方式。"}]}]}`)
				var wire bytes.Buffer
				switch encoding {
				case "gzip":
					zw := gzip.NewWriter(&wire)
					if _, err := zw.Write(body); err != nil {
						t.Fatal(err)
					}
					if err := zw.Close(); err != nil {
						t.Fatal(err)
					}
				case "zstd":
					zw, err := zstd.NewWriter(&wire)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := zw.Write(body); err != nil {
						t.Fatal(err)
					}
					if err := zw.Close(); err != nil {
						t.Fatal(err)
					}
				default:
					wire.Write(body)
				}
				gw := httptest.NewServer(New().Handler())
				defer gw.Close()
				req, err := http.NewRequest(http.MethodPost, gw.URL+"/v1/messages", &wire)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Content-Encoding", encoding)
				req.Header.Set("Authorization", "Bearer "+TokenFor("claude-desktop"))
				req.Header.Set("User-Agent", "Claude/4.29.0 Electron/37.0")
				resp, err := gw.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				got, err := io.ReadAll(resp.Body)
				if err != nil || resp.StatusCode != 200 || !strings.Contains(string(got), "测试成功") {
					t.Fatalf("Desktop request failed: %d %v %s", resp.StatusCode, err, got)
				}
				if f.calls != 1 || !json.Valid(f.got) || !bytes.Contains(f.got, []byte("请求失败的处理方式")) {
					t.Fatalf("upstream received %d calls with invalid or lost JSON: %s", f.calls, f.got)
				}
			})
		}
	}
}
