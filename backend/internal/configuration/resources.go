package configuration

// A resource is journaled in exactly the same way as a file, but its owner
// performs the write. In particular, a SQLite database is never replaced.
import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	_ "modernc.org/sqlite"
)

var almaEndpoint = "http://127.0.0.1:23001"
var credentialRead = readCredential
var credentialWrite = writeCredential
var clientHTTP = &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func externalFormat(format string) bool {
	return format == "newmax" || format == "alma" || format == "zed_credential" || format == "hanako_provider" || format == "hanako_chat"
}

func sameClientResource(a, b *string, format string) bool {
	if format == "alma" && a != nil && b != nil {
		var x, y Object
		if parse(*a, &x) == nil && parse(*b, &y) == nil {
			// Alma may return an encrypted credential rather than the key
			// submitted to its API. Only our newly created provider may be
			// managed (buildResource rejects an unowned name collision).
			// Its credential is write-only; all readable fields still match.
			delete(obj(x["provider"]), "apiKey")
			delete(obj(y["provider"]), "apiKey")
			return encode(x) == encode(y)
		}
	}
	if format == "hanako_provider" && a != nil && b != nil {
		var x, y Object
		if parse(*a, &x) == nil && parse(*b, &y) == nil {
			delete(x, "plugin")
			delete(y, "plugin")
			delete(x, "deleted")
			delete(y, "deleted")
			if x["sealed"] == true || y["sealed"] == true {
				delete(obj(x["provider"]), "api_key")
				delete(obj(y["provider"]), "api_key")
			}
			return encode(x) == encode(y)
		}
	}
	return agent.Same(a, b)
}

