package modelmeta

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

func bare(s string) string {
	s = strings.ToLower(s)
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	return s
}
func sameHost(a, b string) bool {
	x, e := url.Parse(a)
	y, f := url.Parse(b)
	return e == nil && f == nil && x.Hostname() != "" && strings.EqualFold(x.Hostname(), y.Hostname())
}

var gaps = regexp.MustCompile(`[\s\p{Zs}_-]+`)
var channel = regexp.MustCompile(`^[a-z]{2,24}-`)
var effortSuffix = regexp.MustCompile(`(?i)-(?:minimal|low|medium|high|xhigh|max|fast|thinking)$`)
var datedSuffix = regexp.MustCompile(`-(\d{4}-\d{2}-\d{2}|\d{8})$`)

func digit(b byte) bool { return b >= '0' && b <= '9' }
func snapshot(s string) bool {
	if strings.Trim(s, "0123456789") != "" {
		return false
	}
	if len(s) == 8 && strings.HasPrefix(s, "20") {
		return true
	}
	if len(s) != 6 {
		return false
	}
	y, _ := strconv.Atoi(s[:2])
	m, _ := strconv.Atoi(s[2:4])
	d, _ := strconv.Atoi(s[4:])
	return y >= 23 && y <= 39 && m >= 1 && m <= 12 && d >= 1 && d <= 31
}
func SameModel(s string) string {
	s = bare(strings.TrimSpace(s))
	s = strings.Trim(gaps.ReplaceAllString(s, "-"), "-")
	b := []byte(s)
	for i := 1; i+1 < len(b); i++ {
		if (b[i] == '.' || b[i] == 'p') && digit(b[i-1]) && digit(b[i+1]) {
			b[i] = '-'
		}
	}
	s = string(b)
	if i := strings.LastIndexAny(s, "-@"); i > 0 && snapshot(s[i+1:]) {
		s = s[:i]
	}
	return s
}
func unmarked(s string) (string, int64) {
	if !strings.HasSuffix(s, "]") {
		return s, 0
	}
	i := strings.LastIndexByte(s, '[')
	if i <= 0 {
		return s, 0
	}
	mark := s[i+1 : len(s)-1]
	if len(mark) < 2 {
		return s, 0
	}
	unit := float64(0)
	switch mark[len(mark)-1] {
	case 'k':
		unit = 1000
	case 'm':
		unit = 1000000
	default:
		return s, 0
	}
	n, err := strconv.ParseFloat(mark[:len(mark)-1], 64)
	if err != nil || n <= 0 || n*unit > 100_000_000 {
		return s, 0
	}
	return s[:i], int64(n * unit)
}
func unprofiled(s string) string {
	for _, p := range []string{"global.", "us.", "us-gov.", "eu.", "apac.", "jp.", "au.", "ca.", "in."} {
		if rest, ok := strings.CutPrefix(s, p); ok && strings.Contains(rest, ".") {
			return rest
		}
	}
	return s
}
func untagged(s string) (string, bool) {
	if clean, _ := unmarked(strings.TrimSpace(s)); clean != strings.TrimSpace(s) {
		return "", false
	}
	t := strings.TrimSpace(s)
	for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}, {"（", "）"}, {"【", "】"}, {"「", "」"}} {
		if strings.HasSuffix(t, pair[1]) {
			if i := strings.LastIndex(t, pair[0]); i > 0 && (pair[0] != "[" || t[i-1] == ' ') {
				t = strings.TrimSpace(t[:i])
				break
			}
		}
		if strings.HasPrefix(t, pair[0]) {
			if i := strings.Index(t, pair[1]); i > 0 && i+len(pair[1]) < len(t) {
				t = strings.TrimSpace(t[i+len(pair[1]):])
				break
			}
		}
	}
	t = strings.Trim(t, " -_·.")
	return t, t != s && t != "" && strings.ContainsFunc(t, unicode.IsLetter)
}
func (c *Catalog) found(s string) (string, bool) {
	if _, ok := c.aggregates[s]; ok {
		return s, true
	}
	if p := unprofiled(s); p != s {
		return c.found(p)
	}
	if i := strings.IndexByte(s, '('); i > 0 && strings.HasSuffix(s, ")") {
		if _, ok := c.aggregates[s[:i]]; ok {
			return s[:i], true
		}
		s = s[:i]
	}
	if i := strings.IndexByte(s, ':'); i > 0 {
		if _, ok := c.aggregates[s[:i]]; ok {
			return s[:i], true
		}
	}
	return "", false
}
func (c *Catalog) match(id string) (string, int64, bool) {
	s := bare(id)
	for depth := 0; depth < 6; depth++ {
		if k, ok := c.found(s); ok {
			return k, 0, true
		}
		if !c.listed[s] && !c.canonical[s] {
			r, mark := unmarked(s)
			if r != s {
				if k, ok := c.found(r); ok {
					return k, mark, true
				}
			}
			for _, prefix := range []string{"claude-", channel.FindString(r)} {
				if prefix != "" && strings.HasPrefix(r, prefix) {
					rest := strings.TrimPrefix(r, prefix)
					if strings.ContainsAny(rest, "0123456789") {
						if k, ok := c.found(rest); ok {
							return k, mark, true
						}
					}
				}
			}
		}
		if k, ok := c.spellings[SameModel(s)]; ok {
			return k, 0, true
		}
		if t, ok := untagged(s); ok {
			s = t
			continue
		}
		break
	}
	return "", 0, false
}
