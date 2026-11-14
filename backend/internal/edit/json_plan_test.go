package edit

import (
	"strings"
	"testing"
)

func TestPatchJSONPreservesInputAndUnrelatedSettings(t *testing.T) {
	before := []byte("{\n // keep this comment\n \"theme\": \"dark\",\n \"model\": {\"provider\":\"uni-switch\",\"id\":\"old\"}\n}\n")
	original := string(before)
	after, err := PatchJSON(before, []KV{{Path: "model.provider", Value: "native"}}, []string{"model.id"})
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != original {
		t.Fatal("planning changed the input snapshot")
	}
	if !strings.Contains(string(after), "// keep this comment") || !strings.Contains(string(after), `"theme": "dark"`) || !strings.Contains(string(after), `"provider":"native"`) || strings.Contains(string(after), `"id"`) {
		t.Fatalf("unexpected plan: %s", after)
	}
}
