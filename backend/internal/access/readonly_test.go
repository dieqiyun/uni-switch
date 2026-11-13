package access

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A read-only key (Steven on Discord: a desk display's) is made so by
// add-key or access-key, is told so by Authenticate, and is made full
// again, including a key retained from a legacy sharing configuration.
func TestReadOnlyKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	display, err := Update("add-key", Change{Name: "Desk display", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	laptop, err := Update("add-key", Change{Name: "Laptop"})
	if err != nil {
		t.Fatal(err)
	}
	if who, ok := Authenticate(display); !ok || !who.ReadOnly {
		t.Fatal("added read-only:", who, ok)
	}
	who, ok := Authenticate(laptop)
	if !ok || who.ReadOnly {
		t.Fatal("added full:", who, ok)
	}
	id := who.KeyID
	if _, err := Update("access-key", Change{Key: id, ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if who, _ := Authenticate(laptop); !who.ReadOnly {
		t.Fatal("access-key read-only:", who)
	}
	keys, _ := List()
	if !keys[1].ReadOnly || keys[1].Secret != "" {
		t.Fatal("listed:", keys[1])
	}
	if _, err := Update("access-key", Change{Key: id}); err != nil {
		t.Fatal(err)
	}
	if who, _ := Authenticate(laptop); who.ReadOnly {
		t.Fatal("access-key full:", who)
	}

	s := settings.Load()
	s.LANKeyID = id
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if _, err := Update("access-key", Change{Key: id, ReadOnly: true}); err != nil {
		t.Fatal("the sharing key made read-only:", err)
	}
	if who, _ := Authenticate(laptop); !who.ReadOnly {
		t.Fatal("legacy local key was not made read-only:", who)
	}
	if _, err := Update("access-key", Change{Key: id}); err != nil {
		t.Fatal("the sharing key given full access:", err)
	}
}
