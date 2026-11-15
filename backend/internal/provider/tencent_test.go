package provider

import (
	"context"
	"net/http"
	"net/http/httptest"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

const (
	tcPlanChat = "https://tokenhub.tencentmaas.com/plan/v3"
	tcPlanMsgs = "https://tokenhub.tencentmaas.com/plan/anthropic"
	// where Token Plan 个人版 was before Tencent moved it (ysm119)
	tcOldPlanChat = "https://api.lkeap.cloud.tencent.com/plan/v3"
	tcOldPlanMsgs = "https://api.lkeap.cloud.tencent.com/plan/anthropic"
	tcPlanKeys    = "https://console.cloud.tencent.com/tokenhub/tokenplan"
	tcPlanDocs    = "https://cloud.tencent.com/document/product/1823/130060"
	tcCNHost      = "https://tokenhub.tencentmaas.com"
	tcCNKeys      = "https://console.cloud.tencent.com/tokenhub/apikey"
	tcCNDocs      = "https://cloud.tencent.com/document/product/1823/130078"
	tcIntlHost    = "https://tokenhub-intl.tencentmaas.com"
	tcIntlKeys    = "https://console.tencentcloud.com/tokenhub/apikey"
	tcIntlDocs    = "https://www.tencentcloud.com/document/product/1300/78939"
)

// tcRegions are Tencent Cloud's three, as the three presets they were
// before (Jorben on Discord) gave them: the old id, the region it is now,
// and what a provider there is made with.
var tcRegions = []struct {
	old, region                string
	chat, responses, anthropic string
	catalog, keys, website     string
}{
	{"tencent-token-plan", "plan", tcPlanChat, tcPlanChat, tcPlanMsgs, "", tcPlanKeys, tcPlanDocs},
	{"tencent-tokenhub-cn", "cn", tcCNHost + "/v1", tcCNHost + "/v1", tcCNHost, "tencent-tokenhub", tcCNKeys, tcCNDocs},
	{"tencent-tokenhub", "intl", tcIntlHost + "/v1", tcIntlHost + "/v1", tcIntlHost, "tencent-tokenhub", tcIntlKeys, tcIntlDocs},
}

// TokenHub's /v1/models, as its docs show it, asked with the key as a
// Bearer: its language models are kept, the embedding one kept for
// /v1/embeddings, out of the chat models (#5339).
func TestTencentTokenHubList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer sk-test" {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		rw.Write([]byte(`{"object":"list","data":[
{"id":"hy3","object":"model","name":"Hy3","created":1783267200,"status":"online"},
{"id":"deepseek-v4-pro","object":"model","name":"DeepSeek-V4-Pro","created":1776960000,"status":"online"},
{"id":"kinfra-text-embedding-0.6b","object":"model","name":"KInfra Embedding","created":1776960000,"status":"online"}]}`))
	}))
	defer srv.Close()
	ms, _, err := catalog.FetchAt(context.Background(), srv.URL+"/v1", "sk-test", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if chat := catalog.Chat(ms); len(chat) != 2 || chat[0].ID != "hy3" || chat[1].ID != "deepseek-v4-pro" {
		t.Fatalf("%+v", ms)
	}
	if len(ms) != 3 || ms[2].ID != "kinfra-text-embedding-0.6b" || ms[2].Serves != catalog.ServesEmbedding {
		t.Fatalf("the embedding model: %+v", ms)
	}
}
