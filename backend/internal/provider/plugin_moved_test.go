package provider

import (
	"strings"
)

func listed(agent string) string {
	var out []string
	for _, l := range Logins(agent) {
		u := l.User
		if l.Active {
			u = "*" + u
		}
		if l.Own {
			u += " (own)"
		}
		out = append(out, u)
	}
	return strings.Join(out, ",")
}
