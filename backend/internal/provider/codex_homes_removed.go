package provider

// An account Codex is signed in to in another of its folders (codexHomeLogins)
// was read again from there at every look, so Remove refused it and said to
// run `CODEX_HOME=… codex logout` (charliepu on Discord, Windows: the
// folder was C:\Users\…\.codex-db-backup-20260803, a backup no Codex runs
// in). Removed now, the folder's sign-in is set aside here, uni-switch's own
// record: that folder is listed no more while it holds that sign-in. Its
// files are left as they are. A sign-in made there again is another token,
// which nothing here names: it is listed again with no step of the user's.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/dieqiyun/uni-switch/backend/internal/lastgood"
)

// codexAsideMu guards the record's file.
var codexAsideMu sync.Mutex

// codexAsidePath is the record: a Codex folder to the hash of the sign-in
// in it that the user removed from uni-switch (codexAsideKey).
func codexAsidePath() string {
	return filepath.Join(filepath.Dir(Path()), "codex-homes-removed.json")
}

// codexAsideKey names the sign-in auth holds without keeping it: its
// refresh token, or its access token when it has none. "" for none.
func codexAsideKey(auth []byte) string {
	var a codexAuth
	if json.Unmarshal(auth, &a) != nil {
		return ""
	}
	if a.Tokens.RefreshToken != "" {
		return codexGoneHash(a.Tokens.RefreshToken)
	}
	if a.Tokens.AccessToken != "" {
		return codexGoneHash(a.Tokens.AccessToken)
	}
	return ""
}

// readCodexAside reads the record. None yet is empty; one that can't be
// read or doesn't parse is an error: what it sets aside is unknown.
func readCodexAside() (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(codexAsidePath())
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// setCodexAside records that the sign-in auth in the Codex folder dir was
// removed from uni-switch.
func setCodexAside(dir string, auth []byte) error {
	key := codexAsideKey(auth)
	if key == "" {
		return nil
	}
	codexAsideMu.Lock()
	defer codexAsideMu.Unlock()
	m, err := readCodexAside()
	if err != nil {
		// a record that doesn't parse is kept aside, then begun again
		m = map[string]string{}
	}
	m[filepath.Clean(dir)] = key
	out, err := json.MarshalIndent(m, "", "  ")
	if err == nil {
		err = lastgood.KeepUnparsed(codexAsidePath(), lastgood.Into[map[string]string])
	}
	if err == nil {
		err = writePrivate(codexAsidePath(), append(out, '\n'))
	}
	return err
}

// codexSetAside says whether l, read from its folder l.Home, is the sign-in
// the user removed from uni-switch there, by the record m (readCodexAside).
func codexSetAside(l savedLogin, m map[string]string) bool {
	key := codexAsideKey(l.Auth)
	return key != "" && m[filepath.Clean(l.Home)] == key
}
