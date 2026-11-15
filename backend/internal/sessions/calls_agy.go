package sessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Antigravity CLI's (agy's) calls (#2097, parkavenue9639: a call agy made
// without uni-switch was counted nowhere). Each conversation keeps a transcript
// of its steps in
// ~/.gemini/antigravity-cli/brain/<conversation id>/.system_generated/logs/transcript_full.jsonl,
// one JSON object a line. A model's step, as agy 1.2.16 writes it (tried in
// a sandbox against a fake Gemini endpoint):
//
//	{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE",
//	 "created_at":"2026-10-14T06:12:41Z","input_tokens":702,"cache_read_tokens":300,
//	 "output_tokens":9,"content":"…"}
//
// Those are the step's own counts: input_tokens is the prompt less what was
// read from the cache, and output_tokens holds the thoughts as well as the
// answer, as the gateway counts a Gemini reply. created_at is when the step
// began, to the second. A transcript an older agy wrote has the same lines
// without the token fields; they are left out, not counted as zero.
//
// The step names no model (a custom model's isn't anywhere in agy's files,
// and settings.json's is only the one picked now), nor the account, so the
// call is agy's with the model and provider unknown. A step agy sent through
// uni-switch is the gateway's to count: usage's matcher pairs it with the
// gateway's record by its tokens and time (agy sends no session header).

// AgyDir is agy's data folder, ~/.gemini/antigravity-cli.
func AgyDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

// agyCallFiles are each conversation's transcript_full.jsonl.
func agyCallFiles() []file { return agyTranscripts(AgyDir()) }

type agyStep struct {
	Step      *int   `json:"step_index"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
	Input     *int   `json:"input_tokens"`
	CacheRead *int   `json:"cache_read_tokens"`
	Output    *int   `json:"output_tokens"`
}

var agyTokens = []byte(`"output_tokens"`)

// readAgyCalls reads a transcript whole: agy may write a step's line again
// as it finishes, and a step is counted once by its index.
func readAgyCalls(f file) (*callFile, bool) {
	r, err := os.Open(f.path)
	if err != nil {
		return &callFile{Path: f.path, Agent: "agy"}, false
	}
	defer r.Close()
	st := &callFile{Agent: "agy", Path: f.path, Session: strings.TrimPrefix(f.key, "agy:")}
	at := map[int]int{} // a step's index → its call in st.Calls
	br := bufio.NewReaderSize(r, scanBuffer(r, 0))
	for {
		b, err := br.ReadBytes('\n')
		if err != nil && (!errors.Is(err, io.EOF) || len(bytes.TrimSpace(b)) == 0) {
			break
		}
		b = bytes.TrimSpace(b)
		if len(b) > 0 && len(b) <= maxLine && bytes.Contains(b, agyTokens) {
			var s agyStep
			if json.Unmarshal(b, &s) == nil {
				if c, ok := agyCall(st, s); ok {
					if i, seen := at[*s.Step]; seen {
						st.Calls[i] = c
					} else {
						at[*s.Step] = len(st.Calls)
						st.Calls = append(st.Calls, c)
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	return st, true
}

// agyCall is a model's step as a call.
func agyCall(st *callFile, s agyStep) (Call, bool) {
	if s.Source != "MODEL" || s.Step == nil || s.Input == nil || s.Output == nil {
		return Call{}, false
	}
	t, err := time.Parse(time.RFC3339, s.CreatedAt)
	if err != nil {
		return Call{}, false
	}
	c := Call{Agent: "agy", Time: t, Session: st.Session, Tokens: Tokens{Input: *s.Input, Output: *s.Output}}
	if s.CacheRead != nil {
		c.CacheRead = *s.CacheRead
	}
	if c.Tokens.zero() {
		return Call{}, false
	}
	c.Msg = st.str("agy:" + st.Session + ":" + strconv.Itoa(*s.Step))
	return c, true
}
