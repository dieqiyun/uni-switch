package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
)

// The signature is the one Volcengine's own SDK makes: these were made by
// volc-sdk-golang (06f9ef66) base.GetSignRequest for a POST to
// open.volcengineapi.com/ with Action and Version in the query, body {},
// Content-Type application/json, service ark, at 2026-10-09T08:30:15Z.
func TestVolcSignMatchesSDK(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 30, 15, 0, time.UTC)
	for _, c := range []struct{ region, action, sig string }{
		{"cn-beijing", "GetCodingPlanUsage", "deb80c0cd8bd77768ca40f6d47c7204621c0994428f6ab77e23b76e15dc4c047"},
		{"cn-shanghai", "GetAFPUsage", "8a241be0996c93de4dfcd9e8b1db37494bf63170804fcaebe8380ff7dca2bb23"},
	} {
		auth, xDate, sha := volcSign("AKLTexample", "c2VjcmV0LWV4YW1wbGU=", c.region,
			url.Values{"Action": {c.action}, "Version": {"2024-01-01"}}, []byte("{}"), now)
		want := "HMAC-SHA256 Credential=AKLTexample/20261009/" + c.region + "/ark/request, SignedHeaders=content-type;host;x-content-sha256;x-date, Signature=" + c.sig
		if auth != want {
			t.Errorf("%s:\n got %s\nwant %s", c.action, auth, want)
		}
		if xDate != "20261009T083015Z" || sha != "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" {
			t.Errorf("%s: X-Date %s, X-Content-Sha256 %s", c.action, xDate, sha)
		}
	}
	// a time in another zone signs as the same instant in UTC
	cst := time.Date(2026, 10, 9, 16, 30, 15, 0, time.FixedZone("CST", 8*3600))
	if _, xDate, _ := volcSign("a", "b", "cn-beijing", url.Values{}, nil, cst); xDate != "20261009T083015Z" {
		t.Errorf("X-Date in CST: %s", xDate)
	}
}

// The plan is the endpoint's: /api/coding the Coding Plan's, /api/plan the
// Agent Plan's, in the host's region; pay-as-you-go, another vendor and an
// account have none.
func TestVolcPlanOf(t *testing.T) {
	for _, c := range []struct {
		p                    Provider
		action, plan, region string
	}{
		{Provider{Chat: "https://ark.cn-beijing.volces.com/api/coding/v3"}, "GetCodingPlanUsage", "Coding Plan", "cn-beijing"},
		{Provider{Anthropic: "https://ark.cn-beijing.volces.com/api/coding"}, "GetCodingPlanUsage", "Coding Plan", "cn-beijing"},
		{Provider{Chat: "https://ark.cn-shanghai.volces.com/api/plan/v3"}, "GetAFPUsage", "Agent Plan", "cn-shanghai"},
		{Provider{Chat: "https://ark.ap-southeast.volces.com/api/plan"}, "GetAFPUsage", "Agent Plan", "ap-southeast"},
		{Provider{Chat: "https://ark.cn-beijing.volces.com/api/v3"}, "", "", ""},
		{Provider{Chat: "https://relay.example.com/api/coding/v3"}, "", "", ""},
		{Provider{Chat: "https://ark.cn-beijing.volces.com/api/coding/v3", Account: &Account{}}, "", "", ""},
	} {
		action, plan, region, ok := volcPlanOf(c.p)
		if action != c.action || plan != c.plan || region != c.region || ok != (c.action != "") {
			t.Errorf("%+v: %s %s %s %v", c.p, action, plan, region, ok)
		}
	}
	if !TakesVolcAccessKey(Provider{Chat: "https://ark.cn-beijing.volces.com/api/v3"}) || TakesVolcAccessKey(Provider{Chat: "https://api.deepseek.com/v1"}) {
		t.Error("TakesVolcAccessKey")
	}
}

