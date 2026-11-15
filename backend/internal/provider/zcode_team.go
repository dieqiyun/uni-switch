package provider

// PLUGIN-SERVED (see AGENTS.md): ZCode ("zcode") is a retired built-in
// subscription, served by its plugin alone,
// the retired opencode-zcode-auth (provider.BuiltinRetired). What is
// left here are the readers of a team's seat that zcodeOwn and the Zhipu
// plan card (planquota.go) still use. A fix belongs in the plugin
// (github.com/dieqiyun/uni-switch, packages/zcode), with the mover's
// min raised in internal/provider/migrate_zcode.go.

// A team's GLM Coding Plan (团队套餐), bought by an organization on Z.ai or
// BigModel (智谱) and handed out as seats to its members, is served where a
// person's plan is (api.z.ai/api/anthropic, open.bigmodel.cn/api/anthropic)
// but to a key of the team's project, not of the member's own. As ZCode
// 3.14.3 does it (host/index.js):
//
//   - the account's organizations (getCustomerInfo) have the team's
//     project, of projectType 2 (isBigModelTeamCodingPlanProject);
//   - the seat is told by /api/biz/team/subscribe/product/querySubscribeDetail
//     asked with the business sign-in and the project's bigmodel-organization
//     and bigmodel-project headers: usable when status is EFFECTIVE and
//     memberGrantStatus VALID, UNASSIGNED when the admin gave no seat,
//     EXPIRED when the team's plan ran out (fetchTeamCodingPlanEntitlement);
//   - the key is the project's zcode-team-api-key of keyType 2, made when it
//     isn't there, with its secret (ensureBigModelTeamPlanProjectApiKeyWithStatus,
//     copyBigModelTeamPlanProjectApiKeySecret); requests carry it as a
//     person's key is carried, with no more headers;
//   - what is used is /api/monitor/usage/quota/limit?type=2 with that key and
//     the two headers (buildQuotaLimitUrl, createBigModelUsageHeaders): a
//     CREDIT_LIMIT for the five hours (unit 3) and one for the week (unit 6).
//
// BigModel's business API is bigmodel.cn, Z.ai's api.z.ai; BigModel's
// sign-in token goes bare in Authorization, Z.ai's business one as a
// Bearer. The resets a team member may spend are listed by
// /api/biz/customer-package-reset/list?targetType=TEAM, as bigmodel.cn's
// own usage page asks it.
//
// ZCode keeps no team key in its credential store: it finds it when it is
// switched to the team's plan (~/.zcode/v2/setting.json's
// providerFamilyConnectionSelections, resolveAccountTeamPlanRuntimeApiKey),
// and so does uni-switch for ZCode's own account.

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
)

func (k zcodeKey) team() bool { return k.Org != "" && k.Project != "" }

// zcodeBizRoot is the business API of the site a plan is served from.
func zcodeBizRoot(base string) string {
	if h := hostOf(base); h == hostOf(ZCodeBigModelBase) || strings.HasSuffix(h, "bigmodel.cn") {
		return zcodeBigModelAPI
	}
	return zcodeRoot(base)
}

// zcodeTeamHeaders are what the team's business and usage endpoints are
// asked with besides the Authorization.
func zcodeTeamHeaders(base, org, project string) map[string]string {
	lang := "en"
	if zcodeBizRoot(base) == zcodeBigModelAPI {
		lang = "zh"
	}
	return map[string]string{
		"Bigmodel-Organization": org,
		"Bigmodel-Project":      project,
		"Set-Language":          lang,
		"Accept-Language":       "en-US,en",
	}
}

func zcodeWhen(v any) *time.Time {
	if s, ok := v.(string); ok {
		if t := zhipuTime(s); t != nil {
			return t
		}
	}
	if n, ok := zcodeNum(v); ok && n > 0 {
		var t time.Time
		if n > 1e12 {
			t = time.UnixMilli(int64(n))
		} else {
			t = time.Unix(int64(n), 0)
		}
		return &t
	}
	return nil
}

// ---- ZCode's own account on a team's plan -------------------------------------

