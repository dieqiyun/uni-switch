package access

import (
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A key held to a provider's model is held to it under the id it is
// listed by (provider.SetModelID): the key's own "<provider>/<model>"
// moves to the new id with the model, as the provider's picks and the
// routing groups' refs do, so the user's limit keeps meaning that model.
func init() { provider.ModelMoved = MoveModel }

// MoveModel gives every key's models that name from ("<provider>/<model>",
// letter case aside) to instead. A pattern ("relay/*") is the user's own
// and is left as written.
func MoveModel(from, to string) error {
	mu.Lock()
	defer mu.Unlock()
	keys, err := load()
	if err != nil {
		return err
	}
	changed := false
	for i := range keys {
		ms := keys[i].Models
		var out []string
		moved := false
		for _, m := range ms {
			if strings.EqualFold(m, from) {
				m, moved = to, true
			}
			if !containsFold(out, m) {
				out = append(out, m)
			}
		}
		if moved {
			keys[i].Models, changed = out, true
		}
	}
	if !changed {
		return nil
	}
	return save(keys)
}

func containsFold(ms []string, m string) bool {
	for _, x := range ms {
		if strings.EqualFold(x, m) {
			return true
		}
	}
	return false
}
