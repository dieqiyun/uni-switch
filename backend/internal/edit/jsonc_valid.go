package edit

import (
	"bytes"
	"encoding/json"
	"errors"
)

// errEditInvalid is an edit refused because the file it would leave is one
// the app reading it calls invalid.
var errEditInvalid = errors.New("the edit would leave invalid JSON/JSONC; not written")

// ValidJSONC reports whether raw is JSON with comments and trailing commas
// as the apps that write JSONC read it (OpenCode, VS Code and Zed parse
// with jsonc-parser's allowTrailingComma): a comma may end a list of
// members or elements, but a comma with nothing before it ("{ , }", "[,]",
// ",,") is an error there. tidwall/jsonc, which the editors here read
// through, blanks every comma before a closing bracket, so a file that
// passes json.Valid after it can still be one the app calls invalid; an
// edit is checked with this before it is written.
func ValidJSONC(raw []byte) bool {
	b, ok := blankComments(bytes.TrimPrefix(raw, bom))
	if !ok {
		return false
	}
	for _, i := range trailingCommas(b) {
		if lone(b, i) {
			return false
		}
		b[i] = ' '
	}
	return json.Valid(b)
}

// blankComments is a copy of raw with each comment turned to spaces, line
// breaks kept, so offsets in it are offsets in raw; false when a block
// comment is never closed.
func blankComments(raw []byte) ([]byte, bool) {
	b := append([]byte(nil), raw...)
	inStr := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case inStr:
			if c == '\\' {
				i++
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for ; i < len(b) && b[i] != '\n'; i++ {
				b[i] = ' '
			}
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			end := bytes.Index(b[i+2:], []byte("*/"))
			if end < 0 {
				return nil, false
			}
			for j := i; j < i+2+end+2; j++ {
				if b[j] != '\n' && b[j] != '\r' {
					b[j] = ' '
				}
			}
			i += 2 + end + 1
		}
	}
	return b, true
}

// trailingCommas are the offsets of the commas in comment-free b, outside
// strings, that a closing bracket follows.
func trailingCommas(b []byte) []int {
	var at []int
	inStr := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case inStr:
			if c == '\\' {
				i++
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == ',':
			j := i + 1
			for j < len(b) && isSpace(b[j]) {
				j++
			}
			if j < len(b) && (b[j] == '}' || b[j] == ']') {
				at = append(at, i)
			}
		}
	}
	return at
}

// lone is whether the comma at i in comment-free b has no value before it:
// an opening bracket, another comma or a colon.
func lone(b []byte, i int) bool {
	k := i - 1
	for k >= 0 && isSpace(b[k]) {
		k--
	}
	return k < 0 || b[k] == ',' || b[k] == '{' || b[k] == '[' || b[k] == ':'
}

// dropLoneCommas is raw without the commas before a closing bracket that
// have no value before them: what uni-switch's own delete left in a JSONC file
// when the member it took out was the only one and had a trailing comma
// ("mcp": {,}), which the app then calls invalid. Anything else is left as
// it is.
func dropLoneCommas(raw []byte) []byte {
	b, ok := blankComments(raw)
	if !ok {
		return raw
	}
	at := trailingCommas(b)
	var out []byte
	for n := len(at) - 1; n >= 0; n-- {
		if i := at[n]; lone(b, i) {
			if out == nil {
				out = append([]byte(nil), raw...)
			}
			out = append(out[:i], out[i+1:]...)
		}
	}
	if out == nil {
		return raw
	}
	return out
}

// checkJSONC is out, an edit of orig, or an error when the edit would
// leave a file the app reads as invalid that it read before.
func checkJSONC(out, orig []byte) ([]byte, error) {
	if !ValidJSONC(out) && ValidJSONC(orig) {
		return nil, errEditInvalid
	}
	return out, nil
}
