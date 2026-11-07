package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A client that speaks Messages/Chat keeps no Responses reasoning items.
// Following reasoning_carry, a content-addressed local record travels by its
// key in the first call ID. Records are also credential/model scoped so an
// account change cannot replay another account's encrypted reasoning.
const carryMark = "__usr32__"

type reasoningCache struct{ directory, scope string }
type reasoningRecord struct {
	Scope string   `json:"scope"`
	At    int64    `json:"at"`
	Items []Object `json:"items"`
}

func cacheFor(c Call, model string) reasoningCache {
	if c.CacheDirectory == "" {
		return reasoningCache{}
	}
	h := sha256.Sum256([]byte(c.BaseURL + "\x00" + c.APIKey + "\x00" + model))
	return reasoningCache{directory: filepath.Join(c.CacheDirectory, "reasoning"), scope: hex.EncodeToString(h[:])}
}
func (c reasoningCache) keep(id string, items []Object) string {
	if c.directory == "" || len(items) == 0 {
		return id
	}
	b, e := json.Marshal(items)
	if e != nil || len(b) > maxBody {
		return id
	}
	h := sha256.Sum256(append([]byte(c.scope), b...))
	key := hex.EncodeToString(h[:16])
	record, _ := json.Marshal(reasoningRecord{c.scope, time.Now().Unix(), items})
	if os.MkdirAll(c.directory, 0700) != nil {
		return id
	}
	tmp, e := os.CreateTemp(c.directory, ".reason-*")
	if e != nil {
		return id
	}
	name := tmp.Name()
	defer os.Remove(name)
	if tmp.Chmod(0600) != nil {
		tmp.Close()
		return id
	}
	if _, e = tmp.Write(record); e == nil {
		e = tmp.Sync()
	}
	closeErr := tmp.Close()
	if e != nil || closeErr != nil {
		return id
	}
	path := filepath.Join(c.directory, key+".json")
	if os.Rename(name, path) != nil {
		// A concurrent worker may have written the identical content already.
		if _, e := os.Stat(path); e != nil {
			return id
		}
	}
	c.prune()
	if id == "" {
		id = uid("call_")
	}
	return id + carryMark + key
}
func splitCarried(id string) (string, string) {
	at := len(id) - len(carryMark) - 32
	if at < 1 || id[at:at+len(carryMark)] != carryMark {
		return id, ""
	}
	key := id[at+len(carryMark):]
	b, e := hex.DecodeString(key)
	if e != nil || len(b) != 16 {
		return id, ""
	}
	return id[:at], key
}
func (c reasoningCache) restore(r *Request) {
	if c.directory == "" {
		return
	}
	for i, m := range r.Turns {
		lead := []Part{}
		seen := map[string]bool{}
		for j, p := range m.Parts {
			if p.Kind != "call" && p.Kind != "result" {
				continue
			}
			own, key := splitCarried(p.ID)
			if key == "" {
				continue
			}
			r.Turns[i].Parts[j].ID = own
			if p.Kind != "call" || seen[key] {
				continue
			}
			seen[key] = true
			file, e := os.Open(filepath.Join(c.directory, key+".json"))
			if e != nil {
				continue
			}
			b, e := io.ReadAll(io.LimitReader(file, maxBody+1))
			file.Close()
			if e != nil || len(b) > maxBody {
				continue
			}
			var record reasoningRecord
			if json.Unmarshal(b, &record) != nil || record.Scope != c.scope || time.Since(time.Unix(record.At, 0)) > 7*24*time.Hour {
				continue
			}
			for _, it := range record.Items {
				if str(it["type"]) == "gemini_signature" {
					r.Turns[i].Parts[j].Signature = str(it["signature"])
					r.Turns[i].Parts[j].SealedBy = "gemini"
				}
				if str(it["type"]) == "reasoning" && str(it["encrypted_content"]) != "" {
					lead = append(lead, Part{Kind: "thinking", Raw: it, SealedBy: "responses"})
				}
			}
		}
		r.Turns[i].Parts = append(lead, r.Turns[i].Parts...)
	}
}
func (c reasoningCache) prune() {
	entries, e := os.ReadDir(c.directory)
	if e != nil {
		return
	}
	// Only our own fixed-format records; never walk caller-controlled subtrees.
	type cached struct {
		name string
		at   time.Time
		size int64
	}
	kept := []cached{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || len(entry.Name()) != 37 {
			continue
		}
		fi, e := entry.Info()
		if e == nil && fi.Mode().IsRegular() {
			if time.Since(fi.ModTime()) > 7*24*time.Hour {
				_ = os.Remove(filepath.Join(c.directory, entry.Name()))
			} else {
				kept = append(kept, cached{entry.Name(), fi.ModTime(), fi.Size()})
			}
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].at.After(kept[j].at) })
	var total int64
	for i, k := range kept {
		total += k.size
		if i >= 1024 || total > 64<<20 {
			_ = os.Remove(filepath.Join(c.directory, k.name))
		}
	}
}
func (c reasoningCache) renderResult(r *Result, from, to Protocol) {
	if to == Gemini && from != Gemini {
		for i, p := range r.Parts {
			if p.Kind == "call" && p.Signature != "" {
				r.Parts[i].ID = c.keep(p.ID, []Object{{"type": "gemini_signature", "signature": p.Signature}})
			}
		}
		return
	}
	if from == Responses || to != Responses {
		return
	}
	held := []Object{}
	for i, p := range r.Parts {
		if p.Kind == "thinking" && p.SealedBy == "responses" && str(p.Raw["encrypted_content"]) != "" {
			held = append(held, p.Raw)
		} else if p.Kind == "call" && len(held) > 0 {
			r.Parts[i].ID = c.keep(p.ID, held)
			held = nil
		}
	}
}
func (c reasoningCache) stream(from, to Protocol, emit func(Event) error) func(Event) error {
	if to == Gemini && from != Gemini {
		ids := map[int]string{}
		return func(e Event) error {
			if e.Kind == "part" && e.Part.Kind == "call" && e.Part.Signature != "" {
				e.Part.ID = c.keep(e.Part.ID, []Object{{"type": "gemini_signature", "signature": e.Part.Signature}})
				ids[e.Index] = e.Part.ID
			}
			if id := ids[e.Index]; id != "" && (e.Kind == "part_done" || e.Kind == "part_update") {
				e.Part.ID = id
			}
			return emit(e)
		}
	}
	if from == Responses || to != Responses {
		return emit
	}
	held := []Object{}
	ids := map[int]string{}
	awaiting := map[int]bool{}
	var queued []Event
	queuedBytes := 0
	var accept func(Event) error
	accept = func(e Event) error {
		if e.Kind == "part" && e.Part.Kind == "thinking" {
			awaiting[e.Index] = true
		}
		if (e.Kind == "part_done" || e.Kind == "part_update") && e.Part.Kind == "thinking" && e.Part.SealedBy == "responses" && str(e.Part.Raw["encrypted_content"]) != "" {
			held = append(held, e.Part.Raw)
		}
		if e.Kind == "part_done" && e.Part.Kind == "thinking" {
			delete(awaiting, e.Index)
		}
		if len(awaiting) > 0 && (len(queued) > 0 || e.Kind == "part" && e.Part.Kind == "call") {
			queuedBytes += len(e.Text) + len(e.Part.Text) + len(rawArguments(e.Part.Args)) + 128
			if queuedBytes > maxBody {
				return errors.New("等待推理续接记录的工具流超过大小限制")
			}
			queued = append(queued, e)
			return nil
		}
		if e.Kind == "part" && e.Part.Kind == "call" && len(held) > 0 {
			e.Part.ID = c.keep(e.Part.ID, held)
			ids[e.Index] = e.Part.ID
			held = nil
		}
		if e.Kind == "part_done" && ids[e.Index] != "" {
			e.Part.ID = ids[e.Index]
		}
		if err := emit(e); err != nil {
			return err
		}
		if len(awaiting) == 0 && len(queued) > 0 {
			q := queued
			queued = nil
			queuedBytes = 0
			for _, ev := range q {
				if err := accept(ev); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return accept
}
