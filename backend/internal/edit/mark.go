package edit

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Mark, when set, is the note a line uni-switch writes into an agent's TOML,
// YAML or dotenv config gets, as a comment line of its own just above it:
// what wrote it and how to undo it, for someone reading the file after
// uni-switch is gone (Jevik_AI on X: a Codex config left pointing at a gateway
// that no longer ran). table is the TOML table or the YAML mapping's
// dotted path the key is in, "" at the top level; value is the value as
// read. "" leaves the line without a note. JSON has no comments, so
// nothing is marked there. Set by main, so a test of an agent's exact
// bytes sees none.
//
// A note goes on a line this write adds or changes, and stays on one that
// had it; a line of the user's own that uni-switch leaves as it is gets none.
// It is a line of its own, never a comment after the value: an agent that
// reads its config line by line (Grok App's providers.rs) takes the rest
// of a line for the value, but skips a line that starts with #.
var Mark func(table, key, value string) string

// MarkTag starts every note: a comment line that starts with it is
// uni-switch's, and is written again, or taken out, with the line under it.
const MarkTag = "uni-switch: "

const markComment = "# " + MarkTag

func noteOf(table, key, value string) string {
	if Mark == nil {
		return ""
	}
	return Mark(table, key, value)
}

// IsNote says whether line is one of uni-switch's notes.
func IsNote(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), markComment)
}

// withoutNotes is lines without uni-switch's notes.
func withoutNotes(lines []string) []string {
	var out []string
	for _, l := range lines {
		if !IsNote(l) {
			out = append(out, l)
		}
	}
	if len(out) == len(lines) {
		return lines
	}
	return out
}

// noted is, for each line of orig, whether a note of uni-switch's was over it
func noted(orig string) map[string]bool {
	m := map[string]bool{}
	prev := false
	for _, l := range splitLines(orig) {
		if IsNote(l) {
			prev = true
			continue
		}
		k := strings.TrimSuffix(l, "\r")
		m[k] = m[k] || prev
		prev = false
	}
	return m
}

// withNotes is lines (notes taken out) with note[i] over line i where the
// line is new or changed from orig, or had a note there.
func withNotes(lines []string, orig string, note map[int]string) []string {
	if len(note) == 0 {
		return lines
	}
	was := noted(orig)
	out := make([]string, 0, len(lines)+len(note))
	for i, l := range lines {
		k := strings.TrimSuffix(l, "\r")
		if n := note[i]; n != "" {
			if had, there := was[k]; had || !there {
				indent := k[:len(k)-len(strings.TrimLeft(k, " \t"))]
				nl := indent + markComment + n
				if strings.HasSuffix(l, "\r") {
					nl += "\r"
				}
				out = append(out, nl)
			}
		}
		out = append(out, l)
	}
	return out
}

// markTOML puts Mark's notes over lines' one-line scalar keys. A file that
// doesn't parse is left as it is, for writeTOML to refuse.
func markTOML(lines []string, orig string) []string {
	if Mark == nil {
		return lines
	}
	bare := withoutNotes(lines)
	doc, err := parseTOMLFile(bare)
	if err != nil {
		return lines
	}
	note := map[int]string{}
	mark := func(table string, keys []tomlKeySpan) {
		for _, k := range keys {
			if k.scalar && k.to == k.from+1 {
				if n := noteOf(table, k.name, k.value); n != "" {
					note[k.from] = n
				}
			}
		}
	}
	mark("", doc.root.keys)
	for _, t := range doc.tables {
		mark(t.name, t.keys)
	}
	return withNotes(bare, orig, note)
}

// MarkYAMLLines puts Mark's notes in a YAML file's lines, orig being the
// file as it was: over a key whose scalar value is on the key's own line,
// in a block mapping, at the line's indent. Should a note change what the
// file reads as, none is added.
func MarkYAMLLines(lines []string, orig string) []string {
	if Mark == nil {
		return lines
	}
	bare := withoutNotes(lines)
	root, err := parseYAMLDocument([]byte(strings.Join(bare, "\n")))
	if err != nil || root == nil {
		return lines
	}
	note := map[int]string{}
	var walk func(n *yaml.Node, table string)
	walk = func(n *yaml.Node, table string) {
		switch n.Kind {
		case yaml.DocumentNode, yaml.SequenceNode:
			for _, c := range n.Content {
				walk(c, table)
			}
		case yaml.MappingNode:
			if n.Style&yaml.FlowStyle != 0 {
				return
			}
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i], n.Content[i+1]
				if v.Kind != yaml.ScalarNode {
					walk(v, joinPath(table, k.Value))
					continue
				}
				at := k.Line - 1
				if v.Line != k.Line || v.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || at < 0 || at >= len(bare) {
					continue
				}
				if s := noteOf(table, k.Value, v.Value); s != "" {
					note[at] = s
				}
			}
		}
	}
	walk(root, "")
	out := withNotes(bare, orig, note)
	if len(out) == len(lines) && strings.Join(out, "\n") == strings.Join(lines, "\n") {
		return lines
	}
	again, err := parseYAMLDocument([]byte(strings.Join(out, "\n")))
	if err != nil || again == nil || !sameYAMLValue(root, again) {
		return bare
	}
	return out
}

// markEnv puts Mark's notes over a dotenv file's keys. Dotenv readers end
// a value at an unquoted #, but some don't, so a note never shares the
// value's line.
func markEnv(lines []string, orig string) []string {
	if Mark == nil {
		return lines
	}
	bare := withoutNotes(lines)
	note := map[int]string{}
	for i, l := range bare {
		m := envLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		v, ok := unquote(m[2])
		if !ok {
			v = m[2]
			if j := strings.IndexByte(v, '#'); j >= 0 {
				v = strings.TrimSpace(v[:j])
			}
		}
		if n := noteOf("", m[1], v); n != "" {
			note[i] = n
		}
	}
	return withNotes(bare, orig, note)
}

func joinPath(table, key string) string {
	if table == "" {
		return key
	}
	return table + "." + key
}
