package gateway

import "testing"

func TestWithoutFields(t *testing.T) {
	in := []byte(`{"model":"gpt-5","enable_thinking":true,"max_tokens":12345678901234}`)
	if got := string(withoutFields(in, "enable_thinking")); got != `{"max_tokens":12345678901234,"model":"gpt-5"}` {
		t.Fatal(got)
	}
	plain := []byte(`{"model":"x"}`)
	if got := withoutFields(plain, "enable_thinking"); string(got) != string(plain) {
		t.Fatal(string(got))
	}
}
