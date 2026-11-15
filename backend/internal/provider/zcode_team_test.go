package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// zcodeTeamUpstream is zcode.z.ai and BigModel (bigmodel.cn and
// open.bigmodel.cn) for these tests: a BigModel account with a personal
// project that has no plan, and two team coding plan projects, tp0 where
// it has no seat and tp1 where its seat is grant.
type zcodeTeamUpstream struct {
	srv   *httptest.Server
	grant string // tp1's memberGrantStatus
	mu    sync.Mutex
	made  map[string]any // the team key made, as it was asked for
	model *http.Request  // the last request to the plan's endpoint
	quota *http.Request  // the last quota asked
}

const zcodeTeamToken = "bm-tok" // BigModel's sign-in token, as the poll hands it back

func newZCodeTeamUpstream(t *testing.T) *zcodeTeamUpstream {
	t.Helper()
	u := &zcodeTeamUpstream{grant: "VALID"}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		org, proj := r.Header.Get("Bigmodel-Organization"), r.Header.Get("Bigmodel-Project")
		ok := func(data any) {
			json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "ok", "data": data, "success": true})
		}
		deny := func() { w.WriteHeader(401) }
		switch p := r.URL.Path; {
		case p == "/api/v1/oauth/cli/init":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["provider"] != "bigmodel" {
				w.WriteHeader(400)
				return
			}
			ok(map[string]any{"flow_id": "f1", "authorize_url": "https://open.bigmodel.cn/oauth?client_id=c",
				"expires_at": time.Now().Add(time.Minute).Unix(), "poll_interval_sec": 0})
		case p == "/api/v1/oauth/cli/poll/f1":
			ok(map[string]any{"status": "ready", "token": "zc", "bigmodel": map[string]any{"accessToken": zcodeTeamToken},
				"user": map[string]any{"user_id": "u9", "email": "team@example.com", "name": "Team"}})
		case p == "/api/auth/z/login":
			deny() // BigModel's token is never exchanged
		case p == "/api/biz/customer/getCustomerInfo":
			if auth != zcodeTeamToken {
				deny()
				return
			}
			ok(map[string]any{"organizations": []any{
				map[string]any{"organizationId": "o1", "organizationName": "默认机构", "projects": []any{
					map[string]any{"projectId": "p1", "projectName": "默认项目", "projectType": "1"}}},
				map[string]any{"organizationId": "t1", "organizationName": "Acme", "projects": []any{
					map[string]any{"projectId": "tp0", "projectName": "old team", "projectType": 2},
					map[string]any{"projectId": "tp1", "projectName": "team", "projectType": "2"}}},
			}})
		case p == "/api/biz/v1/organization/o1/projects/p1/api_keys" && auth == zcodeTeamToken:
			ok([]any{map[string]any{"name": "zcode-api-key", "apiKey": "pk"}})
		case p == "/api/biz/v1/organization/o1/projects/p1/api_keys/copy/pk" && auth == zcodeTeamToken:
			ok(map[string]any{"secretKey": "ps"})
		case p == "/api/biz/subscription/list":
			if auth != "pk.ps" {
				deny()
				return
			}
			ok([]any{}) // no plan of its own
		case p == "/api/biz/team/subscribe/product/querySubscribeDetail":
			if auth != zcodeTeamToken || org != "t1" {
				deny()
				return
			}
			switch proj {
			case "tp0":
				ok(map[string]any{"hasSubscription": true, "status": "EFFECTIVE", "memberGrantStatus": "UNASSIGNED", "productName": "GLM Coding Team Lite"})
			case "tp1":
				ok(map[string]any{"hasSubscription": true, "status": "EFFECTIVE", "memberGrantStatus": u.grant, "productId": "prod-1",
					"productName": "GLM Coding Team Pro", "subscribeEndTime": "2026-12-31 23:59:59"})
			default:
				deny()
			}
		case p == "/api/biz/v1/organization/t1/projects/tp1/api_keys":
			if auth != zcodeTeamToken || org != "t1" || proj != "tp1" {
				deny()
				return
			}
			u.mu.Lock()
			defer u.mu.Unlock()
			if r.Method == http.MethodPost {
				json.NewDecoder(r.Body).Decode(&u.made)
				ok(map[string]any{"name": "zcode-team-api-key", "apiKey": "tk", "keyType": 2})
				return
			}
			list := []any{map[string]any{"name": "zcode-team-api-key", "apiKey": "wrong-type", "keyType": 1}}
			if u.made != nil {
				list = append(list, map[string]any{"name": "zcode-team-api-key", "apiKey": "tk", "keyType": 2})
			}
			ok(list)
		case p == "/api/biz/v1/organization/t1/projects/tp1/api_keys/copy/tk":
			if auth != zcodeTeamToken || org != "t1" || proj != "tp1" {
				deny()
				return
			}
			ok(map[string]any{"secretKey": "ts"})
		case p == "/api/monitor/usage/quota/limit":
			u.mu.Lock()
			u.quota = r
			u.mu.Unlock()
			if auth != "tk.ts" || r.URL.Query().Get("type") != "2" {
				ok(map[string]any{"limits": []any{}})
				return
			}
			reset := time.Now().Add(2 * time.Hour).UnixMilli()
			ok(map[string]any{"limits": []any{
				map[string]any{"type": "CREDIT_LIMIT", "unit": 3, "percentage": 42, "currentValue": 420, "usage": 1000, "nextResetTime": reset},
				map[string]any{"type": "CREDIT_LIMIT", "unit": 6, "percentage": 10, "currentValue": 1000, "usage": 10000, "nextResetTime": reset + 86400000},
			}})
		case p == "/api/biz/customer-package-reset/list":
			if auth != zcodeTeamToken || org != "t1" || proj != "tp1" || r.URL.Query().Get("targetType") != "TEAM" {
				deny()
				return
			}
			ok(map[string]any{
				"fiveHourResets": []any{
					map[string]any{"available": true, "recordId": 1, "expireTime": "2026-10-05 00:00:00", "grantType": "MONTHLY"},
					map[string]any{"available": false, "recordId": 2, "expireTime": "2026-10-01 00:00:00"},
					map[string]any{"available": true, "recordId": 3, "expireTime": "2026-10-20 00:00:00"},
				},
				"weekResets": []any{map[string]any{"available": true, "recordId": 4, "expireTime": "2026-10-10 00:00:00"}},
			})
		case strings.HasSuffix(p, "/v1/messages"):
			u.mu.Lock()
			u.model = r
			u.mu.Unlock()
			w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(u.srv.Close)
	oldAPI, oldZai, oldBM, oldZaiBase, oldBMBase := zcodeAPI, zcodeZaiAPI, zcodeBigModelAPI, ZCodeZaiBase, ZCodeBigModelBase
	zcodeAPI, zcodeZaiAPI, zcodeBigModelAPI = u.srv.URL, u.srv.URL, u.srv.URL
	ZCodeZaiBase, ZCodeBigModelBase = u.srv.URL+"/zai/api/anthropic", u.srv.URL+"/bigmodel/api/anthropic"
	t.Cleanup(func() {
		zcodeAPI, zcodeZaiAPI, zcodeBigModelAPI, ZCodeZaiBase, ZCodeBigModelBase = oldAPI, oldZai, oldBM, oldZaiBase, oldBMBase
	})
	return u
}

