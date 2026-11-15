package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

// The ids here are real: models.dev's (the local catalog of 10-29), Codex's
// own list and Bedrock's.
func TestFamilyOf(t *testing.T) {
	for _, c := range []struct {
		id, name string
		version  []int
		snapshot string
		preview  bool
	}{
		{"claude-opus-4-8", "claude-opus", []int{4, 8}, "", false},
		{"claude-opus-5-5", "claude-opus", []int{5, 5}, "", false},
		{"claude-opus-5", "claude-opus", []int{5}, "", false},
		// a dated snapshot is its alias
		{"claude-opus-4-5-20251101", "claude-opus", []int{4, 5}, "20251101", false},
		{"claude-haiku-4-5-20251001", "claude-haiku", []int{4, 5}, "20251001", false},
		{"gpt-4o-2024-08-06", "gpt-o", []int{4}, "2024-08-06", false},
		{"deepseek-r1-0528", "deepseek-r", []int{1}, "0528", false},
		{"qwen3-235b-a22b-instruct-2507", "qwen-235b-a22b-instruct", []int{3}, "2507", false},
		{"gemini-2.5-flash-preview-05-20", "gemini-flash", []int{2, 5}, "05-20", true},
		{"gemini-embedding-001", "gemini-embedding", nil, "001", false},
		{"command-r7b-12-2024", "command-r7b", nil, "12-2024", false},
		// the version mid-name
		{"claude-3-5-sonnet-20241022", "claude-sonnet", []int{3, 5}, "20241022", false},
		{"meta-llama/llama-3.3-70b-instruct", "meta-llama/llama-70b-instruct", []int{3, 3}, "", false},
		// sizes and suffixes stay in the name
		{"gpt-5.4-mini", "gpt-mini", []int{5, 4}, "", false},
		{"gpt-5.5-pro", "gpt-pro", []int{5, 5}, "", false},
		{"gpt-4.1-nano", "gpt-nano", []int{4, 1}, "", false},
		{"gpt-5.3-codex-spark", "gpt-codex-spark", []int{5, 3}, "", false},
		{"qwen3.6-27b", "qwen-27b", []int{3, 6}, "", false},
		{"qwen3.6-35b-a3b", "qwen-35b-a3b", []int{3, 6}, "", false},
		{"mistralai/mixtral-8x7b-instruct", "mistralai/mixtral-8x7b-instruct", nil, "", false},
		{"gemma-3n-e4b-it", "gemma-n-e4b-it", []int{3}, "", false},
		// gemini: flash, flash-lite and pro are three
		{"gemini-3.5-flash", "gemini-flash", []int{3, 5}, "", false},
		{"gemini-3.5-flash-lite", "gemini-flash-lite", []int{3, 5}, "", false},
		{"gemini-3.1-pro-preview", "gemini-pro", []int{3, 1}, "", true},
		{"gemini-pro-latest", "gemini-pro", nil, "", false},
		// the o-series
		{"o3", "o", []int{3}, "", false},
		{"o4-mini", "o-mini", []int{4}, "", false},
		{"o1-pro", "o-pro", []int{1}, "", false},
		// a word that ends in its version
		{"kimi-k2.6", "kimi-k", []int{2, 6}, "", false},
		{"glm-4.6v", "glm-v", []int{4, 6}, "", false},
		{"deepseek-v4-flash", "deepseek-flash", []int{4}, "", false},
		// a provider prefix stays
		{"anthropic/claude-opus-4.5", "anthropic/claude-opus", []int{4, 5}, "", false},
		{"us.anthropic.claude-sonnet-4-5-20250929-v1:0", "us.anthropic.claude-sonnet", []int{4, 5, 1, 0}, "20250929", false},
		{"Qwen/Qwen3.8-27B", "qwen/qwen-27b", []int{3, 8}, "", false},
		{"claude-sonnet-4-5[1m]", "claude-sonnet[1m", []int{4, 5}, "", false},
	} {
		f := FamilyOf(c.id)
		if f.Name != c.name || !slices.Equal(f.Version, c.version) || f.Snapshot != c.snapshot || f.Preview != c.preview {
			t.Errorf("FamilyOf(%q) = %+v, want {%s %v %q %v}", c.id, f, c.name, c.version, c.snapshot, c.preview)
		}
	}
}

// Different models never share a family.
func TestFamilyKeepsModelsApart(t *testing.T) {
	for _, pair := range [][2]string{
		{"gpt-5.4", "gpt-5.4-mini"},
		{"gpt-5.4-mini", "gpt-5.4-nano"},
		{"gpt-5.5", "gpt-5.5-pro"},
		{"gpt-4", "gpt-4o"},
		{"o3", "o3-mini"},
		{"o3", "o3-pro"},
		{"gemini-3.5-flash", "gemini-3.1-pro-preview"},
		{"gemini-3.5-flash", "gemini-3.5-flash-lite"},
		{"qwen3-8b", "qwen3-32b"},
		{"llama-3.1-8b-instruct", "llama-3.1-70b-instruct"},
		{"gpt-6.1-sol", "gpt-6.1-terra"},
		{"claude-opus-5-5", "claude-sonnet-5-5"},
		{"kimi-k2-thinking", "kimi-k2"},
		{"openai/gpt-5", "azure/gpt-5"},
		{"deepseek/deepseek-r1:free", "deepseek/deepseek-r1"},
	} {
		if a, b := FamilyOf(pair[0]).Name, FamilyOf(pair[1]).Name; a == b {
			t.Errorf("%s and %s are both %q", pair[0], pair[1], a)
		}
	}
}

