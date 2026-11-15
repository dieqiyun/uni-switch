package edit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/jsonc"
)

// A comment at the start of the element's line stays when the element is
// taken out, as a member's delete keeps the comment before it: VS Code's
// chatLanguageModels.json carries the user's notes beside their groups.

func TestDelJSONItemKeepsACommentBeforeTheElement(t *testing.T) {
	where := map[string]string{"vendor": "customendpoint", "name": "uni-switch"}
	uniSwitch := `{"name": "uni-switch", "vendor": "customendpoint"}`
	for _, in := range []string{
		// last element, its comment on the line with it
		"[\n  {\"name\": \"a\", \"vendor\": \"openai\"},\n  /* uni-switch's group */ " + uniSwitch + "\n]\n",
		// not the last
		"[\n  /* uni-switch's group */ " + uniSwitch + ",\n  {\"name\": \"b\", \"vendor\": \"ollama\"}\n]\n",
		// on one line, the [ on it too
		"[ {\"name\": \"a\", \"vendor\": \"openai\"}, /* uni-switch's group */ " + uniSwitch + " ]\n",
		// a block comment that ends on the element's line
		"[\n  {\"name\": \"a\", \"vendor\": \"openai\"},\n  /* uni-switch's group\n     */ " + uniSwitch + "\n]\n",
	} {
		p := tmpFile(t, "chatLanguageModels.json", in)
		if err := DelJSONItem(p, where); err != nil {
			t.Fatal(err)
		}
		b := read(t, p)
		var got []map[string]any
		if err := json.Unmarshal(jsonc.ToJSON([]byte(b)), &got); err != nil {
			t.Fatalf("%q: not valid JSONC after: %v\n%s", in, err, b)
		}
		if len(got) != strings.Count(in, "\"vendor\"")-1 {
			t.Fatalf("%q: %d elements left:\n%s", in, len(got), b)
		}
		if _, ok := GetJSONItem(p, where); ok {
			t.Fatalf("%q: the element is still there:\n%s", in, b)
		}
		if !strings.Contains(b, "/* uni-switch's group") || !strings.Contains(b, "*/") {
			t.Fatalf("%q: the comment before the element went with it:\n%s", in, b)
		}
	}
}
