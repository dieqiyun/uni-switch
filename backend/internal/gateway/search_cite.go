package gateway

import "strings"

// After a search uni-switch ran for a model (searchReply), or one a provider
// ran and reported (KSearch), the model's answer names the pages it used
// by their addresses: uni-switch's web_search tool asks it to cite the URLs.
// An Anthropic client is told which pages a line cites as Anthropic's own
// search tells it, a text block whose citations are
// web_search_result_location entries for the pages of the reply's
// web_search_tool_result blocks. A line naming a page found is its own
// text block with those citations; the lines between them run together in
// one block as before. The text is the model's, unchanged.
//
// encrypted_index is Anthropic's sealed pointer into its own result, which
// uni-switch can't make: it is "", as the results' encrypted_content is
// (searchResultBlock). cited_text is what the search said of the page, when
// it said something (Hit.Cited), else "".

// citedTextMost is how much of a page's text a citation quotes, as
// Anthropic's does (up to 150 characters).
const citedTextMost = 150

// citer cuts a reply's text into lines once a search has found pages, and
// says which of the pages each line cites. A line that is only whitespace
// is held for the next, so no block is whitespace alone (Anthropic turns
// such a block away when the client sends it back).
type citer struct {
	hits []Hit
	line string // the line being said, not yet ended
	held string // whitespace lines held for the next line
}

// on is whether there are pages a line may cite.
func (c *citer) on() bool { return len(c.hits) > 0 }

// feed takes text and says each line it ends: its text and citations.
func (c *citer) feed(text string, say func(text string, cites []map[string]any, tail bool)) {
	c.line += text
	for {
		i := strings.IndexByte(c.line, '\n')
		if i < 0 {
			return
		}
		line := c.line[:i+1]
		c.line = c.line[i+1:]
		c.end(line, say)
	}
}

func (c *citer) end(line string, say func(string, []map[string]any, bool)) {
	if strings.TrimSpace(line) == "" {
		c.held += line
		return
	}
	line, c.held = c.held+line, ""
	say(line, citesOf(line, c.hits), false)
}

// flush says what is left: a line not ended, and the whitespace held,
// which goes on the block said last (tail).
func (c *citer) flush(say func(string, []map[string]any, bool)) {
	line := c.line
	c.line = ""
	if strings.TrimSpace(line) != "" {
		c.end(line, say)
		return
	}
	if rest := c.held + line; rest != "" {
		c.held = ""
		say(rest, nil, true)
	}
}

// citesOf are the citations of the pages line names, in the order it
// names them, each once.
func citesOf(line string, hits []Hit) []map[string]any {
	type at struct {
		i int
		h Hit
	}
	var found []at
	seen := map[string]bool{}
	for _, h := range hits {
		u := strings.TrimSuffix(h.URL, "/")
		if u == "" || seen[u] {
			continue
		}
		if i := urlIn(line, u); i >= 0 {
			seen[u] = true
			found = append(found, at{i, h})
		}
	}
	if len(found) == 0 {
		return nil
	}
	// in the order the line names them
	for i := 1; i < len(found); i++ {
		for j := i; j > 0 && found[j].i < found[j-1].i; j-- {
			found[j], found[j-1] = found[j-1], found[j]
		}
	}
	cites := make([]map[string]any, 0, len(found))
	for _, f := range found {
		cites = append(cites, map[string]any{"type": "web_search_result_location", "url": f.h.URL, "title": f.h.Title,
			"encrypted_index": "", "cited_text": firstRunes(strings.TrimSpace(f.h.Cited), citedTextMost)})
	}
	return cites
}

// urlIn is where line names the address u (its trailing slash cut), the
// whole of it: what follows, past a slash and the punctuation that ends a
// sentence or markdown's emphasis, is the end, a space, a bracket, a quote
// or a character outside ASCII (Chinese punctuation). -1 when it doesn't.
func urlIn(line, u string) int {
	for from := 0; ; {
		i := strings.Index(line[from:], u)
		if i < 0 {
			return -1
		}
		i += from
		rest := line[i+len(u):]
		if urlEnds(strings.TrimPrefix(rest, "/")) {
			return i
		}
		from = i + 1
	}
}

func urlEnds(rest string) bool {
	rest = strings.TrimLeft(rest, ".,;:!?*_")
	return rest == "" || rest[0] >= 0x80 || strings.IndexByte(" \t\r\n)]}>\"'`|", rest[0]) >= 0
}
