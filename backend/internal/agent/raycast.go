package agent

// Raycast AI takes models of one's own as Custom Providers (Raycast Pro, on
// Mac and Windows; lx_78654 on Discord), from ~/.config/raycast/ai/
// providers.yaml in the user's folder on both (manual.raycast.com/ai/
// custom-providers):
//
//	providers:
//	  - id: uni-switch
//	    name: uni-switch
//	    base_url: http://127.0.0.1:3425/v1   # OpenAI compatible, without /chat/completions
//	    api_keys:
//	      uni-switch: uni-switch-raycast
//	    models:
//	      - id: <provider>/<model>
//	        name: …
//	        context: …
//	        abilities:
//	          temperature: {supported: true}
//	          vision: {supported: …}
//	          system_message: {supported: true}
//	          tools: {supported: true}
//	          reasoning_effort: {supported: …}
//
// Raycast watches the file and picks a change up without a restart. The
// model is picked in Raycast's own model picker (Quick AI, AI Chat, AI
// Commands), not in a file, so what uni-switch sets is whether its models are in
// that picker: one entry of the providers list, id "uni-switch". The user's other
// entries, the top-level tools block and comments stay, and so does a key
// uni-switch doesn't write in its own entry (include_user_email,
// additional_parameters, a model's description). Raycast's User-Agent isn't
// known, so its requests are told apart by their key.

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// raycastApp is Raycast's macOS bundle.
var raycastApp = desktopApp{id: "com.raycast.macos", names: []string{"Raycast.app"}}

func raycast(home string) *Agent {
	// ~/.config in the user's folder on both systems, whatever
	// XDG_CONFIG_HOME says: the manual names that path
	dir := filepath.Join(home, ".config", "raycast")
	path := filepath.Join(dir, "ai", "providers.yaml")
	write := func(on bool) error {
		if !on {
			return raycastUnwire(path)
		}
		return raycastWire(path, raycastProviderYAML(gateway.URL()))
	}
	return &Agent{
		ID: "raycast", Name: "Raycast", Icon: "raycast", Aliases: []string{"raycast-ai"},
		Dir: dir, Path: path,
		// the app on a Mac; its config folder elsewhere, which Raycast for
		// Windows makes at Reveal Providers Config. No command: Raycast
		// has none
		detect: func() bool { return macBundle(raycastApp) || isDir(dir) },
		Sync: func() error {
			if !raycastWired(path) {
				return nil
			}
			return write(true)
		},
		// its picker lists the models picked for it (#1508)
		ListsModels: true,
		Fields: []Field{{
			Key: "provider", Label: "provider",
			Get: func() string {
				if raycastWired(path) {
					return uniSwitchID
				}
				return ""
			},
			Set: func(v string) error { return write(v != "") },
			Options: func(map[string]string) []Option {
				return []Option{{Value: uniSwitchID, Label: "uni-switch", Icon: "uni-switch", Note: "every uni-switch model in Raycast AI's model picker (Raycast Pro)"}}
			},
		}},
	}
}

// raycastSupported is one of a model's abilities in providers.yaml.
type raycastSupported struct {
	Supported bool `yaml:"supported"`
}

type raycastAbilities struct {
	Temperature     raycastSupported `yaml:"temperature"`
	Vision          raycastSupported `yaml:"vision"`
	SystemMessage   raycastSupported `yaml:"system_message"`
	Tools           raycastSupported `yaml:"tools"`
	ReasoningEffort raycastSupported `yaml:"reasoning_effort"`
}

type raycastModel struct {
	ID        string           `yaml:"id"`
	Name      string           `yaml:"name"`
	Context   int              `yaml:"context,omitempty"`
	Abilities raycastAbilities `yaml:"abilities"`
}

type raycastProvider struct {
	ID      string            `yaml:"id"`
	Name    string            `yaml:"name"`
	BaseURL string            `yaml:"base_url"`
	APIKeys map[string]string `yaml:"api_keys"`
	Models  []raycastModel    `yaml:"models"`
}

// raycastProviderYAML is uni-switch's entry in providers.yaml for a gateway at
// gw. Every ability is said, as Raycast assumes tools and vision off and
// temperature on when one isn't: tools on, as every model uni-switch serves an
// agent takes them and Raycast's AI Extensions need them; vision on a model
// that takes images; temperature off on one that refuses it; reasoning
// effort on a model with effort levels.
func raycastProviderYAML(gw string) raycastProvider {
	ms := []raycastModel{}
	for _, m := range sharedModels("raycast") {
		ms = append(ms, raycastModel{ID: m.ID, Name: cmp.Or(m.Name, m.ID), Context: m.Context, Abilities: raycastAbilities{
			Temperature:     raycastSupported{m.Temperature == nil || *m.Temperature},
			Vision:          raycastSupported{m.Images},
			SystemMessage:   raycastSupported{true},
			Tools:           raycastSupported{true},
			ReasoningEffort: raycastSupported{len(m.Efforts) > 0},
		}})
	}
	return raycastProvider{ID: uniSwitchID, Name: "uni-switch", BaseURL: gw + "/v1",
		APIKeys: map[string]string{uniSwitchID: agentKeyAt("raycast", gw)}, Models: ms}
}

// raycastWired says whether providers.yaml has uni-switch's entry.
func raycastWired(path string) bool {
	b, err := edit.Read(path)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return false
	}
	var doc struct {
		Providers []struct {
			ID any `yaml:"id"`
		} `yaml:"providers"`
	}
	if yaml.Unmarshal(b, &doc) != nil {
		return false
	}
	return slices.ContainsFunc(doc.Providers, func(p struct {
		ID any `yaml:"id"`
	}) bool {
		return p.ID == uniSwitchID
	})
}

