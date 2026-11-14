package settings

import (
	"reflect"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/fonts"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func TestFontChoicesPersistAndValidate(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	ui := &fonts.Face{Family: "HarmonyOS Sans SC", Name: "Medium", Weight: 500, Style: "normal", Stretch: 100}
	code := &fonts.Face{Family: "Consolas", Name: "Italic", Weight: 400, Style: "italic", Stretch: 100}
	if err := Save(Settings{UIFont: ui, CodeFont: code}); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if !reflect.DeepEqual(got.UIFont, ui) || !reflect.DeepEqual(got.CodeFont, code) {
		t.Fatalf("font choices lost: %#v %#v", got.UIFont, got.CodeFont)
	}
	bad := *ui
	bad.Weight = 1001
	if Save(Settings{UIFont: &bad}) == nil {
		t.Fatal("saved an invalid font weight")
	}
	if !reflect.DeepEqual(Load().UIFont, ui) {
		t.Fatal("a rejected choice overwrote the saved font")
	}
	if err := Save(Settings{}); err != nil {
		t.Fatal(err)
	}
	if Load().UIFont != nil || Load().CodeFont != nil {
		t.Fatal("default fonts did not persist")
	}
}
