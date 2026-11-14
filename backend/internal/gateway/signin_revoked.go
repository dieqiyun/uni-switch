package gateway

// A subscription account whose sign-in its vendor revoked (#1793, ysicing:
// a ChatGPT account answering 401 token_revoked, asked again on every
// request). provider/signin_revoked.go keeps which credentials are
// revoked; here the gateway tells it what the vendor said, and answers for
// a revoked one without sending it, so the routing loop passes over it
// (failureOf: failAuth) and asks the next member.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// revokedReply is the 401 a request signed with a revoked credential is
// answered with, in the words ChatGPT used, without asking the vendor.
func revokedReply(req *http.Request, why string) *http.Response {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{
		"message": why, "type": "authentication_error", "code": "token_revoked",
	}})
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	return &http.Response{
		Status: "401 Unauthorized", StatusCode: http.StatusUnauthorized,
		Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: h, Body: io.NopCloser(bytes.NewReader(b)), ContentLength: int64(len(b)), Request: req,
	}
}

// noteSignIn tells provider what the vendor said of the sign-in req went
// with: a 401 that says it is revoked marks it, a request served clears a
// mark the account had, and one that says the token expired renews it.
// The body read is put back for the caller.
func noteSignIn(ctx context.Context, a *provider.Account, req *http.Request, res *http.Response) {
	if a == nil || res == nil {
		return
	}
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		provider.ServedSignIn(a)
	case res.StatusCode == http.StatusUnauthorized && res.Body != nil:
		b, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		res.Body = readCloser{io.MultiReader(bytes.NewReader(b), res.Body), res.Body}
		switch {
		case err != nil:
		case provider.SignInRevoked(res.StatusCode, b):
			provider.NoteRevokedSignIn(ctx, a, req)
		case provider.SignInExpired(res.StatusCode, b):
			// expired while its exp was ahead: renewed for the next
			// request (#2973)
			provider.NoteExpiredSignIn(ctx, a, req)
		}
	}
}
