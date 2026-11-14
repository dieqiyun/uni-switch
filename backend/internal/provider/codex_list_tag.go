package provider

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Codex's list's tag is asked for on every GET of its models and every
// backend reply that carries X-Models-Etag (gateway.modelsEtag), and
// each time it built the whole catalog: 3.5 MB a call while a request
// held the catalog, 22 MB without, with 8 providers of 40 models. A live
// heap profile of a uni-switch at rest had 2.1 GB of it in 67 minutes, the
// Codex app asking for its models. It is kept now while nothing it is
// built from changed.

// codexListTagFor is how long a kept tag is used at most: what the stamp
// can't see (an account another app signed in to, held in a keychain; a
// model retired by its date) reaches Codex's list within it.
var codexListTagFor = time.Minute

// codexListSettled is how old a stamped file's time must be to tell a
// later write by: two writes of one size inside a tick of a coarse clock
// leave the same time and size (filememo's settled).
const codexListSettled = 2 * time.Second

// codexListStamp is what the tag is built from, read without building it:
// the writes this uni-switch made or was told of (Changed, a plugin's change,
// catalog.Touched or Reset, filememo.Forget), the environment, and the
// files another process writes — uni-switch's providers, settings and
// models.dev catalog, and Codex's config (the effort new sessions start
// at), sign-in and model cache.
type codexListStamp struct {
	gen, fgen, env uint64
	files          [6]rootStamp
}

func codexListStampNow() codexListStamp {
	held.Lock()
	gen := held.gen
	held.Unlock()
	home := catalog.CodexHome()
	return codexListStamp{gen: gen, fgen: filememo.Gen(), env: environSum(), files: [6]rootStamp{
		stampOf(Path()), stampOf(settings.Path()), stampOf(catalog.CachePath()),
		stampOf(filepath.Join(home, "config.toml")), stampOf(filepath.Join(home, "auth.json")),
		stampOf(catalog.CodexModelsCache()),
	}}
}

// settled reports whether every file's time is old enough that a write
// after it would stamp another.
func (s codexListStamp) settled(now time.Time) bool {
	for _, f := range s.files {
		if f.ok && now.Sub(f.mod) < codexListSettled {
			return false
		}
	}
	return true
}

var codexListTags struct {
	sync.Mutex
	stamp codexListStamp
	at    time.Time
	of    map[string]string
}

// codexListTag is buildCodexListTag's, built again only when its stamp
// moved or codexListTagFor went by.
func codexListTag(kind string) string {
	st := codexListStampNow()
	now := time.Now()
	codexListTags.Lock()
	if codexListTags.stamp != st || now.Sub(codexListTags.at) >= codexListTagFor {
		codexListTags.stamp, codexListTags.at, codexListTags.of = st, now, nil
	}
	if tag, ok := codexListTags.of[kind]; ok {
		codexListTags.Unlock()
		return tag
	}
	codexListTags.Unlock()
	// built from what is there after st was taken: never older than st
	tag := buildCodexListTag(kind)
	if !st.settled(now) {
		return tag
	}
	codexListTags.Lock()
	if codexListTags.stamp == st {
		if codexListTags.of == nil {
			codexListTags.of = map[string]string{}
		}
		codexListTags.of[kind] = tag
	}
	codexListTags.Unlock()
	return tag
}
