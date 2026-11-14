package sessions

import "bytes"

// A Claude Code tool result holds what the tool gave back twice, in its
// message and in toolUseResult: a screenshot is a line of megabytes, and a
// cold read gathered each one whole (takeLong) to read a timestamp, the
// folder and the session id off it. ~200 such lines in a 10GB projects
// folder held 110MB live at the read's peak and set uni-switch's RSS (#3318).
// Such a line is now handed on thinned: every string in it cut to thinKeep
// bytes, the rest kept as it was. A Codex prompt with screenshots is the
// same (a line of 5 to 15MB, 154 of them in a 50GB sessions folder): its
// images are cut, its text is kept.

// thinKeep is how much of a string a thinned line keeps. Over the 1024
// bytes claudeLine routes by, so a line's head reads the same thinned.
const thinKeep = 4 << 10

// thinner cuts a JSON line's strings, a chunk at a time. Every byte
// outside a string is kept, so every key, number and short string is where
// a search of the whole line finds it: a string can't hold an unescaped
// quote, so no search for a quoted key matches inside a cut one.
type thinner struct {
	urls bool // only a data: URL is cut

	in  bool // inside a string
	cut bool // the string is past thinKeep: the rest of it is dropped
	n   int  // the string's bytes so far
	at  int  // where in dst the string starts
	esc int  // an escape's bytes still to come: -1 just after its backslash
}

// add appends chunk, thinned, to dst.
func (t *thinner) add(dst, c []byte) []byte {
	for i := 0; i < len(c); {
		if !t.in {
			j := bytes.IndexByte(c[i:], '"')
			if j < 0 {
				return append(dst, c[i:]...)
			}
			dst = append(dst, c[i:i+j+1]...)
			i += j + 1
			t.in, t.cut, t.n, t.esc, t.at = true, false, 0, 0, len(dst)
			continue
		}
		if t.esc != 0 {
			b := c[i]
			switch {
			case t.esc > 0:
				t.esc--
			case b == 'u':
				t.esc = 4
			default:
				t.esc = 0
			}
			if !t.cut {
				dst = append(dst, b)
			}
			t.n++
			i++
			continue
		}
		r := c[i:]
		q := bytes.IndexByte(r, '"')
		if q < 0 {
			q = len(r)
		}
		e := bytes.IndexByte(r[:q], '\\')
		if e < 0 {
			e = q
		}
		dst = t.keep(dst, r[:e])
		if i += e; i == len(c) {
			break
		}
		if c[i] == '"' {
			dst = append(dst, '"')
			t.in = false
			i++
			continue
		}
		// an escape is kept whole or not at all
		if !t.cut && t.n >= thinKeep && t.cuts(dst) {
			t.cut = true
		}
		if !t.cut {
			dst = append(dst, '\\')
		}
		t.n++
		t.esc = -1
		i++
	}
	return dst
}

// keep appends what of s, a run of a string with no quote or backslash,
// is under thinKeep.
func (t *thinner) keep(dst, s []byte) []byte {
	if t.cut {
		t.n += len(s)
		return dst
	}
	room := thinKeep - t.n
	t.n += len(s)
	if len(s) <= room {
		return append(dst, s...)
	}
	if dst = append(dst, s[:max(room, 0)]...); !t.cuts(dst) {
		return append(dst, s[max(room, 0):]...)
	}
	t.cut = true
	return wholeRunes(dst)
}

// cuts says whether the string, kept to thinKeep bytes in dst, is cut.
func (t *thinner) cuts(dst []byte) bool {
	return !t.urls || bytes.HasPrefix(dst[t.at:], dataURL)
}

var dataURL = []byte("data:")

// wholeRunes is b without a UTF-8 rune its end cuts.
func wholeRunes(b []byte) []byte {
	i := len(b) - 1
	for i >= 0 && len(b)-i < 4 && b[i]&0xC0 == 0x80 {
		i--
	}
	if i < 0 || b[i] < 0xC0 {
		return b
	}
	need := 2
	switch {
	case b[i] >= 0xF0:
		need = 4
	case b[i] >= 0xE0:
		need = 3
	}
	if len(b)-i < need {
		return b[:i]
	}
	return b
}

// thinClaude thins a long Claude line, from its first chunk, that is a
// tool's result, or a line of Claude Code's own that is read for its
// timestamp (an api-request-blob of 15MB): claudeLine and claudeCallLine
// read such a line only for its timestamp, folder, session id and keys,
// all of which a thinned line keeps.
func thinClaude(c []byte) (thinner, bool) {
	return thinner{}, ccResultHead(c)
}

// thinCodex cuts the images of a Codex line its head has let through:
// codexBody reads a prompt's text, never its images.
func thinCodex([]byte) (thinner, bool) {
	return thinner{urls: true}, true
}

func ccResultHead(c []byte) bool {
	c = bytes.TrimLeft(c, " \t\r\n")
	h := c[:min(len(c), 1024)]
	if bytes.HasPrefix(h, ccTop) {
		switch typeAfter(h, ccTop) {
		case "user", "assistant":
		case "ai-title", "custom-title", "summary":
			return false
		default:
			return true
		}
	}
	m, a := bytes.Index(h, ccMessage), bytes.Index(h, ccAttach)
	if m < 0 || a >= 0 && a < m {
		return false
	}
	msg := h[m+len(ccMessage):]
	return bytes.HasPrefix(msg, ccRole) && typeAfter(msg, ccRole) == "user" && ccToolResult(msg)
}
