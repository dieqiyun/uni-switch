package agent

import (
	"os"
	"strings"
)

func dotenvValue(path, key string) string {
	b, e := os.ReadFile(path)
	if e != nil {
		return ""
	}
	value := ""
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) > 1 && (v[0] == '\'' || v[0] == '"') {
			if i := strings.IndexByte(v[1:], v[0]); i >= 0 {
				v = v[1 : i+1]
			}
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		value = v
	}
	return value
}
