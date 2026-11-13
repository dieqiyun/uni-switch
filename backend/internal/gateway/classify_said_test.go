package gateway

// What the classifier said, as it said it (#4598): a group's trace told
// which intent it named, and none when it named no rule's — but not the
// answer itself, so "said none" was the same whether the classifier took
// its own none-of-the-kinds option, named a kind it wasn't sure enough
// of, or answered a number no rule could take. These tests read the
// said of the trace's classified, as the page's JSON has it.

import (
	"encoding/json"
)

// saidOf is the said of a classified, as the trace's JSON spells it: the
// field read here by what the field is called, so a change that drops it
// fails these tests rather than the build.
func saidOf(c *Classified) string {
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	s, _ := m["said"].(string)
	return s
}
