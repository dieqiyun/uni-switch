package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// reported holds the request ids of the reported calls usage.jsonl already
// has (#1874), so a client that sends a call twice, or again after a
// restart, adds it once. It is read from the log itself, never kept apart:
// the log is what the page counts, so it is what says what was counted.
// Only reported rows are looked at, so the set is as big as what clients
// have reported, not as the gateway's history.
var reported struct {
	// ingest is held for a whole IngestReported: two posts of one rid at
	// once must not both find it new.
	ingest sync.Mutex
	path   string
	info   os.FileInfo
	off    int64
	rids   map[string]bool
}

// reportedMark is how a reported row's line spells its source, which the
// scan looks for before it parses a line.
var reportedMark = []byte(`"source":"` + SourceReported + `"`)

// catchUpReported reads the lines written to the log since the last look
// and notes the request ids of the reported calls among them. A log that
// is now another file, or shorter, is read again from the start. Called
// with reported.ingest held.
func catchUpReported(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		reported.path, reported.info, reported.off, reported.rids = path, nil, 0, map[string]bool{}
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := statLogHandle(f)
	if err != nil {
		return err
	}
	if reported.rids == nil || reported.path != path || reported.info == nil || !sameLogFile(reported.info, info) || info.Size() < reported.off {
		reported.path, reported.off, reported.rids = path, 0, map[string]bool{}
	}
	reported.info = info
	if _, err := f.Seek(reported.off, io.SeekStart); err != nil {
		return err
	}
	rd := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			break // an unfinished last line is read again once it ends
		}
		reported.off += int64(len(line))
		if !bytes.Contains(line, reportedMark) {
			continue
		}
		var r struct {
			RID    string `json:"rid"`
			Source string `json:"source"`
		}
		if json.Unmarshal(line, &r) == nil && r.Source == SourceReported && r.RID != "" {
			reported.rids[r.RID] = true
		}
	}
	return nil
}

// IngestReported writes the calls a client reported (#1874) to the log
// the gateway writes its own to, under the same lock, each marked
// SourceReported. A call whose request id the log already has as reported,
// or that came earlier in recs, is skipped. It answers, for each record,
// whether it was written. The records must already be checked: each has
// a RequestID.
//
// Nothing else the gateway does on a call happens: no observer sees it
// (the call is the client's, which can send its own), and no caller key's
// budget is spent, since the call didn't go through uni-switch.
func IngestReported(recs []Record) ([]bool, error) {
	reported.ingest.Lock()
	defer reported.ingest.Unlock()
	path := Path()
	// most of the catching up is done before the gateway's lock is taken,
	// so a long log doesn't hold its calls up; what it wrote meanwhile is
	// read under the lock below
	if err := catchUpReported(path); err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	if err := catchUpReported(path); err != nil {
		return nil, err
	}
	took := make([]bool, len(recs))
	var buf []byte
	batch := map[string]bool{}
	for i, r := range recs {
		if r.RequestID == "" || reported.rids[r.RequestID] || batch[r.RequestID] {
			continue
		}
		r.Source = SourceReported
		r.CallerKeyID, r.CallerKeyName, r.CallerIssuerID = "", "", ""
		b, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		buf = append(append(buf, b...), '\n')
		batch[r.RequestID] = true
		took[i] = true
	}
	if len(buf) == 0 {
		return took, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	before, _ := statLogHandle(f)
	if _, err := f.Write(buf); err != nil {
		// what part of it reached the file is read back by the next scan
		return nil, err
	}
	after, _ := statLogHandle(f)
	noteLogAppend(path, before, after)
	// the lines are the log's now: the next scan starts after them
	for rid := range batch {
		reported.rids[rid] = true
	}
	if after != nil && reported.info != nil && sameLogFile(reported.info, after) && before != nil && before.Size() == reported.off {
		reported.info, reported.off = after, after.Size()
	}
	return took, nil
}
