package modelmeta

import (
	"encoding/json"
	"testing"
)

func TestBatchOwnsFinalMergeAndDoesNotTrustPreviousResolvedClaims(t *testing.T) {
	c := catalog(t, `{"openai":{"models":{"fixture":{"id":"fixture","limit":{"context":128000,"output":8192},"modalities":{"input":["text","image"]},"reasoning_options":[{"type":"effort","values":["low","high"]}]}}}}`)
	raw := json.RawMessage(`{"id":"fixture","officialProfile":{"contextWindow":90000000},"officialCapabilities":{"imageInput":true},"metadata":{"vendor":{"id":"fixture","images":false,"inputs":["text"],"apis":["responses"]},"profile":{"contextWindow":80000000}},"profileOverrides":{"contextWindow":512000,"maxOutputTokens":1000000,"endpoints":{"responses":false,"chatCompletions":true}},"capabilityOverrides":{"imageInput":false}}`)
	got, e := (Batch{BaseURL: "https://relay.invalid/v1", ProviderContext: 256000, Models: []json.RawMessage{raw}}).Resolve(c)
	if e != nil || len(got) != 1 {
		t.Fatal(got, e)
	}
	r := got[0]
	if r.Profile["contextWindow"] != int64(512000) || r.PublishedOutput != 512000 || r.Sources["contextWindow"] != "user:model" || r.Capabilities["imageInput"] != false {
		t.Fatal(r)
	}
	ep := object(r.Profile["endpoints"])
	if ep["responses"] != false || ep["chatCompletions"] != true || r.Sources["endpoints"] != "user:transport" {
		t.Fatal(r)
	}
	// No persisted previous result or supplied official guess participates in
	// the next result once explicit preferences have been removed.
	var model map[string]any
	if e := json.Unmarshal(raw, &model); e != nil {
		t.Fatal(e)
	}
	delete(model, "profileOverrides")
	delete(model, "capabilityOverrides")
	next, _ := json.Marshal(model)
	got, e = (Batch{Models: []json.RawMessage{next}}).Resolve(c)
	if e != nil || got[0].Profile["contextWindow"] != int64(128000) || got[0].Capabilities["imageInput"] != false {
		t.Fatal(got, e)
	}
}
