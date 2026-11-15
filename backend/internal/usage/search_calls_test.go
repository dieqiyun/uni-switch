package usage

import (
	"slices"
	"testing"
	"time"
)

// cybercaius on Discord: which search API a search used, and how often,
// couldn't be seen. Each ask of a search API (SourceSearch) is a row of
// the request log of its own, picked by the Web search purpose, naming the
// API, where it was asked and the key, and counted in nothing: no provider,
// no total or budget. A searcher model's own request
// (kind web_search, a real provider call) still counts.
func TestSearchAPIRowsListedNotCounted(t *testing.T) {
	reportedHome(t)
	now := time.Now().Add(-time.Hour)
	Append(Record{Time: now, Agent: "claude", Provider: "openai", Host: "api.openai.com", Model: "gpt-5-mini", Requested: "gpt-5-mini", Kind: "web_search", Status: 200, Millis: 3000})
	Append(Record{Time: now.Add(time.Second), Agent: "claude", Requested: "Tavily", Host: "api.tavily.com", ProviderKeyID: "k1", ProviderKeyName: "tvly…abcd",
		Millis: 800, Status: 200, Kind: "web_search", Source: SourceSearch})
	Append(Record{Time: now.Add(2 * time.Second), Agent: "claude", Requested: "Brave Search", Host: "api.search.brave.com", Millis: 90, Status: 429,
		Error: "rate limited", Kind: "web_search", Source: SourceSearch})
	Append(Record{Time: now.Add(3 * time.Second), Agent: "uni-switch", Requested: "DeepSeek", Host: "api.deepseek.com", Millis: 50,
		Error: "found nothing", Kind: "web_search", Source: SourceSearch})

	page := QueryPage(All, Filter{}, 0, 50)
	if page.Total != 4 || len(page.Rows) != 4 {
		t.Fatalf("total %d, %d rows: %+v", page.Total, len(page.Rows), page.Rows)
	}
	ds, brave, tavily := page.Rows[0], page.Rows[1], page.Rows[2] // newest first
	if tavily.Source != SourceSearch || !tavily.IsProbe() || tavily.Requested != "Tavily" || tavily.Host != "api.tavily.com" ||
		tavily.ProviderKeyName != "tvly…abcd" || tavily.Failed() || tavily.Provider != "" || tavily.Millis != 800 {
		t.Fatalf("tavily row %+v", tavily)
	}
	if brave.Source != SourceSearch || !brave.Failed() || brave.Status != 429 {
		t.Fatalf("brave row %+v", brave)
	}
	// "deepseek" is a provider's id too: the search API's row is not its
	if ds.Source != SourceSearch || ds.Provider != "" || !ds.Failed() {
		t.Fatalf("deepseek row %+v", ds)
	}
	if page.Sum.Calls != 1 || page.Sum.Errors != 0 || page.Through.Calls != 1 {
		t.Fatalf("sum %+v through %+v", page.Sum, page.Through)
	}
	for dim, shares := range page.By {
		for _, s := range shares {
			if slices.Contains([]string{"Tavily", "Brave Search", "DeepSeek", "deepseek"}, s.ID) || (dim == "provider" && s.Calls != 1) {
				t.Fatalf("%s counts a search API ask: %+v", dim, shares)
			}
		}
	}
	if s := Summarize(All); s.Calls != 1 || s.Errors != 0 {
		t.Fatalf("summary %+v", s.Totals)
	}
	if rs := Load(time.Time{}); len(rs) != 1 {
		t.Fatalf("Load %+v", rs)
	}
	// the Web search purpose lists both kinds of search, and the API's
	// name narrows it to that API's asks
	p := QueryPage(All, Filter{Purpose: PurposeOf("web_search")}, 0, 50)
	if p.Total != 4 || p.Sum.Calls != 1 {
		t.Fatalf("purpose filter: total %d sum %+v", p.Total, p.Sum)
	}
	p = QueryPage(All, Filter{Purpose: PurposeOf("web_search"), Query: "tavily"}, 0, 50)
	if p.Total != 1 || p.Rows[0].Requested != "Tavily" {
		t.Fatalf("query filter: total %d rows %+v", p.Total, p.Rows)
	}
	if hosts := readLogSnapshot().hosts; hosts["api.tavily.com"] != "" || hosts["api.deepseek.com"] != "" {
		t.Fatalf("hosts %v", hosts)
	}
}

// cybercaius on Discord: the Requests page's purposes were All, Connection
// check and the unmarked purpose while the log held searches. A search row,
// a search API's or one a provider's model ran with its own tool, gives the
// purpose list Web search, and that purpose picks it.
func TestSearchRowsListTheirPurpose(t *testing.T) {
	reportedHome(t)
	now := time.Now().Add(-time.Hour)
	Append(Record{Time: now, Agent: "claude", Provider: "deepseek", Host: "api.deepseek.com", Model: "deepseek-flash", Requested: "deepseek-flash", Status: 200, Millis: 3000})
	Append(Record{Time: now, Agent: "claude", Provider: "deepseek", Host: "api.deepseek.com", Model: "deepseek-flash", Requested: "deepseek-flash",
		Status: 200, Kind: "web_search", Source: SourceSearch})
	page := QueryPage(All, Filter{}, 0, 50)
	if !slices.Contains(page.Purposes, PurposeOf("web_search")) {
		t.Fatalf("purposes %v", page.Purposes)
	}
	p := QueryPage(All, Filter{Purpose: PurposeOf("web_search")}, 0, 50)
	if p.Total != 1 || p.Rows[0].Source != SourceSearch || p.Sum.Calls != 0 {
		t.Fatalf("purpose filter: total %d rows %+v sum %+v", p.Total, p.Rows, p.Sum)
	}
}
