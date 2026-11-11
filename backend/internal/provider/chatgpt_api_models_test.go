package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// #933: a ChatGPT API account lists, beside the models its catalog lists,
// those Codex lists for the plan, which the token runs though the catalog
// leaves them out (gpt-6-luna, added by hand, ran); one the catalog hides,
// and uni-switch's own in Codex's list, stay out.
func TestChatGPTAPIListsCodexModels(t *testing.T) {
	home := claudeHome(t)
	codex := filepath.Join(home, ".codex")
	os.MkdirAll(codex, 0o755)
	t.Setenv("CODEX_HOME", codex)
	os.WriteFile(filepath.Join(codex, "models_cache.json"), []byte(`{"models":[
		{"slug":"gpt-6-luna","display_name":"GPT-6-Luna","priority":1,"context_window":272000,"supported_reasoning_levels":[{"effort":"low"},{"effort":"xhigh"}]},
		{"slug":"gpt-5.5","display_name":"GPT-5.5 (Codex)","priority":2},
		{"slug":"gpt-hidden","display_name":"Hidden","priority":3},
		{"slug":"deepseek/deepseek-v4","display_name":"DeepSeek V4","description":"DeepSeek V4 via uni-switch","priority":4}]}`), 0o644)
	f := newFakeOpenAI(t)
	if st := signInVia(t, f, "oaiapp_1", same); st.State != "done" {
		t.Fatalf("sign-in %+v", st)
	}
	p, ok := siwcAccount()
	if !ok {
		t.Fatal("no account")
	}
	ms, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	if len(ms) != 3 || ids[0] != "gpt-5.5" || ids[1] != "gpt-5.4-mini" || ids[2] != "gpt-6-luna" {
		t.Fatalf("models %v", ids)
	}
	if ms[0].Name != "GPT-5.5" || ms[2].Provider != "openai" || len(ms[2].APIs) != 1 || ms[2].APIs[0] != string(Responses) ||
		len(ms[2].Efforts) != 2 {
		t.Errorf("models %+v", ms)
	}
}
