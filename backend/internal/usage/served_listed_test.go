package usage

import (
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// listedAs gives ListedAs these lists, provider by model id, for the
// test's length, and forgets every list read before.
func listedAs(t *testing.T, lists map[string]map[string]string) {
	t.Helper()
	t.Cleanup(ListedForTest(lists))
}

// Qoder's plugin lists its free Qwen model as qfmodel, named
// Qwen3.8-Flash, and answers under that name (plugins#412): the model
// asked for, not another one swapped in (jzing.draw on Discord, 10-27).
// Another name, another provider's list or another model of the same
// list is still a swap.
func TestListedNameIsTheModelAskedFor(t *testing.T) {
	listedAs(t, map[string]map[string]string{"qoder": {"qfmodel": "Qwen3.8-Flash", "dfmodel": "DeepSeek-V4.1-Flash", "auto": "auto"}})
	if !Swapped("qfmodel", "Qwen3.8-Flash") {
		t.Fatal("the names alone read alike; the list is what tells them apart")
	}
	for _, c := range [][3]string{{"qoder", "qfmodel", "Qwen3.8-Flash"}, {"qoder", "qfmodel", "qwen3.8-flash"}, {"qoder", "dfmodel", "DeepSeek-V4.1-Flash"}} {
		if !ListedAs(c[0], c[1], c[2]) {
			t.Errorf("%s/%s answered as %s is the model its list names", c[0], c[1], c[2])
		}
	}
	for _, c := range [][3]string{
		{"qoder", "qfmodel", "DeepSeek-V4.1-Flash"}, // another of its models
		{"qoder", "qfmodel", "Qwen3.7-Flash"},
		{"qoder", "auto", "auto"},             // a name no list gives apart from the id
		{"other", "qfmodel", "Qwen3.8-Flash"}, // a list that doesn't name it
		{"qoder", "nomodel", "Qwen3.8-Flash"},
		{"", "qfmodel", "Qwen3.8-Flash"},
	} {
		if ListedAs(c[0], c[1], c[2]) {
			t.Errorf("%s/%s answered as %s is another model", c[0], c[1], c[2])
		}
	}
}

// A provider's list is read once a minute at most, however many rows ask.
func TestListedNamesReadOnceAMinute(t *testing.T) {
	n := 0
	old := listedModels
	listedModels = func(string) map[string]string { n++; return map[string]string{"qfmodel": "Qwen3.8-Flash"} }
	forgetListed()
	t.Cleanup(func() { listedModels = old; forgetListed() })
	for range 1000 {
		ListedAs("qoder", "qfmodel", "Qwen3.8-Flash")
	}
	if n != 1 {
		t.Fatalf("read the list %d times", n)
	}
	listed.Lock()
	e := listed.by["qoder"]
	e.at = time.Now().Add(-listedFor - time.Second)
	listed.by["qoder"] = e
	listed.Unlock()
	ListedAs("qoder", "qfmodel", "Qwen3.8-Flash")
	if n != 2 {
		t.Fatalf("a list a minute old wasn't read again (%d reads)", n)
	}
}

// A row kept marked swapped for the name its provider's list gives the
// model is read back as not swapped; another model stays swapped.
func TestKeptRowListedNameNotSwapped(t *testing.T) {
	listedAs(t, map[string]map[string]string{"qoder": {"qfmodel": "Qwen3.8-Flash"}})
	c := &rowChunk{}
	c.add(Row{Record: Record{Provider: "qoder", Model: "qfmodel", Served: "Qwen3.8-Flash"}, Swapped: true}, "", 0, false)
	c.add(Row{Record: Record{Provider: "qoder", Model: "qfmodel", Served: "Qwen3.7-Flash"}, Swapped: true}, "", 1, false)
	for _, chunk := range []*rowChunk{c, c.pack().unpack()} {
		if chunk.row(0).Swapped || !chunk.row(1).Swapped {
			t.Fatalf("listed name %v, other model %v", chunk.row(0).Swapped, chunk.row(1).Swapped)
		}
	}
}

// A gateway call priced again reads the listed name as the model asked
// for, as the row does.
func TestBlockPricerListedNameNotSwapped(t *testing.T) {
	listedAs(t, map[string]map[string]string{"qoder": {"qfmodel": "Qwen3.8-Flash"}})
	b := newBlockPricer(provider.Renames{}, nil, func(Record) *catalog.Price { return nil })
	if a := b.as("qoder", "qfmodel", "Qwen3.8-Flash", ""); a.flags&2 != 0 {
		t.Fatalf("listed name priced as a swap: %b", a.flags)
	}
	if a := b.as("qoder", "qfmodel", "Qwen3.7-Flash", ""); a.flags&2 == 0 {
		t.Fatalf("another model priced as not swapped: %b", a.flags)
	}
}
