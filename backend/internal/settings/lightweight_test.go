package settings

import "testing"

// Lightweight mode (#580) is off for a user who never chose (the owner,
// 10-21: too slow to have by default), on for one who turned it on, and
// kept off for one who turned it off; it is saved and read back.
func TestLightweightKept(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if Load().LightweightOn() {
		t.Fatal("on for a user who never chose")
	}
	off, on := false, true
	s := Load()
	s.Lightweight = &on
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if !Load().LightweightOn() {
		t.Fatal("turned on, but read back off")
	}
	s.Lightweight = &off
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if Load().LightweightOn() {
		t.Fatal("turned off, but read back on")
	}
	s.Theme = "dark" // another setting saved keeps it off
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if Load().LightweightOn() {
		t.Fatal("another setting's save turned it on")
	}
}

// Keep awake (xiao_wang24004 on X) is off by default, saved, and this
// computer's own.
func TestKeepAwakeKept(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if Load().KeepAwake {
		t.Fatal("on by default")
	}
	s := Load()
	s.KeepAwake, s.KeepAwakeDisplay = true, true
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if l := Load(); !l.KeepAwake || !l.KeepAwakeDisplay {
		t.Fatal("not kept")
	}
}
