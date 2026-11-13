//go:build darwin && cgo && !nogui

package fonts

import "testing"

// Built-in fonts are installed without having been downloaded. CoreText's
// downloaded=false attribute must not filter out the Mac's Helvetica.
func TestMacBundledFontIsAvailable(t *testing.T) {
	families, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.Name == "Helvetica" && len(family.Styles) > 0 {
			return
		}
	}
	t.Fatal("the installed collection did not include the bundled Helvetica family")
}
