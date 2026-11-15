package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// agDrawer is the image model the tests' Antigravity accounts list.
const agDrawer = "gemini-3.1-flash-image"

// drawnPNG is Code Assist's reply with one image drawn.
const drawnPNG = `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"inlineData":{"mimeType":"image/png","data":"iVBORw0KGgo="}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}}`

func drawOn(t *testing.T, s *Server) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/images/generations",
		strings.NewReader(`{"model":"antigravity/`+agDrawer+`","prompt":"a cat"}`)))
	return rec.Code, rec.Body.String()
}
