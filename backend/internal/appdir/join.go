package appdir

import (
	"path/filepath"
	"sync"
)

// joins are the paths Join has made, by what they were made of. uni-switch
// names its files from these folders on every read of them, and a look
// at the providers reads them for each provider and model: the joins
// were 70 MB of the 1.4 GB twenty window polls allocated.
var joins struct {
	sync.Mutex
	by map[[3]string]string
}

// Join is filepath.Join(dir, name), made once for each pair and kept
// while the folders it is asked for stay the same.
func Join(dir, name string) string { return JoinFile(dir, name, "") }

// JoinFile is filepath.Join(dir, name+ext), made once for each, as Join:
// a provider's list is models/<id>.json.
func JoinFile(dir, name, ext string) string {
	k := [3]string{dir, name, ext}
	joins.Lock()
	defer joins.Unlock()
	if p, ok := joins.by[k]; ok {
		return p
	}
	// a bound, for folders that change under a test, not a uni-switch
	if joins.by == nil || len(joins.by) >= 4096 {
		joins.by = map[[3]string]string{}
	}
	p := filepath.Join(dir, name+ext)
	joins.by[k] = p
	return p
}
