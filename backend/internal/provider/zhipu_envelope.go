package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Zhipu's and Z.ai's /api/v1 — the GLM Coding Plan's Responses API, which
// their Codex pages give (docs.bigmodel.cn/cn/coding-plan/tool/codex) — and
// their /api/monitor answer a refusal 200, in the platform's envelope:
//
//	{"code":1000,"msg":"身份验证失败。","success":false}
//
// where their OpenAI and Anthropic endpoints answer the same key 401
// {"error":{"code":"1000","message":"身份验证失败。"}}. Taken as it came, a
// key refused everywhere tested green on the Responses row (@xanahopper on
// X), and Codex was handed the envelope as a reply. ZhipuEnveloped turns
// it into the refusal it is, at the status Zhipu's error codes are given
// (docs.bigmodel.cn/cn/faq/api-code).

// zhipuCodeStatus is the HTTP status Zhipu gives a business error code;
// a code it lists none for, or a 500 of the platform's, is a 502.
func zhipuCodeStatus(code int) int {
	switch {
	case code >= 1000 && code <= 1005:
		return http.StatusUnauthorized
	case code == 1113, code >= 1302 && code <= 1399:
		return http.StatusTooManyRequests
	case code == 1220:
		return http.StatusForbidden
	case code >= 1210 && code <= 1229, code == 1261, code == 1301:
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

// zhipuEnvelope reads b as the platform's refusal: its code and message,
// and whether it is one ({"success":false} with a code or a message).
func zhipuEnvelope(b []byte) (code int, msg string, ok bool) {
	var v struct {
		Success *bool           `json:"success"`
		Code    json.RawMessage `json:"code"`
		Msg     string          `json:"msg"`
	}
	if json.Unmarshal(b, &v) != nil || v.Success == nil || *v.Success {
		return 0, "", false
	}
	var n json.Number
	if json.Unmarshal(v.Code, &n) == nil {
		code, _ = strconv.Atoi(n.String())
	} else if s := ""; json.Unmarshal(v.Code, &s) == nil {
		code, _ = strconv.Atoi(s)
	}
	msg = strings.TrimSpace(v.Msg)
	return code, msg, code != 0 || msg != ""
}

// ZhipuKeyRefused says whether an envelope's code is Zhipu refusing the
// key itself (1000–1005: not taken, missing, expired, needs a second
// factor), not the request.
func ZhipuKeyRefused(code int) bool { return code >= 1000 && code <= 1005 }

// ZhipuEnveloped is res, or — when it is a 2xx from Zhipu or Z.ai holding
// the platform's refusal envelope — the refusal at its status, with the
// body as their OpenAI API words one. A stream, a compressed body and
// every other host's reply pass as they came.
func ZhipuEnveloped(req *http.Request, res *http.Response) *http.Response {
	if req == nil || res == nil || res.Body == nil || res.StatusCode < 200 || res.StatusCode >= 300 || !zhipuHost(req.URL.Hostname()) {
		return res
	}
	ct := strings.ToLower(res.Header.Get("Content-Type"))
	if !strings.Contains(ct, "json") || strings.Contains(ct, "event-stream") || res.Header.Get("Content-Encoding") != "" {
		return res
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 64<<10+1))
	if err != nil || len(b) > 64<<10 {
		// a refusal is small: a body this long is a reply, left to be read
		// on as it came, and a read that failed to its reader
		res.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(b), res.Body), res.Body}
		return res
	}
	res.Body.Close()
	set := func(nb []byte) *http.Response {
		res.Body = io.NopCloser(bytes.NewReader(nb))
		res.ContentLength = int64(len(nb))
		res.Header.Set("Content-Length", strconv.Itoa(len(nb)))
		return res
	}
	code, msg, ok := zhipuEnvelope(b)
	if !ok {
		res.Body = io.NopCloser(bytes.NewReader(b))
		return res
	}
	if msg == "" {
		msg = "request failed"
	}
	e := map[string]any{"message": msg}
	if code != 0 {
		e["code"] = strconv.Itoa(code)
	}
	nb, _ := json.Marshal(map[string]any{"error": e})
	res.StatusCode = zhipuCodeStatus(code)
	res.Status = strconv.Itoa(res.StatusCode) + " " + http.StatusText(res.StatusCode)
	return set(nb)
}
