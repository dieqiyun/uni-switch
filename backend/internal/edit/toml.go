package edit

// A span editor, following Magpie's edit/toml design: only owned expressions
// are changed. Parsing identifies keys; original comments and unrelated bytes
// are never reconstructed by a whole-document marshal.
import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

type tomlSpan struct {
	path                 []string
	from, to, value, end int
	table                bool
}

func TOML(text string) (map[string]any, error) {
	v := map[string]any{}
	if toml.Unmarshal([]byte(text), &v) != nil {
		return nil, fail("invalid_toml", "TOML 语法无效，请修复后再应用")
	}
	return v, nil
}
func prefix(a, b []string) bool {
	if len(a) > len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func keyText(p []string) string {
	s := make([]string, len(p))
	for i, k := range p {
		s[i] = k
		if k == "" || strings.IndexFunc(k, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
		}) >= 0 {
			s[i] = strconv.Quote(k)
		}
	}
	return strings.Join(s, ".")
}
func literal(v any) string {
	switch x := v.(type) {
	case string:
		b, _ := json.Marshal(x)
		return string(b)
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return string(x)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := []string{}
		for _, k := range keys {
			out = append(out, keyText([]string{k})+" = "+literal(x[k]))
		}
		return "{ " + strings.Join(out, ", ") + " }"
	case []any:
		out := []string{}
		for _, e := range x {
			out = append(out, literal(e))
		}
		return "[" + strings.Join(out, ", ") + "]"
	case []string:
		out := []string{}
		for _, e := range x {
			out = append(out, strconv.Quote(e))
		}
		return "[" + strings.Join(out, ", ") + "]"
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}
func lineStart(s string, i int) int { return strings.LastIndex(s[:i], "\n") + 1 }
func lineEnd(s string, i int) int {
	if n := strings.IndexByte(s[i:], '\n'); n >= 0 {
		return i + n + 1
	}
	return len(s)
}

// TOML strings can contain brackets, equals signs, comments and apparent table
// headers. Track them while finding the end of an already parsed value.
func valueEnd(s string, start int) int {
	depth := 0
	quote := byte(0)
	triple := false
	end := start
	for i := start; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if quote == '"' && c == '\\' {
				i++
				end = i + 1
				continue
			}
			if c == quote {
				if triple {
					if i+2 < len(s) && s[i+1] == quote && s[i+2] == quote {
						i += 2
						quote = 0
					}
				} else {
					quote = 0
				}
			}
			end = i + 1
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			triple = i+2 < len(s) && s[i+1] == c && s[i+2] == c
			if triple {
				i += 2
			}
			end = i + 1
			continue
		}
		if c == '#' {
			if depth == 0 {
				return end
			}
			i = lineEnd(s, i) - 1
			continue
		}
		if c == '[' || c == '{' {
			depth++
		}
		if c == ']' || c == '}' {
			depth--
		}
		if c == '\n' && depth == 0 {
			return end
		}
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			end = i + 1
		}
	}
	return end
}
func spans(s string) ([]tomlSpan, error) {
	if _, e := TOML(s); e != nil {
		return nil, e
	}
	p := unstable.Parser{}
	p.Reset([]byte(s))
	var table []string
	out := []tomlSpan{}
	for p.NextExpression() {
		n := p.Expression()
		if n.Kind == unstable.Comment {
			continue
		}
		parts := []string{}
		start, last := 0, 0
		for it := n.Key(); it.Next(); {
			k := it.Node()
			if len(parts) == 0 {
				start = int(k.Raw.Offset)
			}
			last = int(k.Raw.Offset + k.Raw.Length)
			parts = append(parts, string(k.Data))
		}
		if n.Kind == unstable.Table || n.Kind == unstable.ArrayTable {
			table = parts
			out = append(out, tomlSpan{path: append([]string{}, parts...), from: lineStart(s, start), to: lineEnd(s, last), table: true})
			continue
		}
		eq := last + strings.IndexByte(s[last:], '=') + 1
		for eq < len(s) && (s[eq] == ' ' || s[eq] == '\t') {
			eq++
		}
		end := valueEnd(s, eq)
		out = append(out, tomlSpan{path: append(append([]string{}, table...), parts...), from: lineStart(s, start), to: lineEnd(s, end), value: eq, end: end})
	}
	if p.Error() != nil {
		return nil, fail("invalid_toml", "TOML 格式无效")
	}
	return out, nil
}
func TOMLSet(s string, path []string, value any, remove bool) (string, error) {
	shape, err := TOML(s)
	if err != nil {
		return "", err
	}
	var existing any = shape
	for _, k := range path {
		m, _ := existing.(map[string]any)
		existing = m[k]
	}
	old, _ := json.Marshal(existing)
	next, _ := json.Marshal(value)
	if remove && existing == nil || !remove && string(old) == string(next) {
		return s, nil
	}
	ss, e := spans(s)
	if e != nil {
		return "", e
	}
	if len(path) == 0 {
		return "", fail("invalid_shape", "TOML 键无效")
	}
	_, isMap := value.(map[string]any)
	// Dotted edits inside an existing inline parent keep its siblings and its
	// outer spelling. Adding an explicit table beside it would redefine a key.
	for _, sp := range ss {
		if sp.table || len(sp.path) >= len(path) || !prefix(sp.path, path) {
			continue
		}
		var parent any = shape
		for _, k := range sp.path {
			m, _ := parent.(map[string]any)
			parent = m[k]
		}
		m, ok := parent.(map[string]any)
		if !ok {
			return "", fail("invalid_shape", "客户端配置父级必须是 TOML 表")
		}
		for _, k := range path[len(sp.path) : len(path)-1] {
			if m[k] == nil {
				m[k] = map[string]any{}
			}
			m, ok = m[k].(map[string]any)
			if !ok {
				return "", fail("invalid_shape", "客户端配置父级必须是 TOML 表")
			}
		}
		last := path[len(path)-1]
		if remove {
			delete(m, last)
		} else {
			m[last] = value
		}
		s = s[:sp.value] + literal(parent) + s[sp.end:]
		if _, e := TOML(s); e != nil {
			return "", e
		}
		return s, nil
	}
	for _, sp := range ss {
		if !sp.table && len(sp.path) == len(path) && prefix(path, sp.path) && !isMap {
			if remove {
				s = s[:sp.from] + s[sp.to:]
			} else {
				s = s[:sp.value] + literal(value) + s[sp.end:]
			}
			if remove {
				return cleanTOML(s, path)
			}
			return s, nil
		}
	}
	// Replacing an owned table also removes its explicit descendant tables.
	for i := len(ss) - 1; i >= 0; i-- {
		sp := ss[i]
		if prefix(path, sp.path) {
			s = s[:sp.from] + s[sp.to:]
		}
	}
	if remove {
		return cleanTOML(s, path)
	}
	ss, e = spans(s)
	if e != nil {
		return "", e
	}
	if isMap {
		keys := []string{}
		m := value.(map[string]any)
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		block := "[" + keyText(path) + "]\n"
		for _, k := range keys {
			block += keyText([]string{k}) + " = " + literal(m[k]) + "\n"
		}
		if s != "" && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += block
	} else {
		at := 0
		parent := []string{}
		chosen := -1
		for i, sp := range ss {
			if sp.table && len(sp.path) < len(path) && len(sp.path) > len(parent) && prefix(sp.path, path) {
				parent = sp.path
				chosen = i
				at = sp.to
			}
		}
		if chosen >= 0 {
			for _, sp := range ss[chosen+1:] {
				if sp.table {
					break
				}
				at = sp.to
			}
		}
		if len(parent) == 0 {
			at = 0
			for _, sp := range ss {
				if sp.table {
					break
				}
				at = sp.to
			}
		}
		line := keyText(path[len(parent):]) + " = " + literal(value) + "\n"
		if at > 0 && s[at-1] != '\n' {
			line = "\n" + line
		}
		s = s[:at] + line + s[at:]
	}
	if _, e = TOML(s); e != nil {
		return "", e
	}
	return s, nil
}
func cleanTOML(s string, path []string) (string, error) {
	ss, e := spans(s)
	if e != nil {
		return "", e
	}
	for i := len(ss) - 1; i >= 0; i-- {
		sp := ss[i]
		if !sp.table || !prefix(sp.path, path) {
			continue
		}
		used := false
		for _, other := range ss {
			if !other.table && prefix(sp.path, other.path) {
				used = true
				break
			}
		}
		if !used {
			s = s[:sp.from] + s[sp.to:]
		}
	}
	return s, nil
}
