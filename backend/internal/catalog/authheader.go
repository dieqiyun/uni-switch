package catalog

import (
	"bytes"
	"context"
	"net/http"
)

// Which header a key goes in, for a vendor that takes one of the two alone
// (provider.Provider.AuthHeader): "" sends both.
const (
	AuthXAPIKey = "x-api-key"
	AuthBearer  = "bearer"
)

// OneAuthOrder is the order the two headers are tried in alone when a
// vendor refuses both together: Anthropic's own x-api-key first.
var OneAuthOrder = []string{AuthXAPIKey, AuthBearer}

// BothAuthRefused is a 401 whose body says x-api-key and Authorization
// can't both be sent (Aloha on Discord: "鉴权 header， x-api-key 和
// Authorization 不可以同时存在"), in Chinese or English.
func BothAuthRefused(status int, body []byte) bool {
	if status != http.StatusUnauthorized {
		return false
	}
	b := bytes.ToLower(body)
	if !bytes.Contains(b, []byte("x-api-key")) || !bytes.Contains(b, []byte("authorization")) {
		return false
	}
	for _, s := range []string{
		"同时", "并存", "共存", "只能使用一个", "只能选择一个", "只能有一个",
		"cannot both", "can't both", "can not both", "cannot be both", "not both",
		"together", "at the same time", "simultaneous", "mutually exclusive",
		"only one of", "only one auth", "only one header", "both present", "both be present", "both provided", "both be provided",
	} {
		if bytes.Contains(b, []byte(s)) {
			return true
		}
	}
	return false
}

// OneAuth leaves h with the key in the one header only names: x-api-key
// alone, or Authorization alone.
func OneAuth(h http.Header, only string) {
	switch only {
	case AuthXAPIKey:
		h.Del("Authorization")
	case AuthBearer:
		h.Del("x-api-key")
	}
}

type listAuthKey struct{}

type listAuth struct {
	only    string
	learned func(string)
}

// WithListAuth has a model list asked under ctx send its key in the one
// header only names ("" both), and, sending both, ask again with each
// alone when the vendor refuses the two together, telling learned the one
// it took.
func WithListAuth(ctx context.Context, only string, learned func(string)) context.Context {
	return context.WithValue(ctx, listAuthKey{}, listAuth{only, learned})
}

func listAuthOf(ctx context.Context) listAuth {
	a, _ := ctx.Value(listAuthKey{}).(listAuth)
	return a
}
