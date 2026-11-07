// Package accounts owns subscription credentials independently of native client
// configuration. Only redacted summaries cross management IPC.
package accounts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Account struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	User              string `json:"user"`
	Enabled           bool   `json:"enabled"`
	Project           string `json:"project,omitempty"`
	Access            string `json:"access,omitempty"`
	Refresh           string `json:"refresh,omitempty"`
	Expiry            int64  `json:"expiry,omitempty"`
	AccountID         string `json:"accountId,omitempty"`
	APIBase           string `json:"apiBase,omitempty"`
	Session           string `json:"session,omitempty"`
	SessionExpiry     int64  `json:"sessionExpiry,omitempty"`
	ConfigDir         string `json:"configDir,omitempty"`
	OAuthClientID     string `json:"oauthClientId,omitempty"`
	OAuthClientSecret string `json:"oauthClientSecret,omitempty"`
}
type Summary struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	User    string `json:"user"`
	Enabled bool   `json:"enabled"`
	Project string `json:"project,omitempty"`
	Expiry  int64  `json:"expiry,omitempty"`
	Active  bool   `json:"active"`
}
type disk struct {
	Accounts []Account         `json:"accounts"`
	Active   map[string]string `json:"active"`
}
type Manager struct {
	mu        sync.Mutex
	Directory string
	Client    *http.Client
	Endpoints Endpoints
	flows     map[string]*flow
}

var managers = struct {
	sync.Mutex
	values map[string]*Manager
}{values: map[string]*Manager{}}

func Open(directory string) *Manager {
	managers.Lock()
	defer managers.Unlock()
	if m := managers.values[directory]; m != nil {
		return m
	}
	m := &Manager{Directory: directory, Client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Endpoints: Defaults(), flows: map[string]*flow{}}
	managers.values[directory] = m
	return m
}
func random() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func (m *Manager) load() (disk, error) {
	d := disk{Accounts: []Account{}, Active: map[string]string{}}
	raw, e := edit.Read(filepath.Join(m.Directory, "subscriptions", "accounts.json"))
	if e != nil {
		return d, e
	}
	if raw != nil {
		if json.Unmarshal([]byte(*raw), &d) != nil || d.Active == nil {
			return d, errors.New("订阅账号资料损坏，请保留数据目录")
		}
	}
	return d, nil
}
func (m *Manager) save(d disk) error {
	if !filepath.IsAbs(m.Directory) {
		return errors.New("账号目录必须是绝对路径")
	}
	if e := edit.PrivateDirectory(filepath.Join(m.Directory, "subscriptions")); e != nil {
		return e
	}
	b, e := json.Marshal(d)
	if e != nil {
		return e
	}
	s := string(b)
	return edit.WriteAtomic(filepath.Join(m.Directory, "subscriptions", "accounts.json"), &s)
}
func (m *Manager) List() ([]Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, e := m.load()
	if e != nil {
		return nil, e
	}
	out := []Summary{}
	for _, a := range d.Accounts {
		out = append(out, Summary{a.ID, a.Kind, a.Name, a.User, a.Enabled, a.Project, a.Expiry, d.Active[a.Kind] == a.ID})
	}
	return out, nil
}
func (m *Manager) store(a Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, e := m.load()
	if e != nil {
		return e
	}
	for i, old := range d.Accounts {
		if old.Kind == a.Kind && old.User == a.User {
			a.ID = old.ID
			d.Accounts[i] = a
			return m.save(d)
		}
	}
	d.Accounts = append(d.Accounts, a)
	d.Active[a.Kind] = a.ID
	return m.save(d)
}
func (m *Manager) Edit(id, operation, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, e := m.load()
	if e != nil {
		return e
	}
	for i, a := range d.Accounts {
		if a.ID != id {
			continue
		}
		switch operation {
		case "switch":
			if !a.Enabled {
				return errors.New("请先启用账号")
			}
			d.Active[a.Kind] = id
		case "enable":
			d.Accounts[i].Enabled = value == "true"
		case "project":
			d.Accounts[i].Project = value
		case "delete":
			d.Accounts = append(d.Accounts[:i], d.Accounts[i+1:]...)
			if d.Active[a.Kind] == id {
				delete(d.Active, a.Kind)
			}
		default:
			return errors.New("未知账号操作")
		}
		return m.save(d)
	}
	return errors.New("账号不存在")
}

// One persistent core owns refresh. The lock covers read -> network -> atomic
// save so rotating refresh tokens cannot be overwritten by another request.
func (m *Manager) Resolve(ctx context.Context, id string, force bool) (Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, e := m.load()
	if e != nil {
		return Account{}, e
	}
	for i, a := range d.Accounts {
		if a.ID != id && !(id == a.Kind && d.Active[a.Kind] == a.ID) {
			continue
		}
		if !a.Enabled {
			return Account{}, errors.New("订阅账号已停用")
		}
		if a.Kind == "claude" {
			return a, nil
		}
		if a.Kind == "copilot" {
			e = m.copilot(ctx, &a, force)
		} else if force || a.Expiry <= time.Now().Add(time.Minute).Unix() {
			e = m.refresh(ctx, &a)
		}
		if e != nil {
			return Account{}, e
		}
		if (a.Kind == "google" || a.Kind == "antigravity") && a.Project == "" {
			e = m.googleProject(ctx, &a)
			if e != nil {
				return Account{}, e
			}
		}
		d.Accounts[i] = a
		if e = m.save(d); e != nil {
			return Account{}, e
		}
		return a, nil
	}
	return Account{}, errors.New("订阅账号不存在")
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.flows {
		f.cancel()
	}
}
func CaptureBrowser(args []string) bool {
	path := os.Getenv("UNI_SWITCH_LOGIN_URL")
	if path == "" || len(args) != 1 || !validLoginURL(args[0]) {
		return false
	}
	_ = os.WriteFile(path, []byte(args[0]), 0600)
	return true
}

func CloseAll() {
	managers.Lock()
	defer managers.Unlock()
	for _, m := range managers.values {
		m.Close()
	}
}