func familyModels(ids ...string) []Model {
	out := make([]Model, len(ids))
	for i, id := range ids {
		out[i] = Model{ID: id}
	}
	return out
}

// Anthropic's list as models.dev had it on 10-27, release dates and all:
// Haiku 5.5, Sonnet 5.5, Opus 5.5 and Fable 5.1, in the list's order.
func TestBestInFamilyClaude(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "models.dev-2026-10-27-claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]struct {
		Models map[string]struct {
			Released string `json:"release_date"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatal(err)
	}
	var ms []Model
	for id, m := range d["anthropic"].Models {
		ms = append(ms, Model{ID: id, Released: m.Released})
	}
	// newest first, as the vendor's list has them
	sort.Slice(ms, func(i, j int) bool {
		return ms[i].Released > ms[j].Released || ms[i].Released == ms[j].Released && ms[i].ID < ms[j].ID
	})
	got := ids(BestInFamily(ms))
	want := slices.DeleteFunc(ids(ms), func(id string) bool {
		return !slices.Contains([]string{"claude-haiku-5-5", "claude-sonnet-5-5", "claude-opus-5-5", "claude-fable-5-1"}, id)
	})
	if !slices.Equal(got, want) || len(got) != 4 {
		t.Errorf("BestInFamily(anthropic) = %v, want %v", got, want)
	}
}

func TestBestInFamily(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []Model
		want []string
	}{
		{"codex", familyModels("gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"),
			[]string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-luna", "gpt-5.6-terra"}},
		// a newer one listed takes the old one's place by itself
		{"opus 5.6", familyModels("claude-opus-5-6", "claude-opus-5-5", "claude-sonnet-5-5"), []string{"claude-opus-5-6", "claude-sonnet-5-5"}},
		{"openai sizes", familyModels("gpt-5.6", "gpt-5.5", "gpt-5.4-mini", "gpt-5-mini", "gpt-4.1-nano", "gpt-5.4-nano", "gpt-5.5-pro", "gpt-5-pro"),
			[]string{"gpt-5.6", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-5.5-pro"}},
		{"o-series", familyModels("o1", "o3", "o3-mini", "o4-mini", "o1-pro", "o3-pro"), []string{"o3", "o4-mini", "o3-pro"}},
		{"gemini", familyModels("gemini-2.5-flash", "gemini-3.5-flash", "gemini-2.5-pro", "gemini-3.1-pro-preview", "gemini-3.1-flash-lite-preview", "gemini-3.1-flash-lite", "gemini-pro-latest"),
			[]string{"gemini-3.5-flash", "gemini-3.1-pro-preview", "gemini-3.1-flash-lite"}},
		// at one version, a release over its preview
		{"preview", familyModels("gemini-3-pro-image-preview", "gemini-3-pro-image"), []string{"gemini-3-pro-image"}},
		// the alias over its dated snapshots, whatever their dates say
		{"snapshots", []Model{{ID: "gpt-4o", Released: "2024-05-13"}, {ID: "gpt-4o-2024-08-06", Released: "2024-08-06"}, {ID: "gpt-4o-2024-11-20", Released: "2024-11-20"}},
			[]string{"gpt-4o"}},
		{"snapshots alone", []Model{{ID: "mistral-medium-2505", Released: "2025-05-07"}, {ID: "mistral-medium-2508", Released: "2025-08-12"}}, []string{"mistral-medium-2508"}},
		{"qwen sizes", familyModels("Qwen/Qwen3.5-27B", "Qwen/Qwen3.6-27B", "Qwen/Qwen3.8-27B", "Qwen/Qwen3.6-35B-A3B", "Qwen/Qwen3.5-35B-A3B"),
			[]string{"Qwen/Qwen3.8-27B", "Qwen/Qwen3.6-35B-A3B"}},
		{"mid-name version", familyModels("claude-3-5-sonnet-20241022", "claude-sonnet-4-5"), []string{"claude-sonnet-4-5"}},
		{"bedrock", familyModels("us.anthropic.claude-sonnet-4-5-20250929-v1:0", "us.anthropic.claude-sonnet-5-5", "us.meta.llama3-1-70b-instruct-v1:0", "us.meta.llama3-3-70b-instruct-v1:0"),
			[]string{"us.anthropic.claude-sonnet-5-5", "us.meta.llama3-3-70b-instruct-v1:0"}},
		{"provider prefix", familyModels("openai/gpt-5.5", "openai/gpt-5.6", "azure/gpt-5.5"), []string{"openai/gpt-5.6", "azure/gpt-5.5"}},
		{"none", nil, nil},
	} {
		if got := ids(BestInFamily(c.in)); !slices.Equal(got, c.want) {
			t.Errorf("%s: BestInFamily = %v, want %v", c.name, got, c.want)
		}
	}
}
