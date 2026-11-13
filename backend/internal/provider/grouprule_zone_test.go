package provider

import (
	"testing"
	"time"
)

// farZone is a zone whose clock is hours off this computer's at the week
// the tests use: Asia/Shanghai (gaubeebangeel on X: uni-switch on a server in
// UTC, its user in Beijing), or America/New_York on a computer that keeps
// Beijing's time, so a window read on the computer's clock is visibly wrong.
func farZone(t *testing.T) (string, *time.Location) {
	t.Helper()
	for _, name := range []string{"Asia/Shanghai", "America/New_York"} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Date(2026, 9, 28, 12, 0, 0, 0, loc)
		_, a := at.Zone()
		_, b := at.In(time.Local).Zone()
		if d := a - b; d >= 4*3600 || d <= -4*3600 {
			return name, loc
		}
	}
	t.Fatal("no zone far from this computer's")
	return "", nil
}
