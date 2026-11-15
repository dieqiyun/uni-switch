package usage

import (
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// A row holds no pointer, so a slice of rows is one the collector never
// scans: its *time.Location made the pages' and kept buffers' rows a scan
// on every cycle (#4737).
func TestPackedRowsAreNotScanned(t *testing.T) {
	var walk func(reflect.Type, string)
	walk = func(ty reflect.Type, path string) {
		switch ty.Kind() {
		case reflect.Pointer, reflect.String, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.Interface, reflect.UnsafePointer:
			t.Errorf("packedRow%s is a %s: every []packedRow is scanned", path, ty.Kind())
		case reflect.Array:
			walk(ty.Elem(), path+"[]")
		case reflect.Struct:
			for i := range ty.NumField() {
				f := ty.Field(i)
				walk(f.Type, path+"."+f.Name)
			}
		}
	}
	walk(reflect.TypeOf(packedRow{}), "")
}

// A row's time keeps its zone through pack and unpack, and the times of
// a zone parsed again and again, a new FixedZone each, are one zone.
func TestRowZoneKeptThroughPack(t *testing.T) {
	before := len(*rowZones.list.Load())
	c := &rowChunk{}
	var want []time.Time
	base := time.Date(2026, 11, 1, 9, 0, 0, 123, time.UTC)
	for i := range 90 {
		var at time.Time
		switch i % 3 {
		case 0:
			s := base.Add(time.Duration(i) * time.Minute).In(time.FixedZone("", 5*3600+1800)).Format(time.RFC3339Nano)
			parsed, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				t.Fatal(err)
			}
			at = parsed
		case 1:
			at = base.Add(time.Duration(i) * time.Minute)
		default:
			at = base.Add(time.Duration(i) * time.Minute).Local()
		}
		want = append(want, at)
		c.add(Row{Record: Record{Time: at, Agent: "codex", Model: "m", Input: i}}, fmt.Sprint("msg", i), int64(i), false)
	}
	c.add(Row{Record: Record{Agent: "codex", Model: "m"}}, "zero", 90, false)
	want = append(want, time.Time{})
	packed := c.pack()
	if len(packed.Archive) == 0 {
		t.Fatal("the chunk wasn't packed")
	}
	for _, chunk := range []*rowChunk{c, packed.unpack()} {
		for i, w := range want {
			got := chunk.Rows[i].Time()
			if !got.Equal(w) || got.IsZero() != w.IsZero() {
				t.Fatalf("row %d: %v, want %v", i, got, w)
			}
			gn, go_ := got.Zone()
			wn, wo := w.Zone()
			if gn != wn || go_ != wo || got.Location().String() != w.Location().String() {
				t.Fatalf("row %d is in %q %d (%s), want %q %d (%s)", i, gn, go_, got.Location(), wn, wo, w.Location())
			}
		}
	}
	if grew := len(*rowZones.list.Load()) - before; grew > 1 {
		t.Fatalf("30 parses of +05:30 added %d zones, want 1 at most", grew)
	}
}

// BenchmarkCollectWithRowsHeld is a collection while a million rows are
// held, as a year's page holds them.
func BenchmarkCollectWithRowsHeld(b *testing.B) {
	rows := make([]packedRow, 1_000_000)
	for i := range rows {
		rows[i].setTime(time.Unix(int64(i), 0))
	}
	b.ResetTimer()
	for range b.N {
		runtime.GC()
	}
	b.StopTimer()
	runtime.KeepAlive(rows)
}