// GetCodingPlanUsage as pi-ark-usage's fixture has it: Percent used,
// ResetTimestamp in seconds; an unknown level is left out.
func TestVolcReadCodingPlan(t *testing.T) {
	ws := volcReadCodingPlan(json.RawMessage(`{"Status":"ok","UpdateTimestamp":1758000000,"QuotaUsage":[
		{"Level":"session","Percent":10.5,"ResetTimestamp":1758100000},
		{"Level":"weekly","Percent":20.25,"ResetTimestamp":1758200000},
		{"Level":"monthly","Percent":"30.75","ResetTimestamp":1758300000},
		{"Level":"daily","Percent":1}]}`))
	if len(ws) != 3 || ws[0].Name != "5 hours" || ws[0].Used != 10.5 || ws[0].Span != 5*time.Hour || ws[0].ResetsAt == nil || ws[0].ResetsAt.Unix() != 1758100000 ||
		ws[1].Name != "Weekly" || ws[1].Used != 20.25 || ws[2].Name != "Month" || ws[2].Used != 30.75 || ws[2].ResetsAt.Unix() != 1758300000 {
		t.Fatalf("%+v", ws)
	}
	if ws := volcReadCodingPlan(json.RawMessage(`{"QuotaUsage":[]}`)); len(ws) != 0 {
		t.Fatalf("none: %+v", ws)
	}
}

// GetAFPUsage in both forms the tools built on it read: Used of Quota with
// a reset in ms (dsh-ark-quota), and Percent or Used of Total with
// ResetTimestamp (arkcli); a bucket with no quota is not on the plan.
func TestVolcReadAFP(t *testing.T) {
	ws, tier := volcReadAFP(json.RawMessage(`{"AFPFiveHour":{"Quota":100,"Used":42,"ResetTime":1790000000000},
		"AFPDaily":{"Quota":500,"Used":400},"AFPWeekly":{"Quota":0,"Used":5},"AFPMonthly":{"Quota":200,"Used":50}}`))
	if tier != "" || len(ws) != 2 || ws[0].Name != "5 hours" || ws[0].Used != 42 || ws[0].Amount != 42 || ws[0].Limit != 100 ||
		ws[0].ResetsAt == nil || ws[0].ResetsAt.Unix() != 1790000000 || ws[1].Name != "Month" || ws[1].Used != 25 || ws[1].ResetsAt != nil {
		t.Fatalf("Quota form: %+v", ws)
	}
	ws, tier = volcReadAFP(json.RawMessage(`{"Tier":"Pro","AFPFiveHour":{"Percent":12.5,"ResetTimestamp":1790000000},
		"AFPWeekly":{"Used":"30","Total":"120","ResetTime":"2026-10-12T00:00:00Z"}}`))
	if tier != "Pro" || len(ws) != 2 || ws[0].Used != 12.5 || ws[0].ResetsAt.Unix() != 1790000000 ||
		ws[1].Name != "Weekly" || ws[1].Used != 25 || ws[1].ResetsAt.Format(time.RFC3339) != "2026-10-12T00:00:00Z" {
		t.Fatalf("Total form: %+v %q", ws, tier)
	}
}

