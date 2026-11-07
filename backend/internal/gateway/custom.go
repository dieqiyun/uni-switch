package gateway

import (
	"encoding/json"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// Incrementally locate the top-level input string and decode its complete
// characters. Escapes, surrogate pairs and UTF-8 split across chunks wait.
// This is an independent implementation of custom_tool.customStream's rule.
type customReader struct {
	depth                                                             int
	inString, keyString, escaped, expectKey, expectValue, input, done bool
	key                                                               []byte
	last                                                              string
	pending                                                           []byte
}

func (c *customReader) feed(s string) string {
	if c.done {
		return ""
	}
	b := []byte(s)
	if c.input {
		c.pending = append(c.pending, b...)
		return c.take()
	}
	for i, ch := range b {
		if c.inString {
			if c.keyString {
				c.key = append(c.key, ch)
			}
			if c.escaped {
				c.escaped = false
				continue
			}
			if ch == '\\' {
				c.escaped = true
				continue
			}
			if ch == '"' {
				c.inString = false
				if c.keyString {
					_ = json.Unmarshal(append([]byte{'"'}, c.key...), &c.last)
					c.keyString = false
					c.expectKey = false
				}
			}
			continue
		}
		switch ch {
		case '{', '[':
			c.depth++
			if c.depth == 1 && ch == '{' {
				c.expectKey = true
			}
		case '}', ']':
			c.depth--
			if c.depth == 0 {
				c.done = true
				return ""
			}
		case ',':
			if c.depth == 1 {
				c.expectKey = true
				c.expectValue = false
				c.last = ""
			}
		case ':':
			if c.depth == 1 {
				c.expectValue = true
			}
		case '"':
			if c.depth == 0 || c.depth == 1 && c.expectValue && c.last == "input" {
				c.input = true
				c.pending = append(c.pending, b[i+1:]...)
				return c.take()
			}
			c.inString = true
			c.keyString = c.depth == 1 && c.expectKey
			c.key = nil
		}
	}
	return ""
}
func (c *customReader) take() string {
	out := []byte{}
	i := 0
	for i < len(c.pending) {
		b := c.pending[i]
		if b == '"' {
			c.done = true
			i++
			break
		}
		if b == '\\' {
			if i+1 >= len(c.pending) {
				break
			}
			next := c.pending[i+1]
			if next == 'u' {
				if i+6 > len(c.pending) {
					break
				}
				n, e := strconv.ParseUint(string(c.pending[i+2:i+6]), 16, 16)
				if e != nil {
					c.done = true
					break
				}
				r := rune(n)
				step := 6
				if r >= 0xd800 && r <= 0xdbff {
					if i+12 > len(c.pending) {
						break
					}
					if string(c.pending[i+6:i+8]) != "\\u" {
						c.done = true
						break
					}
					lo, e := strconv.ParseUint(string(c.pending[i+8:i+12]), 16, 16)
					if e != nil || lo < 0xdc00 || lo > 0xdfff {
						c.done = true
						break
					}
					r = utf16.DecodeRune(r, rune(lo))
					step = 12
				} else if r >= 0xdc00 && r <= 0xdfff {
					c.done = true
					break
				}
				out = utf8.AppendRune(out, r)
				i += step
				continue
			}
			mapped := map[byte]byte{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}
			v, ok := mapped[next]
			if !ok {
				c.done = true
				break
			}
			out = append(out, v)
			i += 2
			continue
		}
		if !utf8.FullRune(c.pending[i:]) {
			break
		}
		r, n := utf8.DecodeRune(c.pending[i:])
		if r == utf8.RuneError && n == 1 {
			c.done = true
			break
		}
		out = append(out, c.pending[i:i+n]...)
		i += n
	}
	c.pending = append(c.pending[:0], c.pending[i:]...)
	return string(out)
}