// A GLM key's team windows are asked where ZCode asks them, with the team
// project's headers when the key is a team seat's uni-switch holds.
func TestZhipuKeyTeamWindows(t *testing.T) {
	signIn(t)
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	u := newZCodeTeamUpstream(t)
	// a seat signed in to the built-in, its team project's key in logins.json
	plan := "GLM Coding Team Pro"
	auth, _ := json.Marshal(zcodeKey{Key: "tk.ts", Base: ZCodeBigModelBase, Org: "t1", Project: "tp1"})
	if err := addSideLogin(savedLogin{Agent: "zcode", User: "team@example.com", Plan: plan, Auth: auth}, "", func(savedLogin) {}); err != nil {
		t.Fatal(err)
	}
	plan, ws, err := zhipuKeyTeamWindows(context.Background(), ZCodeBigModelBase, "tk.ts", nil)
	if err != nil || plan != "GLM Coding Team" || len(ws) != 2 || ws[0].Name != "5 hours" || ws[0].Used != 42 || ws[0].Span != 5*time.Hour || ws[1].Name != "7 days" {
		t.Fatalf("windows: %v %q %+v", err, plan, ws)
	}
	if u.quota.Header.Get("Bigmodel-Project") != "tp1" {
		t.Fatalf("asked with: %v", u.quota.Header)
	}
	// a key that isn't a seat uni-switch holds is asked without them, and has none
	if _, ws, err := zhipuKeyTeamWindows(context.Background(), ZCodeBigModelBase, "other.key", nil); err != nil || len(ws) != 0 {
		t.Fatalf("another key: %v %+v", err, ws)
	}
}

// ZCode's own account, switched in ZCode to a team's plan, keeps no team
// key: uni-switch finds it as ZCode does, from ZCode's sign-in and the project
// its settings name.
func TestZCodeOwnTeam(t *testing.T) {
	home := signIn(t)
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	newZCodeTeamUpstream(t)
	writeFile(t, filepath.Join(home, ".zcode", "v2", "credentials.json"), map[string]any{
		"oauth:bigmodel:user_info":    zcodeEncrypt(t, `{"user_id":"u9","email":"team@example.com"}`),
		"oauth:bigmodel:access_token": zcodeEncrypt(t, zcodeTeamToken),
		"zcodejwttoken":               zcodeEncrypt(t, "zc"),
	})
	writeFile(t, filepath.Join(home, ".zcode", "v2", "setting.json"), map[string]any{
		"providerFamilyDomain": "bigmodel",
		"providerFamilyConnectionSelections": map[string]any{
			"bigmodel": map[string]any{"kind": "team-coding-plan", "productId": "prod-1", "organizationId": "t1", "projectId": "tp1"},
		},
	})
	who, k, ok := zcodeOwn()
	if !ok || who != "team@example.com" || k.Key != "" || k.Org != "t1" || k.Project != "tp1" || k.Base != ZCodeBigModelBase || k.JWT != "zc" {
		t.Fatalf("own: %v %q %+v", ok, who, k)
	}
	// ZCode on its individual plan again: as before
	writeFile(t, filepath.Join(home, ".zcode", "v2", "setting.json"), map[string]any{
		"providerFamilyDomain":               "bigmodel",
		"providerFamilyConnectionSelections": map[string]any{"bigmodel": map[string]any{"kind": "individual-coding-plan"}},
	})
	if _, k, ok := zcodeOwn(); !ok || k.team() {
		t.Fatalf("own, individual: %v %+v", ok, k)
	}
}