// The Usage page shows a card for each access key and plan, asked of
// open.volcengineapi.com only, signed with the access key and never with
// the plan's key; pay-as-you-go, a provider with no Secret and one
// switched off have none, and a refused access key says what to check.
func TestVolcPlanQuotas(t *testing.T) {
	isolate(t)
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("PATH", h)
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
	secrets := map[string]string{"AK1": "SK1", "AKbad": "SKwrong"}
	var mu sync.Mutex
	asked := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Host") != "open.volcengineapi.com" || r.Method != http.MethodPost || r.URL.Path != "/" || string(body) != "{}" {
			t.Errorf("asked %s %s%s with %q", r.Method, r.Header.Get("X-Host"), r.URL, body)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		auth := r.Header.Get("Authorization")
		if strings.Contains(auth, "ark-k") {
			t.Errorf("the plan's key was sent: %s", auth)
		}
		// Credential=<ak>/<date>/<region>/ark/request
		cred, _, _ := strings.Cut(strings.TrimPrefix(auth, "HMAC-SHA256 Credential="), ",")
		parts := strings.Split(cred, "/")
		if len(parts) != 5 {
			t.Errorf("Authorization %q", auth)
			return
		}
		ak, region, action := parts[0], parts[2], r.URL.Query().Get("Action")
		xDate, _ := time.Parse("20060102T150405Z", r.Header.Get("X-Date"))
		want, _, _ := volcSign(ak, secrets[ak], region, r.URL.Query(), body, xDate)
		if ak == "AKbad" || auth != want || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"ResponseMetadata":{"Action":"`+action+`","Error":{"Code":"SignatureDoesNotMatch","Message":"The request signature we calculated does not match"}}}`)
			return
		}
		mu.Lock()
		asked[ak+" "+region+" "+action]++
		mu.Unlock()
		switch action {
		case "GetCodingPlanUsage":
			io.WriteString(w, `{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Status":"ok","QuotaUsage":[{"Level":"session","Percent":10.5,"ResetTimestamp":1758100000},{"Level":"weekly","Percent":20.25,"ResetTimestamp":1758200000},{"Level":"monthly","Percent":30.75,"ResetTimestamp":1758300000}]}}`)
		case "GetAFPUsage":
			io.WriteString(w, `{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Tier":"Lite","AFPFiveHour":{"Quota":100,"Used":42},"AFPWeekly":{"Quota":400,"Used":100},"AFPMonthly":{"Quota":0,"Used":0}}}`)
		default:
			t.Errorf("action %q", action)
		}
	}))
	defer srv.Close()
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = rewrite{srv}
	t.Cleanup(func() { http.DefaultClient.Transport = old })
	forgetPlanQuotas()
	t.Cleanup(forgetPlanQuotas)

	for _, p := range []Provider{
		{ID: "volcengine", Name: "Volcengine Ark", Chat: "https://ark.cn-beijing.volces.com/api/coding/v3", Key: "ark-k", AccessKeyID: "AK1", SecretAccessKey: "SK1"},
		// the same access key's Coding Plan again: one card
		{ID: "volc-copy", Name: "Ark copy", Anthropic: "https://ark.cn-beijing.volces.com/api/coding", Key: "ark-k", AccessKeyID: "AK1", SecretAccessKey: "SK1"},
		{ID: "volc-agent", Name: "Ark Agent", Chat: "https://ark.cn-shanghai.volces.com/api/plan/v3", Key: "ark-k", AccessKeyID: "AK1", SecretAccessKey: "SK1"},
		{ID: "volc-payg", Name: "Ark API", Chat: "https://ark.cn-beijing.volces.com/api/v3", Key: "ark-k", AccessKeyID: "AK1", SecretAccessKey: "SK1"},
		{ID: "volc-nosecret", Name: "Ark half", Chat: "https://ark.cn-beijing.volces.com/api/plan/v3", Key: "ark-k", AccessKeyID: "AK2"},
		{ID: "volc-off", Name: "Ark off", Chat: "https://ark.cn-beijing.volces.com/api/plan/v3", Key: "ark-k", AccessKeyID: "AK3", SecretAccessKey: "SK3", Off: true},
		{ID: "volc-bad", Name: "Ark bad", Chat: "https://ark.cn-beijing.volces.com/api/coding/v3", Key: "ark-k", AccessKeyID: "AKbad", SecretAccessKey: "SKwrong"},
	} {
		if err := Save(p); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]SubscriptionQuota{}
	for _, q := range PlanQuotas(context.Background()) {
		if q.Plan != "" && strings.HasPrefix(q.Provider, "volc") {
			got[q.Provider] = q
		}
	}
	if len(got) != 3 {
		t.Fatalf("cards: %+v", got)
	}
	coding, ok := got["volcengine"]
	if !ok {
		coding = got["volc-copy"]
	}
	if coding.Plan != "Coding Plan" || coding.Error != "" || len(coding.Windows) != 3 || coding.Windows[0].Name != "5 hours" || coding.Windows[0].Used != 10.5 ||
		coding.Windows[2].Name != "Month" || coding.Windows[2].ResetsAt.Unix() != 1758300000 {
		t.Errorf("Coding Plan: %+v", coding)
	}
	if q := got["volc-agent"]; q.Plan != "Agent Plan Lite" || q.Error != "" || len(q.Windows) != 2 || q.Windows[0].Used != 42 || q.Windows[1].Name != "Weekly" || q.Windows[1].Used != 25 {
		t.Errorf("Agent Plan: %+v", q)
	}
	if q := got["volc-bad"]; len(q.Windows) != 0 || !strings.Contains(q.Error, "SignatureDoesNotMatch") || !strings.Contains(q.Error, "AccessKey ID and Secret") {
		t.Errorf("refused: %+v", q)
	}
	if asked["AK1 cn-beijing GetCodingPlanUsage"] != 1 || asked["AK1 cn-shanghai GetAFPUsage"] != 1 || len(asked) != 2 {
		t.Errorf("asked: %v", asked)
	}
}
