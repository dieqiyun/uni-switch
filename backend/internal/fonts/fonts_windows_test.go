//go:build windows && !nogui

package fonts

import "testing"

func TestWindowsFontNamesAndWeights(t *testing.T) {
	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range list {
		if family.Name != "Segoe UI" {
			continue
		}
		for _, face := range family.Styles {
			if face.Name == "Regular" && face.Weight == 400 && face.Style == "normal" && face.Stretch == 100 {
				return
			}
		}
		t.Fatalf("Segoe UI has no regular face: %#v", family)
	}
	t.Fatal("DirectWrite did not return the Windows UI font's family name")
}
