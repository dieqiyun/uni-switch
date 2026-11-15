package settings

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// The catalog hears what the user said a model is the same as
// (ModelSameAs) for every spec it gives of a model it doesn't list by its
// own id: the window, the reply limit, the levels and images (pinmmis on
// X: a relay's model renamed "ds.4.1(WB)" had none of deepseek-v4.1-flash's).
func init() { catalog.UserSame = sameAsBare }

// sameMemo is the bare-id index of the ModelSameAs map last read, kept
// while Shared hands out that same map.
var sameMemo struct {
	sync.Mutex
	of    map[string]string
	index map[string]string
}

// sameAsBare is the model the user said a model of this bare id is the
// same as, at any provider: the first, by provider id, of those saying so.
func sameAsBare(bare string) (string, bool) {
	same := Shared().ModelSameAs
	if len(same) == 0 {
		return "", false
	}
	sameMemo.Lock()
	defer sameMemo.Unlock()
	if sameMemo.of == nil || reflect.ValueOf(sameMemo.of).UnsafePointer() != reflect.ValueOf(same).UnsafePointer() {
		index := map[string]string{}
		for _, k := range slices.Sorted(maps.Keys(same)) {
			_, model, ok := strings.Cut(k, "/")
			v := strings.TrimSpace(same[k])
			if !ok || model == "" || model == "*" || v == "" {
				continue
			}
			b := strings.ToLower(model)
			if i := strings.LastIndexByte(b, '/'); i >= 0 {
				b = b[i+1:]
			}
			if _, had := index[b]; !had {
				index[b] = v
			}
		}
		sameMemo.of, sameMemo.index = same, index
	}
	v, ok := sameMemo.index[bare]
	return v, ok
}
