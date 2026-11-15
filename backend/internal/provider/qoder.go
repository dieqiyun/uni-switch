package provider

// PLUGIN-SERVED (see AGENTS.md): Qoder ("qoder") and Qoder CN ("qoder-cn")
// are retired built-in subscriptions served by their plugin,
// the retired opencode-qoder-auth, moved or not. Their built-in
// sign-in, models, requests and usage are gone; what is left here reads
// the built-in accounts in logins.json for the move (migrate_qoder.go).
// A fix belongs in the plugin (github.com/dieqiyun/uni-switch,
// packages/qoder), with the movers' min raised in
// internal/provider/migrate_qoder.go.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/qoder"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// qoderMu keeps a Qoder account's saved pairs still while the move reads
// them. Credentials are decoded into private values on every read.
var qoderMu sync.Mutex

// Qoder's two sites are two subscriptions: "qoder" (qoder.com) and "qoder-cn"
// (qoder.cn), whose accounts exist only there. Each keeps its own accounts in
// logins.json under its id, and each credential names its site.
var qoderAgents = []string{qoder.ProviderKey, QoderCNID}

// QoderCNID is the Qoder CN subscription's id, the same as Qoder CN's agent.
const QoderCNID = qoder.CNProviderKey

func qoderWho(c qoder.Credential) string { return firstNonEmpty(c.Email, c.UID) }

func qoderSaved(l savedLogin) (qoder.Credential, bool) {
	var c qoder.Credential
	err := json.Unmarshal(l.Auth, &c)
	if l.Agent == qoder.CNProviderKey {
		c.Site = qoder.CNProviderKey // the account's list says its site, whatever the blob does
	}
	return c, err == nil && c.UID != "" && c.Token != ""
}

// migrateQoder moves the old single-account file exactly once. A newer
// logins.json entry takes precedence; failed writes leave the old file intact.
// With nothing to move it takes no lock, so listing accounts doesn't wait
// behind a refresh.
func migrateQoder() error {
	if settings.AccountDiscoveryOff() {
		return nil
	}
	path := filepath.Join(filepath.Dir(Path()), "qoder.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	qoderMu.Lock()
	defer qoderMu.Unlock()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var c qoder.Credential
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}
	if c.UID == "" || c.Token == "" {
		return fmt.Errorf("Qoder: unreadable legacy sign-in")
	}
	if c.MachineID == "" {
		c.MachineID = qoder.NewMachineID()
	}
	loginsMu.Lock()
	defer loginsMu.Unlock()
	ls := readLogins()
	found := false
	for _, l := range ls {
		if l.Agent == "qoder" {
			old, ok := qoderSaved(l)
			if ok && old.UID == c.UID {
				found = true
				break
			}
		}
	}
	if !found {
		auth, err := json.Marshal(c)
		if err != nil {
			return err
		}
		ls = append(ls, savedLogin{Agent: "qoder", User: qoderWho(c), Auth: auth, On: true, Seen: time.Now().UTC()})
		if err := writeLogins(ls); err != nil {
			return err
		}
	}
	return os.Remove(path)
}

func qoderLoginsOf(agent string) []sideLogin {
	if agent == qoder.ProviderKey && migrateQoder() != nil {
		return nil
	}
	ls := sideLogins(agent, "", func(l savedLogin) bool { _, ok := qoderSaved(l); return ok })
	for i := range ls {
		ls[i].Lapsed = ls[i].saved.Lapsed // a refused refresh shows on the account
	}
	return ls
}