// zcodeOwnTeam is ZCode's own account when ZCode is switched to a team's
// plan: the project it picked and the sign-in its key is found with.
func zcodeOwnTeam(store map[string]string, secret []byte) (zcodeKey, bool) {
	home, _ := os.UserHomeDir()
	var set struct {
		Domain string `json:"providerFamilyDomain"`
		Sel    map[string]struct {
			Kind    string `json:"kind"`
			Org     string `json:"organizationId"`
			Project string `json:"projectId"`
		} `json:"providerFamilyConnectionSelections"`
		Legacy map[string]string `json:"modelProviderFamilySelectedKeys"` // before providerFamilyConnectionSelections (migrateLegacyAccountConnectionSettings)
	}
	if !readJSON(filepath.Join(appdir.ZCodeHome(home), ".zcode", "v2", "setting.json"), &set) {
		return zcodeKey{}, false
	}
	families := []string{"zai", "bigmodel"}
	if set.Domain == "zai" || set.Domain == "bigmodel" {
		families = []string{set.Domain}
	}
	for _, fam := range families {
		org, project := "", ""
		if s, ok := set.Sel[fam]; ok {
			if s.Kind == "team-coding-plan" {
				org, project = strings.TrimSpace(s.Org), strings.TrimSpace(s.Project)
			}
		} else if rest, ok := strings.CutPrefix(strings.TrimSpace(set.Legacy[fam]), "team-plan:builtin:"+fam+"-coding-plan:"); ok {
			if parts := strings.Split(rest, ":"); len(parts) == 3 {
				org, _ = url.PathUnescape(parts[1])
				project, _ = url.PathUnescape(parts[2])
			}
		}
		if org == "" || project == "" {
			continue
		}
		v, ok := store["oauth:"+fam+":access_token"]
		if !ok {
			continue
		}
		tok, ok := zcodeDecrypt(secret, v)
		if tok = strings.TrimSpace(tok); !ok || tok == "" {
			continue
		}
		if jv, ok := store["zcodejwttoken"]; ok && fam == "bigmodel" {
			if jwt, ok := zcodeDecrypt(secret, jv); ok && strings.TrimSpace(jwt) == tok {
				continue // a stale token ZCode won't use either
			}
		}
		base := ZCodeZaiBase
		if fam == "bigmodel" {
			base = ZCodeBigModelBase
		}
		return zcodeKey{Base: base, Token: tok, Org: org, Project: project}, true
	}
	return zcodeKey{}, false
}

// ---- allowance ------------------------------------------------------------------

// zhipuPersonalResets is how many resets a GLM Coding Plan of a person's
// own holds, as bigmodel.cn's usage page lists them (#1191), asked with
// the plan's key where its term is (zhipuTerm). Only read: a reset is
// spent on the vendor's own page, never from uni-switch.
func zhipuPersonalResets(ctx context.Context, root, key string) *ResetCredits {
	return zhipuResets(ctx, root+"/api/biz/customer-package-reset/list?targetType=PERSONAL", key, nil)
}

// zhipuResets reads customer-package-reset/list: the resets of the five
// hours and of the week, of which only those still available count (one
// run out stays listed a week, available false). nil when the list
// can't be read, so no count is shown rather than 0, and when it holds
// none.
func zhipuResets(ctx context.Context, u, auth string, hdr map[string]string) *ResetCredits {
	type reset struct {
		Available bool `json:"available"`
		Expire    any  `json:"expireTime"`
	}
	var data struct {
		FiveHour []reset `json:"fiveHourResets"`
		Week     []reset `json:"weekResets"`
	}
	if err := zcodeCallH(ctx, http.MethodGet, u, auth, hdr, nil, &data); err != nil {
		return nil
	}
	r := &ResetCredits{ByWindow: true}
	count := func(rs []reset, window string) (n int) {
		for _, x := range rs {
			if !x.Available {
				continue
			}
			n++
			t := zcodeWhen(x.Expire)
			if t != nil && (r.Until == nil || t.Before(*r.Until)) {
				r.Until = t
			}
			r.Each = append(r.Each, ResetCard{Until: t, Window: window})
		}
		return n
	}
	r.FiveHour, r.Weekly = count(data.FiveHour, "fiveHour"), count(data.Week, "weekly")
	r.Count = r.FiveHour + r.Weekly
	sortResetCards(r.Each)
	if r.Count == 0 {
		return nil
	}
	return r
}

// zhipuTeamOf is the team project a GLM key belongs to, when that key is a
// ZCode account's team key uni-switch holds.
func zhipuTeamOf(key string) (org, project string) {
	for _, l := range zcodeLogins() {
		if l.key.team() && l.key.Key == key {
			return l.key.Org, l.key.Project
		}
	}
	return "", ""
}

// zcodeNum reads a number the API gives as a number or a string.
func zcodeNum(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}
