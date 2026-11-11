package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
)

func toolsPost(h http.Handler, body string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, ToolsMCPPath, strings.NewReader(body))
	r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:3425"
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const toolsList = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`

func toolsCall(name, args string) string {
	return `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
}
