package codexcat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// Codex before 0.145 loads no catalog an entry of which lacks
// supports_reasoning_summaries, and sends the effort and asks for the
// thinking summary only for a model whose entry says true (#1450): every
// entry says it, true for a model with effort levels.
func TestCodexCatalogReasoningSummaries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	var got struct {
		Models []map[string]any `json:"models"`
	}
	json.Unmarshal(Catalog([]catalog.Model{
		{ID: "deepseek/deepseek-v4", Name: "v4", Efforts: []string{"high", "max"}},
		{ID: "fake/m1", Name: "m1"},
	}), &got)
	if len(got.Models) != 2 {
		t.Fatalf("%v", got)
	}
	if got.Models[0]["supports_reasoning_summaries"] != true {
		t.Errorf("a model with efforts: %v", got.Models[0]["supports_reasoning_summaries"])
	}
	if v, ok := got.Models[1]["supports_reasoning_summaries"]; !ok || v != false {
		t.Errorf("a model without efforts: %v %v", v, ok)
	}
}

// Keep carries over what the user put in by hand, Codex's own entries too,
// and a true of supports_reasoning_summaries where uni-switch says false; of
// the rest uni-switch owns, nothing.
func TestKeepUserKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"models":[
		{"slug":"gpt-5.5","display_name":"GPT-5.5","priority":3,"visibility":"list","context_window":272000,"supports_reasoning_summaries":true}]}`), 0o644)
	ms := []catalog.Model{
		{ID: "fake/m1", Name: "m1"},
		{ID: "codex/gpt-5.5", Name: "GPT-5.5 · Codex", Efforts: []string{"low", "high"}},
	}
	b := Catalog(ms)
	if got := Keep(nil, b); string(got) != string(b) {
		t.Error("no file: changed")
	}
	if got := Keep([]byte("{not json"), b); string(got) != string(b) {
		t.Error("a file that doesn't parse: changed")
	}
	if got := Keep(b, b); string(got) != string(b) {
		t.Error("uni-switch's own file: changed")
	}

	cur := `{"models":[
	 {"slug":"fake/m1","supports_reasoning_summaries":true,"x_note":{"by":"me"},"max_output_tokens":128000,
	  "context_window":1,"auto_review_model_override":"old/one","shell_type":"local"},
	 {"slug":"codex/gpt-5.5","supports_reasoning_summaries":false,"upgrade":{"model":"gpt-6"},"x_mine":1},
	 {"slug":"gone/model","x_note":"left"}]}`
	var got struct {
		Models []map[string]any `json:"models"`
	}
	d := json.NewDecoder(strings.NewReader(string(Keep([]byte(cur), b))))
	d.UseNumber()
	if err := d.Decode(&got); err != nil || len(got.Models) != 2 {
		t.Fatalf("%v %v", err, got)
	}
	m1, own := got.Models[0], got.Models[1]
	if m1["supports_reasoning_summaries"] != true || m1["max_output_tokens"] != json.Number("128000") {
		t.Errorf("user's keys: %v %v", m1["supports_reasoning_summaries"], m1["max_output_tokens"])
	}
	if n, _ := m1["x_note"].(map[string]any); n["by"] != "me" {
		t.Errorf("user's object: %v", m1["x_note"])
	}
	if _, ok := m1["context_window"]; ok {
		t.Error("a window uni-switch left out came back")
	}
	if _, ok := m1["auto_review_model_override"]; ok {
		t.Error("an auto-review model uni-switch took out came back")
	}
	if m1["shell_type"] != "unified_exec" {
		t.Errorf("a key uni-switch writes: %v", m1["shell_type"])
	}
	// Codex's own entry: its cache's value is uni-switch's; a key of the user's stays
	if own["supports_reasoning_summaries"] != true || own["x_mine"] != json.Number("1") {
		t.Errorf("own entry: %v %v", own["supports_reasoning_summaries"], own["x_mine"])
	}
	if _, ok := own["upgrade"]; ok {
		t.Error("upgrade prompt came back")
	}
}