// raycastIndex is the place of uni-switch's entry in the providers list, -1
// when it has none.
func raycastIndex(seq *yaml.Node) int {
	for i, it := range seq.Content {
		if it.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(it.Content); j += 2 {
			if it.Content[j].Value == "id" && it.Content[j+1].Value == uniSwitchID {
				return i
			}
		}
	}
	return -1
}

// raycastWire puts uni-switch's entry into providers.yaml, made when there is
// none; an entry there already keeps what uni-switch doesn't write in it. The
// file is written only when the entry says something else.
func raycastWire(path string, p raycastProvider) error {
	var want yaml.Node
	if err := want.Encode(p); err != nil {
		return err
	}
	b, err := edit.Read(path)
	if err != nil {
		return err
	}
	// a file that isn't there or holds nothing yet, or one without a list
	missing := len(bytes.TrimSpace(b)) == 0
	var bad error
	if !missing {
		err = edit.EditYAMLNode(path, "providers", func(n *yaml.Node) (bool, bool) {
			switch {
			case n == nil:
				missing = true
				return false, false
			case n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == ""):
				*n = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			case n.Kind != yaml.SequenceNode:
				bad = fmt.Errorf("%s: providers is not a list", path)
				return false, false
			}
			i := raycastIndex(n)
			if i < 0 {
				n.Content = append(n.Content, &want)
				return true, false
			}
			have := n.Content[i]
			merged := mergeYAMLKept(have, &want)
			if sameYAMLNode(have, merged) {
				return false, false
			}
			n.Content[i] = merged
			return true, false
		})
	}
	if err != nil || bad != nil {
		return cmp.Or(err, bad)
	}
	if missing {
		return edit.SetYAML(path, edit.KV{Path: "providers", Value: []raycastProvider{p}})
	}
	return nil
}

// raycastUnwire takes uni-switch's entry out of providers.yaml, the list with it
// when it was the only one, and the file when nothing is left in it.
func raycastUnwire(path string) error {
	if !raycastWired(path) {
		return nil
	}
	err := edit.EditYAMLNode(path, "providers", func(n *yaml.Node) (bool, bool) {
		if n == nil || n.Kind != yaml.SequenceNode {
			return false, false
		}
		i := raycastIndex(n)
		if i < 0 {
			return false, false
		}
		n.Content = slices.Delete(n.Content, i, i+1)
		return true, len(n.Content) == 0
	})
	if err != nil {
		return err
	}
	// what deleting the list leaves of a file with nothing else: {}
	var rest map[string]any
	if b, err := os.ReadFile(path); err == nil && yaml.Unmarshal(b, &rest) == nil && len(rest) == 0 && !bytes.Contains(b, []byte("#")) {
		return os.Remove(path)
	}
	return nil
}

// mergeYAMLKept is want with each key of have that want lacks, mappings in
// both merged alike and the models list matched by id: a key uni-switch doesn't
// write, at any depth, is the user's and stays with its comments, while a
// model gone from the catalog leaves the list.
func mergeYAMLKept(have, want *yaml.Node) *yaml.Node {
	if have.Kind != yaml.MappingNode || want.Kind != yaml.MappingNode {
		return want
	}
	out := *want
	out.Content = slices.Clone(want.Content)
	out.HeadComment, out.LineComment, out.FootComment = have.HeadComment, have.LineComment, have.FootComment
	for j := 0; j+1 < len(have.Content); j += 2 {
		k, hv := have.Content[j], have.Content[j+1]
		i := -1
		for x := 0; x+1 < len(out.Content); x += 2 {
			if out.Content[x].Value == k.Value {
				i = x
				break
			}
		}
		if i < 0 {
			out.Content = append(out.Content, k, hv)
			continue
		}
		wk := *out.Content[i]
		wk.HeadComment, wk.LineComment, wk.FootComment = k.HeadComment, k.LineComment, k.FootComment
		out.Content[i] = &wk
		wv := out.Content[i+1]
		switch {
		case k.Value == "models" && hv.Kind == yaml.SequenceNode && wv.Kind == yaml.SequenceNode:
			ms := *wv
			ms.Content = slices.Clone(wv.Content)
			for x, m := range ms.Content {
				if h := yamlItemByID(hv, yamlID(m)); h != nil {
					ms.Content[x] = mergeYAMLKept(h, m)
				}
			}
			out.Content[i+1] = &ms
		case hv.Kind == yaml.MappingNode && wv.Kind == yaml.MappingNode:
			out.Content[i+1] = mergeYAMLKept(hv, wv)
		}
	}
	return &out
}

// yamlID is a mapping's id, "" for none.
func yamlID(n *yaml.Node) string {
	if n.Kind != yaml.MappingNode {
		return ""
	}
	for j := 0; j+1 < len(n.Content); j += 2 {
		if n.Content[j].Value == "id" {
			return n.Content[j+1].Value
		}
	}
	return ""
}

// yamlItemByID is the mapping of seq whose id is id, nil for none.
func yamlItemByID(seq *yaml.Node, id string) *yaml.Node {
	if id == "" {
		return nil
	}
	for _, it := range seq.Content {
		if yamlID(it) == id {
			return it
		}
	}
	return nil
}

// sameYAMLNode says whether two nodes hold the same values, whatever their
// style and comments.
func sameYAMLNode(a, b *yaml.Node) bool {
	var x, y any
	if a.Decode(&x) != nil || b.Decode(&y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