func resourceHTTP(method, endpoint, token string, body any, result any) error {
	var rd io.Reader
	if body != nil {
		rd = strings.NewReader(encode(body))
	}
	req, err := http.NewRequest(method, endpoint, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := clientHTTP.Do(req)
	if err != nil {
		return fail("client_unavailable", "客户端本地 API 无法连接，请先启动客户端再重试")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(b) > 4<<20 {
		return fail("client_api", "客户端 API 响应超过限制")
	}
	if resp.StatusCode/100 != 2 {
		return fail("client_api", "客户端拒绝了配置操作（HTTP "+resp.Status+"）")
	}
	if result != nil && len(bytes.TrimSpace(b)) > 0 {
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		if d.Decode(result) != nil {
			return fail("client_api", "客户端返回了无效配置")
		}
	}
	return nil
}

func newmaxDB(path string) (*sql.DB, error) {
	if err := agent.CheckPath(path); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fail("client_unavailable", "请先打开 NewMax 一次，退出后再配置")
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=rw&_pragma=busy_timeout(5000)&_txlock=immediate"}
	if len(u.Path) > 1 && u.Path[1] == ':' {
		u.Path = "/" + u.Path
	}
	db, err := sql.Open("sqlite", u.String())
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}
func readApp(q interface{ QueryRow(string, ...any) *sql.Row }) (*string, error) {
	var data string
	err := q.QueryRow("SELECT value FROM settings WHERE key='app'").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fail("client_database", "无法读取 NewMax settings.app，请确认数据库版本")
	}
	return &data, nil
}

func readClientResource(path, format string) (*string, error) {
	switch format {
	case "newmax":
		if _, e := os.Stat(path); os.IsNotExist(e) {
			return nil, nil
		}
		db, err := newmaxDB(path)
		if err != nil {
			return nil, err
		}
		defer db.Close()
		return readApp(db)
	case "hanako_provider", "hanako_chat":
		return readHanako(path, format)
	case "alma":
		return readAlma()
	case "zed_credential":
		return credentialRead(path)
	default:
		if err := agent.CheckPath(path); err != nil {
			return nil, err
		}
		return edit.Read(path)
	}
}

func writeClientResource(path, format string, before, after *string) error {
	if format == "newmax" {
		db, err := newmaxDB(path)
		if err != nil {
			return err
		}
		defer db.Close()
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		now, err := readApp(tx)
		if err != nil {
			return err
		}
		if !agent.Same(now, before) {
			return fail("config_conflict", "NewMax 配置已变化，请刷新后重试")
		}
		if after == nil {
			_, err = tx.Exec("DELETE FROM settings WHERE key='app'")
		} else {
			_, err = tx.Exec("INSERT INTO settings(key,value) VALUES('app',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", *after)
		}
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	now, err := readClientResource(path, format)
	if err != nil {
		return err
	}
	if !sameClientResource(now, before, format) {
		return fail("config_conflict", "配置在写入前已变化，请刷新后重试")
	}
	switch format {
	case "hanako_provider", "hanako_chat":
		return writeHanako(path, format, after)
	case "alma":
		return writeAlma(after)
	case "zed_credential":
		return credentialWrite(path, after)
	case "script":
		if err := edit.WriteAtomic(path, after); err != nil {
			return err
		}
		if after != nil {
			return os.Chmod(path, 0700)
		}
		return nil
	default:
		return edit.WriteAtomic(path, after)
	}
}
func applyClientChanges(changes []edit.Change, formats map[string]string) error {
	for _, c := range changes {
		if err := writeClientResource(c.Path, formats[c.Path], c.Before, c.After); err != nil {
			return err
		}
		got, err := readClientResource(c.Path, formats[c.Path])
		if err != nil {
			return err
		}
		if !sameClientResource(got, c.After, formats[c.Path]) {
			return fail("verify_failed", "客户端配置回读校验失败，操作日志已保留")
		}
	}
	return nil
}
func rollbackClientChanges(changes []edit.Change, formats map[string]string) error {
	var first error
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		now, err := readClientResource(c.Path, formats[c.Path])
		if err == nil && !sameClientResource(now, c.Before, formats[c.Path]) {
			if !sameClientResource(now, c.After, formats[c.Path]) {
				if formats[c.Path] == "alma" && almaPartial(now, c.Before, c.After) {
					err = writeAlma(c.Before)
				} else {
					err = fail("recovery_conflict", "恢复时发现客户端配置已被外部修改，已保留操作日志")
				}
			} else {
				err = writeClientResource(c.Path, formats[c.Path], now, c.Before)
			}
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

func almaPartial(now, before, after *string) bool {
	if now == nil || before == nil || after == nil {
		return false
	}
	var n, b, a Object
	if parse(*now, &n) != nil || parse(*before, &b) != nil || parse(*after, &a) != nil {
		return false
	}
	if encode(n["defaultModel"]) != encode(b["defaultModel"]) && encode(n["defaultModel"]) != encode(a["defaultModel"]) {
		return false
	}
	np, bp, ap := obj(n["provider"]), obj(b["provider"]), obj(a["provider"])
	if np == nil {
		return bp == nil || ap == nil
	}
	if np["name"] != "uni-switch" {
		return false
	}
	for k, v := range np {
		if k == "apiKey" {
			continue
		}
		if encode(v) == encode(bp[k]) || encode(v) == encode(ap[k]) {
			continue
		}
		if (k == "models" || k == "availableModels") && len(arr(v)) == 0 {
			continue
		}
		return false
	}
	return true
}

// Alma assigns provider IDs. Our snapshot normalizes that ID to a stable name;
// every write resolves it again, preserving all unrelated providers/settings.
func almaState() (Object, Object, []Object, error) {
	var ps []Object
	var settings Object
	if err := resourceHTTP("GET", almaEndpoint+"/api/providers", "", nil, &ps); err != nil {
		return nil, nil, nil, err
	}
	if err := resourceHTTP("GET", almaEndpoint+"/api/settings", "", nil, &settings); err != nil {
		return nil, nil, nil, err
	}
	if settings == nil {
		settings = Object{}
	}
	for _, p := range ps {
		if p["name"] == "uni-switch" && (p["type"] == "openai" || p["type"] == "custom") {
			return p, settings, ps, nil
		}
	}
	return nil, settings, ps, nil
}
func readAlma() (*string, error) {
	p, settings, _, err := almaState()
	if err != nil {
		return nil, err
	}
	state := Object{"provider": nil, "defaultModel": agent.Get(settings, "chat.defaultModel")}
	if p != nil {
		v := Object{}
		for _, k := range []string{"name", "type", "baseURL", "apiKey", "enabled", "models", "availableModels"} {
			if x, ok := p[k]; ok {
				v[k] = x
			}
		}
		state["provider"] = v
		if model := str(state["defaultModel"]); strings.HasPrefix(model, str(p["id"])+":") {
			state["defaultModel"] = "uni-switch:" + strings.TrimPrefix(model, str(p["id"])+":")
		}
	}
	return ptr(encode(state)), nil
}
func writeAlma(after *string) error {
	if after == nil {
		return fail("invalid_state", "缺少 Alma 恢复快照")
	}
	var want Object
	if err := parse(*after, &want); err != nil {
		return err
	}
	current, settings, _, err := almaState()
	if err != nil {
		return err
	}
	p := obj(want["provider"])
	pid := str(current["id"])
	if p != nil {
		core := Object{}
		for _, k := range []string{"name", "type", "baseURL", "apiKey", "enabled"} {
			core[k] = p[k]
		}
		if pid == "" {
			var made Object
			if err = resourceHTTP("POST", almaEndpoint+"/api/providers", "", core, &made); err != nil {
				return err
			}
			pid = str(made["id"])
			if pid == "" {
				current, _, _, err = almaState()
				if err != nil {
					return err
				}
				pid = str(current["id"])
			}
			if pid == "" {
				return fail("client_api", "Alma 未确认创建供应商")
			}
		} else if err = resourceHTTP("PUT", almaEndpoint+"/api/providers/"+url.PathEscape(pid), "", core, nil); err != nil {
			return err
		}
		if err = resourceHTTP("PUT", almaEndpoint+"/api/providers/"+url.PathEscape(pid)+"/models", "", Object{"models": p["models"], "availableModels": p["availableModels"]}, nil); err != nil {
			return err
		}
	}
	model := want["defaultModel"]
	if s := str(model); strings.HasPrefix(s, "uni-switch:") {
		model = pid + ":" + strings.TrimPrefix(s, "uni-switch:")
	}
	if err = agent.Set(settings, "chat.defaultModel", model, model == nil); err != nil {
		return err
	}
	if err = resourceHTTP("PUT", almaEndpoint+"/api/settings", "", settings, nil); err != nil {
		return err
	}
	if p == nil && pid != "" {
		return resourceHTTP("DELETE", almaEndpoint+"/api/providers/"+url.PathEscape(pid), "", nil, nil)
	}
	return nil
}
