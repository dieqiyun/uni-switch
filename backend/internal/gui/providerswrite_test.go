package gui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// bigProviders is an answer the size real accounts give the Providers
// page: many providers, each with a long model list.
func bigProviders() providersJSON {
	var s providersJSON
	for p := range 30 {
		row := providerJSON{ID: fmt.Sprintf("p%d", p), Name: "Provider <one> & two", Chat: "https://api.example.com/v1"}
		for m := range 200 {
			row.Models = append(row.Models, modelJSON{ID: fmt.Sprintf("model-%d", m), Name: fmt.Sprintf("Model %d", m), Efforts: []string{"low", "high"}})
		}
		s.Providers = append(s.Providers, row)
	}
	s.Presets = []presetJSON{}
	s.CodexApp = "someone"
	s.Movable = []string{"x"}
	return s
}

// The answer is written as writeJSON wrote it, with no providers too.
func TestProvidersWrittenAsEncoded(t *testing.T) {
	for _, s := range []providersJSON{bigProviders(), {Providers: []providerJSON{}}, {}} {
		w := &sinkWriter{keep: true}
		writeProvidersJSON(w, s)
		var want bytes.Buffer
		if err := json.NewEncoder(&want).Encode(s); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(w.body.Bytes(), want.Bytes()) {
			t.Fatalf("the answer was written as\n%.300s\nwant\n%.300s", w.body.Bytes(), want.Bytes())
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type %q", ct)
		}
	}
}

// Each poll of the Providers page encoded the answer whole, growing a
// buffer to its size: 27 MB of what forty polls allocated. Each provider
// is encoded alone now.
func TestProvidersWriteGrowsNoBufferItsSize(t *testing.T) {
	if raceOn {
		t.Skip("-race: sync.Pool drops what it holds, so allocations are not what a build allocates")
	}
	s := bigProviders()
	w := &sinkWriter{}
	writeProvidersJSON(w, s) // json's encoders, warmed
	size := w.n
	const runs = 5
	var all uint64
	for range runs {
		// polls are seconds apart, and a GC between them empties the
		// buffers json pools (twice: a pool keeps a GC's victims once)
		runtime.GC()
		runtime.GC()
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		writeProvidersJSON(&sinkWriter{}, s)
		runtime.ReadMemStats(&m1)
		all += m1.TotalAlloc - m0.TotalAlloc
	}
	if per := int(all) / runs; per > size/2 {
		t.Fatalf("writing a %d-byte answer allocated %d bytes, want under half its size", size, per)
	}
}

// The sum writeProvidersJSON takes while writing is the one keepProviders
// takes of what it keeps: rows with headers, a proxy or an extra body
// summed without them, the gateway's calls and moved models left out.
func TestProvidersWriteSumsWhatIsKept(t *testing.T) {
	secret := bigProviders()
	secret.Providers[0].Headers = map[string]string{"X-Key": "k"}
	secret.Providers[1].Proxy = "socks5://127.0.0.1:1080"
	secret.Providers[2].ExtraBody = provider.ExtraBody{"a": json.RawMessage(`1`)}
	secret.Providers[3].AccountProxies = map[string]string{"a": "http://p"}
	secret.Gateway.Calls = []gateway.Call{{ID: 1}}
	secret.Moved = []agent.Move{{Agent: "codex", Field: "model", From: "a", To: "b"}}
	secret.Fetching, secret.Added = true, 2
	for _, s := range []providersJSON{bigProviders(), secret, {Providers: []providerJSON{}}, {}} {
		sum, ok := writeProvidersJSON(&sinkWriter{}, s)
		want, err := encodedSum(keptProviders(s))
		if err != nil {
			t.Fatal(err)
		}
		if !ok || sum != want {
			t.Fatalf("writing %d providers summed %x (ok %v), want %x", len(s.Providers), sum[:4], ok, want[:4])
		}
	}
}
