package sessions

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/lastgood"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Deleting a gateway session (lc on Discord: a Claude Code session seen only
// through uni-switch's gateway was listed "read only"). Such a session has no
// file of the agent's on this computer, so what uni-switch can delete is what it
// keeps of it: its row in the session lists, and the conversation text it
// recorded, if any. The text is moved into uni-switch's trash like a native
// session's files, under trash/sessions/gateway/, and the row is hidden up
// to the last request it had; Restore moves the text back and shows the row
// again. The usage ledger is never touched, so Usage's totals stay as they
// were, and a request under the same session after the delete lists it
// again. Trashed text still expires with the recording's 7 days, and
// clearing the recorded conversations clears it too: the recording promises
// that much.

// gatewayTrash is the trash folder (and key prefix) of deleted gateway
// sessions; no agent has this id.
const gatewayTrash = "gateway"

var hiddenMu sync.Mutex

func gatewayHiddenPath() string { return filepath.Join(settings.Dir(), "gateway-sessions-hidden.json") }

type hiddenGateway struct {
	Agent string    `json:"agent"`
	ID    string    `json:"id"`
	Last  time.Time `json:"last"`
}

func readHidden() []hiddenGateway {
	var out []hiddenGateway
	b, err := os.ReadFile(gatewayHiddenPath())
	if err == nil {
		json.Unmarshal(b, &out)
	}
	return out
}

func writeHidden(list []hiddenGateway) error {
	if err := keepUnreadHidden(); err != nil {
		return err
	}
	if len(list) == 0 {
		if err := os.Remove(gatewayHiddenPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(settings.Dir(), 0o700); err != nil {
		return err
	}
	tmp := gatewayHiddenPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, gatewayHiddenPath())
}

// keepUnreadHidden copies a gateway-sessions-hidden.json that doesn't parse
// aside before it is written over (or taken away): it holds the deletions
// uni-switch made before, and a read that came back empty is unknown, not
// nothing. A copy that can't be made stops the write.
func keepUnreadHidden() error {
	return lastgood.KeepUnparsed(gatewayHiddenPath(), lastgood.Into[[]hiddenGateway])
}

// GatewayHidden says whether a gateway session was deleted: true when it
// was, and had no request after its last one then.
func GatewayHidden() func(agent, id string, last time.Time) bool {
	hiddenMu.Lock()
	list := readHidden()
	hiddenMu.Unlock()
	m := map[string]time.Time{}
	for _, h := range list {
		m[h.Agent+"|"+h.ID] = h.Last
	}
	return func(agent, id string, last time.Time) bool {
		at, ok := m[agent+"|"+id]
		return ok && !last.After(at)
	}
}

// GatewaySizes is what conversation text uni-switch keeps of each gateway
// session, in bytes, read in one walk of the store.
func GatewaySizes() func(agent, id string) int64 {
	by := map[string]int64{}
	gatewayMu.Lock()
	root := gatewayDir()
	gatewayMu.Unlock()
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			by[filepath.Base(filepath.Dir(p))] += fi.Size()
		}
		return nil
	})
	return func(agent, id string) int64 { return by[gatewayIdentity(agent, id)] }
}

// DeleteGateway deletes a gateway session as described above: last is its
// last request as listed. One that had a request in the last minute may
// still be going on, and is left (ErrActive).
func DeleteGateway(agent, id string, title string, last time.Time) (Trashed, error) {
	if agent == "" || id == "" {
		return Trashed{}, errors.New("no such session")
	}
	now := time.Now()
	if now.Sub(last) < ActiveWindow {
		return Trashed{}, ErrActive
	}
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	hash := gatewayIdentity(agent, id)
	buckets, _ := filepath.Glob(filepath.Join(gatewayDir(), "*", hash))
	t := Trashed{Agent: agent, ID: id, Title: title, Last: last, Deleted: now, Gateway: true, Items: []moved{}}
	name := now.UTC().Format("20060102T150405.000000000") + "-" + hash[:16]
	dir := filepath.Join(TrashDir(), gatewayTrash, name)
	t.Key = gatewayTrash + "/" + name
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o700); err != nil {
		return Trashed{}, err
	}
	for _, p := range buckets {
		// named by its date, which its expiry is read from
		m := moved{From: p, Name: filepath.Base(filepath.Dir(p))}
		t.Size += sizeOf(p)
		if err := move(p, filepath.Join(dir, "files", m.Name)); err != nil {
			for _, back := range t.Items {
				move(filepath.Join(dir, "files", back.Name), back.From)
			}
			os.RemoveAll(dir)
			return Trashed{}, err
		}
		t.Items = append(t.Items, m)
		writeManifest(dir, t)
	}
	if err := writeManifest(dir, t); err != nil {
		return Trashed{}, err
	}
	hiddenMu.Lock()
	defer hiddenMu.Unlock()
	list := readHidden()
	kept := list[:0]
	for _, h := range list {
		if h.Agent != agent || h.ID != id {
			kept = append(kept, h)
		}
	}
	if err := writeHidden(append(kept, hiddenGateway{Agent: agent, ID: id, Last: last})); err != nil {
		// the text goes back, and the session stays as it was
		for _, back := range t.Items {
			move(filepath.Join(dir, "files", back.Name), back.From)
		}
		os.RemoveAll(dir)
		return Trashed{}, err
	}
	return t, nil
}

// restoreGateway moves a deleted gateway session's text back, what of it
// hasn't expired or been cleared since, and lists the session again.
func restoreGateway(dir string, t Trashed) error {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	var back []moved
	for _, m := range t.Items {
		if _, err := os.Lstat(filepath.Join(dir, "files", m.Name)); err != nil {
			continue
		}
		if _, err := os.Lstat(m.From); err == nil {
			return errors.New("the session was recorded again that day; nothing was restored")
		}
		back = append(back, m)
	}
	for i, m := range back {
		if err := move(filepath.Join(dir, "files", m.Name), m.From); err != nil {
			for _, b := range back[:i] {
				move(b.From, filepath.Join(dir, "files", b.Name))
			}
			return err
		}
	}
	hiddenMu.Lock()
	defer hiddenMu.Unlock()
	list := readHidden()
	kept := list[:0]
	for _, h := range list {
		if h.Agent != t.Agent || h.ID != t.ID {
			kept = append(kept, h)
		}
	}
	return writeHidden(kept)
}

// gatewayTrashFiles are the trashed gateway text folders, each with the
// date it was recorded on ("" when its name isn't one).
func gatewayTrashFiles() (paths, dates []string) {
	paths, _ = filepath.Glob(filepath.Join(TrashDir(), gatewayTrash, "*", "files", "*"))
	for _, p := range paths {
		d := filepath.Base(p)
		if _, err := time.Parse(time.DateOnly, d); err != nil {
			d = ""
		}
		dates = append(dates, d)
	}
	return paths, dates
}

// clearGatewayTrash erases the text of deleted gateway sessions, all of it,
// or (expired) what is past the recording's retention. Called under
// gatewayMu. The trash notes are kept, so Restore still lists the session
// again.
func clearGatewayTrash(expired func(date string) bool) error {
	paths, dates := gatewayTrashFiles()
	var errs []error
	for i, p := range paths {
		if expired == nil || (dates[i] != "" && expired(dates[i])) {
			if err := os.RemoveAll(p); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func isGatewayKey(key string) bool { return strings.HasPrefix(key, gatewayTrash+"/") }
