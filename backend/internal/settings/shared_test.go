package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// writeRaw puts b in settings.json as another program would, no .bak
// kept, with a time of its own so the file is seen as changed.
func writeRaw(t *testing.T, b string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(Path()), 0o755)
	if err := os.WriteFile(Path(), []byte(b), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(time.Duration(len(b)) * time.Second)
	os.Chtimes(Path(), at, at)
}

// A gateway request asks for the settings a dozen times over; parsing
// settings.json for each was ~800 MB a 10-minute profile. An unchanged
// file is parsed once, by Load and by Shared, and a change is read.
func TestUnchangedSettingsParsedOnce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeRaw(t, `{"proxy":"direct","modelImages":{"a/m":true},"fetchPrivate":["10.0.0.1"]}`)
	Load()
	n := Parses()
	for range 100 {
		if Load().Proxy != "direct" || Shared().Proxy != "direct" {
			t.Fatal("read the wrong settings")
		}
		LoadChecked()
	}
	if got := Parses() - n; got != 0 {
		t.Fatalf("an unchanged settings.json was parsed %d more times in 300 reads", got)
	}
	writeRaw(t, `{"proxy":"http://127.0.0.1:7890"}`)
	if Load().Proxy != "http://127.0.0.1:7890" || Shared().Proxy != "http://127.0.0.1:7890" {
		t.Fatalf("a change is not read: %q %q", Load().Proxy, Shared().Proxy)
	}
	if Shared().ModelImages != nil {
		t.Fatal("the old file's settings are still shared")
	}
}

// What Load gives is the caller's to change: a write into its maps or
// slices reaches neither the next Load nor Shared.
func TestLoadIsTheCallersOwn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeRaw(t, `{"modelImages":{"a/m":true},"pickedModels":{"codex":["a/m"]},"fetchPrivate":["10.0.0.1"],"refusalRetries":2}`)
	s := Load()
	s.ModelImages["a/m"] = false
	s.ModelImages["b/m"] = true
	s.PickedModels["codex"][0] = "changed"
	s.FetchPrivate[0] = "changed"
	*s.RefusalRetries = 9
	for name, got := range map[string]Settings{"Load": Load(), "Shared": Shared()} {
		if !got.ModelImages["a/m"] || len(got.ModelImages) != 1 || got.PickedModels["codex"][0] != "a/m" ||
			got.FetchPrivate[0] != "10.0.0.1" || *got.RefusalRetries != 2 {
			t.Errorf("%s after a caller changed Load's: %+v %+v %v %d", name, got.ModelImages, got.PickedModels, got.FetchPrivate, *got.RefusalRetries)
		}
	}
}

// Shared's slices are held at their length: a caller appending to one
// gets a new array and leaves the next caller's alone.
func TestSharedAppendReallocates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeRaw(t, `{"fetchPrivate":["10.0.0.1","10.0.0.2","10.0.0.3"],"codexWarmAts":["7:00","6:00"]}`)
	a := append(Shared().FetchPrivate, "evil")
	b := append(Shared().FetchPrivate, "other")
	c := append(Shared().CodexWarmAts, "23:00")
	if a[3] != "evil" || len(Shared().FetchPrivate) != 3 || c[2] != "23:00" {
		t.Fatalf("appends reached the shared list: %v %v %v", a, b, Shared().FetchPrivate)
	}
}

// A file that can't be read or parsed is unknown, a memo of the file read
// before notwithstanding: LoadChecked says so, and Shared is Load's
// defaults, not the old file's settings.
func TestUnreadableSettingsNotTheLastParsed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeRaw(t, `{"corsOrigins":["10.0.0.1"]}`)
	if _, err := LoadChecked(); err != nil {
		t.Fatal(err)
	}
	Shared()
	writeRaw(t, `{"corsOrigins":["10.0.0.1"`)
	if _, err := LoadChecked(); err == nil {
		t.Fatal("a file that doesn't parse read as known")
	}
	if got := Shared().CORSOrigins; got != nil {
		t.Fatalf("Shared kept the last file's list: %v", got)
	}
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		return
	}
	writeRaw(t, `{"corsOrigins":["10.0.0.9"]}`)
	if _, err := LoadChecked(); err != nil {
		t.Fatal(err)
	}
	os.Chmod(Path(), 0)
	t.Cleanup(func() { os.Chmod(Path(), 0o600) })
	at := time.Now().Add(time.Hour)
	os.Chtimes(Path(), at, at)
	if _, err := LoadChecked(); err == nil {
		t.Fatal("an unreadable file read as known")
	}
}

// clone copies every field: each map, slice and pointer of a Settings
// with all of them set is a new one, and the copy is equal. A field of a
// kind clone can't copy (an unexported field in a struct it holds)
// panics here first.
func TestCloneSharesNothing(t *testing.T) {
	var s Settings
	fill(reflect.ValueOf(&s).Elem())
	c := s.clone()
	if !reflect.DeepEqual(s, c) {
		t.Fatal("the clone differs")
	}
	apart(t, "Settings", reflect.ValueOf(s), reflect.ValueOf(c))
}

// fill sets every field of v, each map and slice with one entry.
func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fill(k)
		fill(e)
		m.SetMapIndex(k, e)
		v.Set(m)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fill(s.Index(0))
		v.Set(s)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fill(p.Elem())
		v.Set(p)
	case reflect.Struct:
		for i := range v.NumField() {
			fill(v.Field(i))
		}
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int64, reflect.Int32:
		v.SetInt(1)
	case reflect.Float64, reflect.Float32:
		v.SetFloat(1)
	}
}

// apart fails when a and b share a map, slice or pointer anywhere.
func apart(t *testing.T, path string, a, b reflect.Value) {
	t.Helper()
	switch a.Kind() {
	case reflect.Map:
		if a.UnsafePointer() == b.UnsafePointer() {
			t.Errorf("%s is shared", path)
		}
		it := a.MapRange()
		for it.Next() {
			apart(t, path+"[]", it.Value(), b.MapIndex(it.Key()))
		}
	case reflect.Slice:
		if a.UnsafePointer() == b.UnsafePointer() {
			t.Errorf("%s is shared", path)
		}
		for i := range a.Len() {
			apart(t, path+"[i]", a.Index(i), b.Index(i))
		}
	case reflect.Pointer:
		if a.Pointer() == b.Pointer() {
			t.Errorf("%s is shared", path)
		}
		apart(t, path, a.Elem(), b.Elem())
	case reflect.Struct:
		for i := range a.NumField() {
			apart(t, path+"."+a.Type().Field(i).Name, a.Field(i), b.Field(i))
		}
	}
}
