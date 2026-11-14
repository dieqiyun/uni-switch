package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/budget"
	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
)

// The web search MCP server's second tool, web_fetch (#2286, blackrion):
// an agent that found a page with web_search, and has no fetch tool of its
// own, reads it. The URL is the agent's, so the page is downloaded the way
// a caller's image is (publicClient): from public addresses only, through
// every redirect, so a gateway key's holder can't have uni-switch read a
// service only this computer, or the user's network, reaches. A page is
// read for at most webFetchTimeout and webFetchRead bytes; an HTML page is
// given as Markdown-like text, other text as it is, anything else refused.
// A long page comes in parts of max_chars characters, offset naming where
// the next starts.

const fetchMCPTool = "web_fetch"

const (
	webFetchTimeout  = 30 * time.Second
	webFetchRead     = 5 << 20
	webFetchChars    = 20000
	webFetchMaxChars = 100000
)

// webFetchClient downloads the page; a test swaps it.
var webFetchClient = publicClient

func fetchTool() map[string]any {
	return map[string]any{
		"name":        fetchMCPTool,
		"title":       "Web fetch",
		"description": "Fetch a web page by its http(s) URL and return its text (an HTML page as Markdown, with its links). A long page comes in parts: the answer says the offset to read on from.",
		"inputSchema": json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"The page's http:// or https:// URL"},"max_chars":{"type":"integer","description":"At most this many characters (default 20000, at most 100000)"},"offset":{"type":"integer","description":"The character to start from, to read on in a long page (default 0)"}},"required":["url"]}`),
		"annotations": map[string]any{"readOnlyHint": true, "openWorldHint": true},
	}
}

type fetchArgs struct {
	URL      string `json:"url"`
	MaxChars int    `json:"max_chars"`
	Offset   int    `json:"offset"`
}

// fetchMCPRun reads the page a web_fetch names, held to the calling gateway
// key's limit as a search is.
func (s *Server) fetchMCPRun(r *http.Request, a fetchArgs) (string, error) {
	raw := strings.TrimSpace(a.URL)
	if raw == "" {
		return "", searchMCPError("web_fetch needs a url")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", searchMCPError("web_fetch reads only http:// and https:// URLs, not " + raw)
	}
	if a.Offset < 0 {
		return "", searchMCPError("offset can't be negative")
	}
	limit := a.MaxChars
	if limit <= 0 {
		limit = webFetchChars
	}
	limit = min(limit, webFetchMaxChars)
	release, refused := budget.Reserve(access.Caller(r.Context()), int64(len(raw)), "", limitClock())
	if refused != nil {
		return "", refused
	}
	defer release()
	ctx, cancel := context.WithTimeout(netproxy.With(r.Context(), ""), webFetchTimeout)
	defer cancel()
	title, text, final, err := fetchPage(ctx, u)
	if err != nil {
		return "", searchMCPError("couldn't fetch " + u.Redacted() + ": " + err.Error())
	}
	return fetchPart(final, title, text, a.Offset, limit), nil
}

// fetchPart is what web_fetch answers: where the page was read from, its
// title, and the part of its text from offset, at most limit characters,
// saying where the rest starts.
func fetchPart(final, title, text string, offset, limit int) string {
	var b strings.Builder
	b.WriteString("URL: " + final + "\n")
	if title != "" {
		b.WriteString("Title: " + title + "\n")
	}
	b.WriteString("\n")
	runes := []rune(text)
	if offset >= len(runes) {
		if len(runes) == 0 {
			b.WriteString("(the page has no text)")
		} else {
			fmt.Fprintf(&b, "(the page has %d characters, none from offset %d)", len(runes), offset)
		}
		return b.String()
	}
	end := min(offset+limit, len(runes))
	b.WriteString(string(runes[offset:end]))
	if end < len(runes) {
		fmt.Fprintf(&b, "\n\n[%d more characters: call web_fetch with offset=%d to read on]", len(runes)-end, end)
	}
	return b.String()
}

// fetchPage downloads u and gives its title, its text, and the URL it was
// read from after redirects.
func fetchPage(ctx context.Context, u *url.URL) (title, text, final string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.5")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; web-fetch)")
	// the URL each redirect leads to, by name: the response's own request
	// is the one sent, to the address the name was pinned to
	last := req.URL
	c := *webFetchClient
	c.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if webFetchClient.CheckRedirect != nil {
			if err := webFetchClient.CheckRedirect(next, via); err != nil {
				return err
			}
		} else if len(via) >= 10 {
			return fmt.Errorf("it redirects more than 10 times")
		}
		last = next.URL
		return nil
	}
	resp, err := c.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", "", "", fmt.Errorf("it took longer than %s", webFetchTimeout)
		}
		if errors.Is(err, errPrivateAddress) {
			return "", "", "", errors.New("it is at a private address (this computer, its network, or a name that resolves to one), which web_fetch doesn't read")
		}
		return "", "", "", err
	}
	defer resp.Body.Close()
	final = last.Redacted()
	body, err := io.ReadAll(io.LimitReader(resp.Body, webFetchRead))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", "", "", fmt.Errorf("it took longer than %s", webFetchTimeout)
		}
		return "", "", "", err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(body)
	}
	mt, _, _ := mime.ParseMediaType(ct)
	kind := fetchKind(mt)
	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("the site answered %s", resp.Status)
		if kind != "" {
			if t := fetchText(body, ct, kind, last)[1]; strings.TrimSpace(t) != "" {
				msg += ": " + fetchClip(t, 300)
			}
		}
		return "", "", "", fmt.Errorf("%s", msg)
	}
	if kind == "" {
		return "", "", "", fmt.Errorf("it is %s, not a page of text", mt)
	}
	got := fetchText(body, ct, kind, last)
	return got[0], got[1], final, nil
}

// fetchKind is "html" for a page, "text" for other text, "" for neither.
func fetchKind(mt string) string {
	switch {
	case mt == "text/html" || mt == "application/xhtml+xml":
		return "html"
	case strings.HasPrefix(mt, "text/"), mt == "application/json", mt == "application/xml",
		strings.HasSuffix(mt, "+json"), strings.HasSuffix(mt, "+xml"),
		mt == "application/javascript", mt == "application/x-ndjson":
		return "text"
	}
	return ""
}

// fetchText is a body's title and text, decoded from its charset.
func fetchText(body []byte, ct, kind string, base *url.URL) [2]string {
	r, err := charset.NewReader(strings.NewReader(string(body)), ct)
	if err != nil {
		r = strings.NewReader(string(body))
	}
	if kind == "text" {
		b, _ := io.ReadAll(r)
		return [2]string{"", strings.ToValidUTF8(string(b), "�")}
	}
	doc, err := html.Parse(r)
	if err != nil {
		return [2]string{"", ""}
	}
	return htmlText(doc, base)
}

func fetchClip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// htmlText is an HTML page's title and its text as Markdown: headings,
// lists, links (made absolute), code and paragraphs kept; scripts, styles
// and other markup that isn't read dropped.
func htmlText(doc *html.Node, base *url.URL) [2]string {
	m := &mdWriter{base: base}
	m.walk(doc)
	return [2]string{strings.Join(strings.Fields(m.title), " "), m.done()}
}

type mdWriter struct {
	b     bytes.Buffer
	base  *url.URL
	title string
	pre   int
	space bool // a space is owed before the next word
	tight bool // just after a link's "[", a list's "- " or a heading's "#": no space before the first word
	lists []int
}

var mdSkip = map[string]bool{"script": true, "style": true, "noscript": true, "template": true, "svg": true, "iframe": true, "object": true, "canvas": true, "head": true, "select": true, "button": true, "input": true, "textarea": true}

var mdBlock = map[string]bool{"p": true, "div": true, "section": true, "article": true, "header": true, "footer": true, "main": true, "aside": true, "nav": true, "table": true, "tr": true, "blockquote": true, "form": true, "figure": true, "figcaption": true, "dl": true, "dt": true, "dd": true, "address": true, "details": true, "summary": true, "body": true, "ul": true, "ol": true}

// newline ends the current line, at most n blank-ish breaks in a row.
func (m *mdWriter) newline(n int) {
	m.tight = false
	s := m.b.Bytes()
	have := len(s) - len(bytes.TrimRight(s, "\n"))
	if len(s) == 0 {
		have = n
	}
	for ; have < n; have++ {
		m.b.WriteByte('\n')
	}
	m.space = false
}

func (m *mdWriter) atLineStart() bool {
	s := m.b.Bytes()
	return len(s) == 0 || s[len(s)-1] == '\n'

}

func (m *mdWriter) words(t string) {
	if m.pre > 0 {
		m.b.WriteString(t)
		return
	}
	lead := len(t) > 0 && strings.TrimLeft(t[:1], " \t\r\n\f") == ""
	trail := len(t) > 0 && strings.TrimRight(t[len(t)-1:], " \t\r\n\f") == ""
	f := strings.Fields(t)
	if len(f) == 0 {
		if lead {
			m.space = true
		}
		return
	}
	if (lead || m.space) && !m.atLineStart() && !m.tight {
		m.b.WriteByte(' ')
	}
	m.tight = false
	m.b.WriteString(strings.Join(f, " "))
	m.space = trail
}

func attr(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, k string) bool {
	for _, a := range n.Attr {
		if a.Key == k {
			return true
		}
	}
	return false
}

func (m *mdWriter) kids(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		m.walk(c)
	}
}

func (m *mdWriter) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		m.words(n.Data)
		return
	case html.DocumentNode:
		m.kids(n)
		return
	case html.ElementNode:
	default:
		return
	}
	tag := n.Data
	if tag == "title" && m.title == "" {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				m.title += c.Data
			}
		}
		return
	}
	if tag == "head" {
		// only its title is read
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "title" {
				m.walk(c)
			}
		}
		return
	}
	if mdSkip[tag] || hasAttr(n, "hidden") || attr(n, "aria-hidden") == "true" {
		return
	}
	switch tag {
	case "br":
		m.b.WriteByte('\n')
		m.space = false
	case "hr":
		m.newline(2)
		m.b.WriteString("---")
		m.newline(2)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		m.newline(2)
		m.b.WriteString(strings.Repeat("#", int(tag[1]-'0')) + " ")
		m.tight = true
		m.kids(n)
		m.newline(2)
	case "li":
		m.newline(1)
		depth := max(len(m.lists)-1, 0)
		m.b.WriteString(strings.Repeat("  ", depth))
		if len(m.lists) > 0 && m.lists[len(m.lists)-1] > 0 {
			fmt.Fprintf(&m.b, "%d. ", m.lists[len(m.lists)-1])
			m.lists[len(m.lists)-1]++
		} else {
			m.b.WriteString("- ")
		}
		m.tight = true
		m.kids(n)
		m.newline(1)
	case "ul", "ol":
		start := 0
		if tag == "ol" {
			start = 1
		}
		m.lists = append(m.lists, start)
		m.newline(1)
		m.kids(n)
		m.lists = m.lists[:len(m.lists)-1]
		m.newline(1)
	case "pre":
		m.newline(2)
		m.b.WriteString("```\n")
		m.pre++
		m.kids(n)
		m.pre--
		m.newline(1)
		m.b.WriteString("```")
		m.newline(2)
	case "code":
		if m.pre > 0 {
			m.kids(n)
			return
		}
		if m.space && !m.atLineStart() {
			m.b.WriteByte(' ')
		}
		m.space = false
		m.b.WriteByte('`')
		m.kids(n)
		m.b.WriteByte('`')
	case "td", "th":
		if !m.atLineStart() {
			m.b.WriteString(" | ")
		}
		m.space = false
		m.kids(n)
	case "a":
		href := strings.TrimSpace(attr(n, "href"))
		link := ""
		if href != "" && !strings.HasPrefix(href, "#") {
			if h, err := m.base.Parse(href); err == nil && (h.Scheme == "http" || h.Scheme == "https") {
				link = h.String()
			}
		}
		if link == "" || m.pre > 0 {
			m.kids(n)
			return
		}
		if m.space && !m.atLineStart() {
			m.b.WriteByte(' ')
		}
		m.space = false
		at := m.b.Len()
		m.b.WriteByte('[')
		m.tight = true
		m.kids(n)
		m.tight = false
		if m.b.Len() == at+1 {
			// a link with no text (an icon's): its URL alone says nothing
			m.b.Truncate(at)
			return
		}
		m.b.WriteString("](" + link + ")")
	case "img":
		if alt := strings.TrimSpace(attr(n, "alt")); alt != "" {
			m.words(" " + alt + " ")
		}
	default:
		if mdBlock[tag] {
			m.newline(2)
			if tag == "blockquote" {
				m.b.WriteString("> ")
			}
			m.kids(n)
			m.newline(2)
			return
		}
		m.kids(n)
	}
}

func (m *mdWriter) done() string {
	lines := strings.Split(m.b.String(), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if t := strings.TrimSpace(l); t == "-" || t == ">" {
			// a list item or quote whose text was all dropped
			l = ""
		}
		if l == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
