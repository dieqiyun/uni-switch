package modelmeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const CatalogURL = "https://models.dev/api.json"
const LabsURL = "https://models.dev/models.json"
const cacheLimit = 32 << 20

type Cache struct {
	Directory, Fallback string
	Client              *http.Client
	CatalogURL, LabsURL string
}
type Registry struct {
	SchemaVersion int    `json:"schemaVersion"`
	Version       int64  `json:"version"`
	VerifiedAt    string `json:"verifiedAt"`
	Entries       []any  `json:"entries"`
}
type Status struct {
	Registry        Registry `json:"registry"`
	CheckedAt       *int64   `json:"checkedAt"`
	Updated         bool     `json:"updated"`
	UpdateAvailable bool     `json:"updateAvailable"`
	Message         string   `json:"message"`
	Source          string   `json:"source"`
	SHA256          string   `json:"sha256"`
	Reference       string   `json:"reference"`
	Policy          string   `json:"policy"`
	Models          int      `json:"models"`
	Stale           bool     `json:"stale"`
}
type stamp struct {
	CheckedAt int64 `json:"checkedAt"`
}

var updateMu sync.Mutex

func limitedFile(path string) ([]byte, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, cacheLimit+1))
	if e != nil || len(b) > cacheLimit {
		return nil, fmt.Errorf("catalog size invalid")
	}
	return b, nil
}
func (c Cache) Load() (*Catalog, string, []byte) {
	labs, _ := limitedFile(filepath.Join(c.Directory, "models-labs.json"))
	paths := []string{filepath.Join(c.Directory, "models-dev.json"), c.Fallback}
	for _, path := range paths {
		if path == "" || c.Directory == "" && path == "models-dev.json" {
			continue
		}
		if b, e := limitedFile(path); e == nil {
			if cat, e := ReadCatalog(b, labs); e == nil && len(cat.providers) > 0 {
				return cat, path, b
			}
		}
	}
	return EmptyCatalog(), "", nil
}
func (c Cache) Status() Status {
	cat, path, b := c.Load()
	s := Status{Registry: Registry{SchemaVersion: 1, Entries: []any{}, VerifiedAt: "尚无缓存"}, UpdateAvailable: true, Source: CatalogURL, Reference: Reference, Policy: Policy, Models: len(cat.aggregates), Stale: true, Message: "没有可用目录缓存；保留实时供应商资料和用户设置，未知字段不推断支持"}
	if len(b) > 0 {
		hash := sha256.Sum256(b)
		s.SHA256 = hex.EncodeToString(hash[:])
		if info, e := os.Stat(path); e == nil {
			s.Registry.Version = info.ModTime().Unix()
			s.Registry.VerifiedAt = info.ModTime().UTC().Format("2006-01-02")
			s.Stale = time.Since(info.ModTime()) >= 24*time.Hour
		}
		s.Message = "使用 models.dev 缓存补全；供应商自己的目录、实时资料和用户设置按固定规则合并"
	}
	if b, e := limitedFile(filepath.Join(c.Directory, "models-checked.json")); e == nil {
		var t stamp
		if json.Unmarshal(b, &t) == nil && t.CheckedAt > 0 {
			s.CheckedAt = &t.CheckedAt
		}
	}
	return s
}
func atomicSave(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".modelmeta-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp, path)
}
func (c Cache) download(ctx context.Context, url string) ([]byte, error) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "uni-switch-modelmeta/1")
	// This client has no cookie jar, supplier credentials, or Authorization headers.
	res, e := client.Do(req)
	if e != nil {
		return nil, errors.New("无法下载模型目录，已保留缓存")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("模型目录 HTTP %d，已保留缓存", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, cacheLimit+1))
	if e != nil || len(b) > cacheLimit {
		return nil, errors.New("模型目录过大或下载中断，已保留缓存")
	}
	return b, nil
}
func (c Cache) Update(ctx context.Context, force bool) (Status, error) {
	updateMu.Lock()
	defer updateMu.Unlock()
	old := c.Status()
	if !force && !old.Stale {
		return old, nil
	}
	if c.Directory == "" {
		return old, errors.New("模型目录缓存路径未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	checked, _ := json.Marshal(stamp{CheckedAt: time.Now().Unix()})
	if e := atomicSave(filepath.Join(c.Directory, "models-checked.json"), checked); e != nil {
		return old, errors.New("无法保存模型目录检查状态")
	}
	api, labsURL := c.CatalogURL, c.LabsURL
	if api == "" {
		api = CatalogURL
	}
	if labsURL == "" {
		labsURL = LabsURL
	}
	b, e := c.download(ctx, api)
	if e != nil {
		return c.Status(), e
	}
	parsed, e := ReadCatalog(b, nil)
	if e != nil || len(parsed.providers) == 0 {
		return c.Status(), errors.New("模型目录格式无效，已保留缓存")
	}
	// The lab file supplements provider-listed rows. A failed lab refresh leaves
	// its previous valid cache; it does not discard a successful main refresh.
	labs, labErr := c.download(ctx, labsURL)
	if labErr == nil {
		var rows map[string]catalogRow
		if json.Unmarshal(labs, &rows) == nil && len(rows) > 0 {
			if e = atomicSave(filepath.Join(c.Directory, "models-labs.json"), labs); e != nil {
				return c.Status(), errors.New("无法保存实验室目录")
			}
		}
	}
	if e = atomicSave(filepath.Join(c.Directory, "models-dev.json"), b); e != nil {
		return c.Status(), errors.New("无法保存模型目录，已保留缓存")
	}
	next := c.Status()
	next.Updated = next.SHA256 != old.SHA256
	return next, nil
}
