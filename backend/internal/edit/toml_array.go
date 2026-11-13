package edit

import (
	"fmt"
	"slices"

	"github.com/pelletier/go-toml/v2/unstable"
)

// SetTOMLArrayTable replaces one named array entry and its child tables, or
// appends it. block is a complete array-table declaration, including its header.
// Duplicate matches, invalid input and replacements for another entry are errors.
func SetTOMLArrayTable(path, name, key, value, block string) error {
	return editTOMLArrayTable(path, name, key, value, block)
}

// DelTOMLArrayTable removes just the matching entry and its child tables.
func DelTOMLArrayTable(path, name, key, value string) error {
	return editTOMLArrayTable(path, name, key, value, "")
}

func editTOMLArrayTable(path, name, key, value, block string) error {
	raw, err := Read(path)
	if err != nil {
		return err
	}
	if err := validateTOML(raw); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	lines := splitLines(string(raw))
	doc, err := parseTOMLFile(lines)
	if err != nil {
		return err
	}
	spans, err := namedTOMLArray(doc, name, key, value, false)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var replacement []string
	if block != "" {
		if err := validateTOML([]byte(block)); err != nil {
			return fmt.Errorf("replacement: %w", err)
		}
		replacement = splitLines(block)
		newDoc, err := parseTOMLFile(replacement)
		if err != nil {
			return err
		}
		matches, err := namedTOMLArray(newDoc, name, key, value, false)
		if err != nil || len(matches) == 0 || len(newDoc.root.keys) > 0 {
			return fmt.Errorf("replacement must declare one %s entry with %s = %q", name, key, value)
		}
		// All headers must belong to this entry, including child array tables.
		want := tomlHeaderParts(name)
		for _, table := range newDoc.tables {
			parts := tomlHeaderParts(table.name)
			if !tomlDescendant(parts, want) && table.from != matches[0].from {
				return fmt.Errorf("replacement contains another table")
			}
		}
		replacement = trimBlank(replacement)
	}
	if len(spans) == 0 {
		if block == "" {
			return nil
		}
		out := trimBlank(lines)
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, replacement...)
		return writeTOML(path, out)
	}
	var out []string
	at := 0
	for i, span := range spans {
		out = append(out, lines[at:span.from]...)
		if i == 0 {
			out = append(out, replacement...)
		}
		at = span.to
	}
	out = append(out, lines[at:]...)
	return writeTOML(path, out)
}

// SetTOMLArrayTables replaces every entry of the array table name whose key
// is value, with its child tables, by blocks: where the first of them was,
// else at the end of the file. Entries with another value stay as they are,
// and no blocks removes the matching ones. Each block is one complete entry,
// its header included, with key = value; anything else in one is an error.
func SetTOMLArrayTables(path, name, key, value string, blocks []string) error {
	raw, err := Read(path)
	if err != nil {
		return err
	}
	if err := validateTOML(raw); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	lines := splitLines(string(raw))
	doc, err := parseTOMLFile(lines)
	if err != nil {
		return err
	}
	spans, err := namedTOMLArray(doc, name, key, value, true)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var replacement []string
	want := tomlHeaderParts(name)
	for _, block := range blocks {
		if err := validateTOML([]byte(block)); err != nil {
			return fmt.Errorf("replacement: %w", err)
		}
		bl := splitLines(block)
		newDoc, err := parseTOMLFile(bl)
		if err != nil {
			return err
		}
		matches, err := namedTOMLArray(newDoc, name, key, value, true)
		if err != nil || len(matches) == 0 || len(newDoc.root.keys) > 0 || !newDoc.tables[0].array ||
			!slices.Equal(tomlHeaderParts(newDoc.tables[0].name), want) {
			return fmt.Errorf("replacement must declare one %s entry with %s = %q", name, key, value)
		}
		for i, table := range newDoc.tables {
			parts := tomlHeaderParts(table.name)
			if i > 0 && (slices.Equal(parts, want) || !tomlDescendant(parts, want)) {
				return fmt.Errorf("replacement contains another table")
			}
		}
		if len(replacement) > 0 {
			replacement = append(replacement, "")
		}
		replacement = append(replacement, trimBlank(bl)...)
	}
	if len(spans) == 0 {
		if len(replacement) == 0 {
			return nil
		}
		out := trimBlank(lines)
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, replacement...)
		return writeTOML(path, append(out, ""))
	}
	var out []string
	at := 0
	for i, span := range spans {
		gap := lines[at:span.from]
		// The blank lines between two entries replaced went with them, or
		// every sync would add another.
		if i == 0 || len(trimBlank(gap)) > 0 {
			out = append(out, gap...)
		}
		if i == 0 {
			if len(replacement) == 0 {
				out = trimBlank(out)
			}
			out = append(out, replacement...)
		}
		at = span.to
	}
	out = append(out, lines[at:]...)
	if joinLinesLike(out, string(raw)) == string(raw) {
		return nil
	}
	return writeTOML(path, out)
}

type tomlArraySpan struct{ from, to int }

// namedTOMLArray are the line spans of the entries of the array table name
// whose key is value, each followed by its child tables'; more than one such
// entry is an error unless all is set.
func namedTOMLArray(doc tomlFile, name, key, value string, all bool) ([]tomlArraySpan, error) {
	want := tomlHeaderParts(name)
	if len(want) == 0 {
		return nil, fmt.Errorf("invalid table name %q", name)
	}
	// An inline array cannot safely be treated as independent table blocks.
	for _, kv := range doc.root.keys {
		if kv.name == name {
			return nil, fmt.Errorf("%s is an inline value, not an array table", name)
		}
	}
	var spans []tomlArraySpan
	for i, table := range doc.tables {
		if !slices.Equal(tomlHeaderParts(table.name), want) {
			continue
		}
		if !table.array {
			return nil, fmt.Errorf("%s is not an array table", name)
		}
		matched := false
		for _, kv := range table.keys {
			if kv.name == key && kv.scalar && kv.value == value {
				matched = true
			}
		}
		if !matched {
			continue
		}
		if len(spans) > 0 && !all {
			return nil, fmt.Errorf("multiple %s entries have %s = %q", name, key, value)
		}
		spans = append(spans, tomlArraySpan{table.from, tomlContentEnd(table)})
		for j := i + 1; j < len(doc.tables); j++ {
			parts := tomlHeaderParts(doc.tables[j].name)
			if slices.Equal(parts, want) {
				break
			}
			if tomlDescendant(parts, want) {
				spans = append(spans, tomlArraySpan{doc.tables[j].from, tomlContentEnd(doc.tables[j])})
			}
		}
	}
	return spans, nil
}

func tomlContentEnd(table tomlTableSpan) int {
	if len(table.keys) > 0 {
		return table.keys[len(table.keys)-1].to
	}
	return table.from + 1
}

func tomlDescendant(parts, parent []string) bool {
	return len(parts) > len(parent) && slices.Equal(parts[:len(parent)], parent)
}

func tomlHeaderParts(name string) []string {
	var p unstable.Parser
	p.Reset([]byte("[" + name + "]"))
	if !p.NextExpression() || p.Expression().Kind != unstable.Table {
		return nil
	}
	var parts []string
	for keys := p.Expression().Key(); keys.Next(); {
		parts = append(parts, string(keys.Node().Data))
	}
	if p.NextExpression() || p.Error() != nil {
		return nil
	}
	return parts
}
