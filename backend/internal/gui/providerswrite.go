package gui

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
)

// providersNull is how a providersJSON with no providers begins:
// Providers is its first field.
const providersNull = `{"providers":null`

// writeProvidersJSON writes s to rw as writeJSON does, each provider's row
// encoded alone. Encoding the answer whole grew a buffer to its size each
// poll: 27 MB of what forty polls of the Providers page allocated with
// real accounts.
//
// It returns the encodedSum of what keepProviders keeps of s, taken from
// the same encoding: a row kept as it is is summed as it is written, and
// only one with headers, a proxy or an extra body is encoded again
// without them. Encoding the kept list again only to sum it was 37 MB of
// what forty polls allocated.
func writeProvidersJSON(rw http.ResponseWriter, s providersJSON) ([32]byte, bool) {
	rows := s.Providers
	f := keptProviders(s)
	s.Providers, f.Providers.Providers = nil, nil
	tail, err := json.Marshal(s)
	keptTail, kerr := json.Marshal(f)
	v, _ := json.Marshal(Version)
	keptHead := `{"version":` + string(v) + `,"providers":` + providersNull
	if rows == nil || err != nil || kerr != nil ||
		!bytes.HasPrefix(tail, []byte(providersNull)) || !bytes.HasPrefix(keptTail, []byte(keptHead)) {
		s.Providers = rows
		writeJSON(rw, s)
		sum, err := encodedSum(keptProviders(s))
		return sum, err == nil
	}
	rw.Header().Set("Content-Type", "application/json")
	out := bufio.NewWriterSize(rw, 32<<10)
	defer out.Flush()
	h := sha256.New()
	_, _ = out.WriteString(`{"providers":[`)
	h.Write([]byte(keptHead[:len(keptHead)-len("null")] + "["))
	enc := json.NewEncoder(lineless{out})
	both := json.NewEncoder(lineless{io.MultiWriter(out, h)})
	kept := json.NewEncoder(lineless{h})
	for i := range rows {
		if i > 0 {
			_ = out.WriteByte(',')
			h.Write([]byte{','})
		}
		// a pointer, so a method on *T is used as it is in the slice
		if keepsAllOf(&rows[i]) {
			if both.Encode(&rows[i]) != nil {
				return [32]byte{}, false
			}
			continue
		}
		if enc.Encode(&rows[i]) != nil {
			return [32]byte{}, false
		}
		k := keptProvider(rows[i])
		if kept.Encode(&k) != nil {
			return [32]byte{}, false
		}
	}
	_ = out.WriteByte(']')
	_, _ = out.Write(tail[len(providersNull):])
	_ = out.WriteByte('\n')
	h.Write([]byte{']'})
	h.Write(keptTail[len(keptHead):])
	h.Write([]byte{'\n'})
	return [32]byte(h.Sum(nil)), true
}
