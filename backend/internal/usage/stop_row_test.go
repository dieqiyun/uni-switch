package usage

import (
	"encoding/csv"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// The upstream's own stop reason, which the gateway records with each call,
// reaches the Requests page and /v1/uni-switch/usage/requests (its "Upstream
// stop reason"): it is what tells a reply the model ended from one cut
// short, and the page reads its rows from the packed cache, which kept
// every other string of a row but this one (#1927, Gluxggg).
func TestRequestPageKeepsUpstreamStop(t *testing.T) {
	pageHome(t)
	now := time.Now()
	// the reporter's row, as /v1/uni-switch/usage/requests gave it, with the
	// stop reason the gateway wrote for it
	Append(Record{RouteID: 1791852707775, Time: now, Agent: "dsh", Provider: "opendesign", Model: "deepseek-v4.1-flash", Requested: "opendesign/deepseek-v4.1-flash", Served: "deepseek-flash",
		Input: 2724, Output: 777, CacheRead: 480128, Reasoning: 738, Effort: "max", Millis: 18832, TTFT: 12202, FirstText: 12202, Flow: 13, Status: 200,
		RequestID: "baa54a5099bcb72e7f9c990dafc66870", ResponseID: "dcab2089-548f-4915-8adf-5fc2382ee273", Endpoint: "/v1/responses", Stop: "completed"})
	// one whose upstream said no reason
	Append(Record{RouteID: 1791852707776, Time: now.Add(time.Second), Agent: "dsh", Provider: "opendesign", Model: "deepseek-v4.1-flash", Status: 200, Endpoint: "/v1/responses"})
	for repeat := 0; repeat < 2; repeat++ { // read, then from the cache
		page := QueryPage(All, Filter{}, 0, 100)
		byRoute := map[int64]Row{}
		for _, r := range page.Rows {
			byRoute[r.RouteID] = r
		}
		if r, ok := byRoute[1791852707775]; !ok || r.Stop != "completed" || r.ResponseID != "dcab2089-548f-4915-8adf-5fc2382ee273" {
			t.Fatalf("read %d: the upstream's stop reason was dropped from the page's row: %q (%+v)", repeat, r.Stop, page.Rows)
		}
		if r := byRoute[1791852707776]; r.Stop != "" {
			t.Fatalf("read %d: a call whose upstream said no reason reads %q", repeat, r.Stop)
		}
		// and in the page's CSV export
		var b strings.Builder
		if err := WriteCSV(&b, []Row{byRoute[1791852707775]}); err != nil {
			t.Fatal(err)
		}
		cells, err := csv.NewReader(strings.NewReader(b.String())).ReadAll()
		col := slices.Index(CSVHeader, "upstream_stop")
		if err != nil || col < 0 || len(cells) != 2 || cells[1][col] != "completed" {
			t.Fatalf("read %d: CSV: %v %q", repeat, err, b.String())
		}
	}
	c := &rowChunk{}
	c.add(Row{Record: Record{Time: now, Stop: "tool_calls", ParentAgent: "a0"}}, "msg", 1, false)
	for _, chunk := range []*rowChunk{c, c.pack().unpack()} {
		if r := chunk.row(0); r.Stop != "tool_calls" || r.ParentAgent != "a0" || chunk.Strings[chunk.text(&chunk.Rows[0], rowMsg)] != "msg" {
			t.Fatalf("packed and read back: stop %q, parent %q, msg %q", r.Stop, r.ParentAgent, chunk.Strings[chunk.text(&chunk.Rows[0], rowMsg)])
		}
	}
}

// Every string of a call's record comes back from the packed row cache as
// it went in: the stop reason (#1927) and the service tiers (#1784) were
// each added to Record without it, and every row the Requests page and
// /v1/uni-switch/usage/requests read came back with them empty. Computer is
// the chunk's, Source is the row's own (Row.Source, which the record's is
// read back from only when it is "reported"), and the bodies are never
// logged.
func TestRowTextHoldsEveryString(t *testing.T) {
	var in Row
	v := reflect.ValueOf(&in.Record).Elem()
	var names []string
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.Type.Kind() != reflect.String || !f.IsExported() || f.Tag.Get("json") == "-" || f.Name == "Computer" || f.Name == "Source" {
			continue
		}
		v.Field(i).SetString("v-" + f.Name)
		names = append(names, f.Name)
	}
	in.Time = time.Now()
	c := &rowChunk{}
	c.add(in, "", 1, false)
	for _, chunk := range []*rowChunk{c, c.pack().unpack()} {
		out := reflect.ValueOf(chunk.row(0).Record)
		for _, n := range names {
			if got := out.FieldByName(n).String(); got != "v-"+n {
				t.Errorf("Record.%s comes back from the packed row as %q: add it to rowText", n, got)
			}
		}
	}
}
