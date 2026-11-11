package provider

// Claude Code on its own login asks Anthropic itself, so uni-switch learns the
// account it is on is out only from a /usage run, which unasked comes
// claudeUsageWait (5 to 15 minutes) after the last, and the switch looked
// only every loginSwitchEvery. An account burnt from 4% left to its limit
// in two minutes, and Claude Code said "You've hit your 5-hour limit" for
// over a minute while the other account had all of it left (#4443,
// mydosu). So while Claude has an account to move to, its sign-in is
// looked at every claudeLookEvery, and two things make that look read
// /usage at once rather than at its wait:
//
//   - the account near out (claudeNearOut): read every claudeNearOutWait;
//   - Claude Code having written a rate_limit refusal into one of its
//     sessions since the last read (scanClaudeLimits), which is what it
//     shows as "You've hit your … limit". Which account it was refused on
//     the transcript doesn't say, so it is only the cue for the read; the
//     read decides.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// claudeLookEvery is how often Claude's sign-in is looked at between
// KeepOnAnAccountWithRoom's full looks, while it has an account to move to.
// A var so tests can say.
var claudeLookEvery = time.Minute

// claudeNearOutWait is how long after a /usage run the next unasked one is
// due while the account Claude Code is on is near out, in place of
// claudeUsageWait.
const claudeNearOutWait = time.Minute

// claudeNearOut says the windows ws, of the account Claude Code is on, are
// near out: one that stops the account for every model is at
// codexNearShare or past.
func claudeNearOut(ws []QuotaWindow, now time.Time) bool {
	return usedPast(SubscriptionQuota{Windows: ws}, codexNearShare, now)
}

// claudeWaitOf is how long after e's last /usage run the next unasked one
// of the account Claude Code is on is due.
func claudeWaitOf(e claudeUsageEntry, now time.Time) time.Duration {
	if e.ws != nil && claudeNearOut(e.windows(now), now) {
		return min(e.wait, claudeNearOutWait)
	}
	return e.wait
}

// claudeLimitHit is when Claude Code last wrote a rate_limit refusal into
// one of its sessions, as unix nanoseconds (zero: none seen).
var claudeLimitHit atomic.Int64

// claudeLimitHitAfter says Claude Code was refused for its limit after t.
func claudeLimitHitAfter(t time.Time) bool {
	n := claudeLimitHit.Load()
	return n != 0 && time.Unix(0, n).After(t)
}

// claudeTranscripts is where scanClaudeLimits got to: the sessions written
// to are read on from where the last scan stopped, so each line is read
// once.
var claudeTranscripts struct {
	sync.Mutex
	started bool
	off     map[string]int64
}

// claudeScanTail is the most of one session read by one scan: a refusal is
// the last line Claude Code writes, and a session that grew more than this
// since is read from its end.
const claudeScanTail = 1 << 20

// scanClaudeLimits reads what Claude Code wrote to its sessions (each
// project's *.jsonl) since the last scan and keeps the time of the latest
// rate_limit refusal in claudeLimitHit. The first scan only marks where the
// sessions end: what was refused before uni-switch looked is the startup
// read's to tell. Its cost is a stat of each session and a read of what
// was added to the ones written to.
func scanClaudeLimits() {
	c := &claudeTranscripts
	c.Lock()
	defer c.Unlock()
	projects := filepath.Join(filepath.Dir(claudeCredentialsPath()), "projects")
	dirs, _ := os.ReadDir(projects)
	off := make(map[string]int64, len(c.off))
	for _, d := range dirs {
		root := filepath.Join(projects, d.Name())
		if !d.IsDir() {
			if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
				continue
			}
		}
		files, _ := os.ReadDir(root)
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			fi, err := f.Info()
			if err != nil {
				continue
			}
			path := filepath.Join(root, f.Name())
			was, known := c.off[path]
			switch {
			case !c.started:
				off[path] = fi.Size()
			case known && was == fi.Size():
				off[path] = was
			default:
				if !known {
					was = 0 // a session begun since the last scan
				}
				off[path] = scanClaudeSession(path, was, fi.Size())
			}
		}
	}
	c.off, c.started = off, true
}

// scanClaudeSession reads path from from to size for rate_limit refusals,
// and answers where it got to: the end of its last whole line, so a line
// still being written is read whole next time.
func scanClaudeSession(path string, from, size int64) int64 {
	if size < from {
		from = 0 // written over
	}
	start := max(from, size-claudeScanTail)
	f, err := os.Open(path)
	if err != nil {
		return from
	}
	defer f.Close()
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return from
	}
	r := bufio.NewReader(io.LimitReader(f, size-start))
	pos := start
	if start > from {
		// started mid-line: what is before the first newline is cut
		skipped, err := r.ReadBytes('\n')
		pos += int64(len(skipped))
		if err != nil {
			return size // all one line not ended, longer than any refusal
		}
	}
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return pos // the rest is a line not yet ended
		}
		pos += int64(len(line))
		noteClaudeRefusal(line)
	}
}

// claudeRefusal is what marks a line Claude Code writes for a refusal for
// the account's limit ("You've hit your session limit"), cheaply found
// before it is parsed.
var claudeRefusal = []byte(`"rate_limit"`)

// noteClaudeRefusal keeps the time of line in claudeLimitHit when it is
// Claude Code's own message for a rate_limit refusal.
func noteClaudeRefusal(line []byte) {
	if !bytes.Contains(line, claudeRefusal) {
		return
	}
	var m struct {
		Error     string `json:"error"`
		APIError  bool   `json:"isApiErrorMessage"`
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal(line, &m) != nil || m.Error != "rate_limit" || !m.APIError {
		return
	}
	at, err := time.Parse(time.RFC3339Nano, m.Timestamp)
	if err != nil {
		at = time.Now()
	}
	for {
		was := claudeLimitHit.Load()
		if at.UnixNano() <= was || claudeLimitHit.CompareAndSwap(was, at.UnixNano()) {
			return
		}
	}
}

// claudeLookDue says Claude's sign-in is to be looked at before the next
// full look: it has an account to move to, and the account Claude Code is
// on is near out, or Claude Code was refused for its limit since that
// account's last /usage run. A refusal makes the account's reading stale
// (StaleAllowance), so the look reads it even with allowances read only
// when asked.
func claudeLookDue() bool {
	if leftAlone("claude") {
		return false
	}
	var active string
	spare := false
	for _, l := range Logins("claude") {
		switch {
		case l.Active:
			active = l.User
		case l.On && l.Lapsed == "" && l.home == "" && !l.Token:
			spare = true
		}
	}
	if active == "" || !spare {
		return false
	}
	scanClaudeLimits()
	claudeUsage.Lock()
	e := claudeUsage.m[strings.ToLower(active)]
	claudeUsage.Unlock()
	if claudeLimitHitAfter(e.tried) {
		StaleAllowance("claude", active)
		return true
	}
	return e.ws != nil && claudeNearOut(e.windows(time.Now()), time.Now())
}
