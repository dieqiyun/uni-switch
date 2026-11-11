package iconproxy

import (
	"net/url"
	"testing"
)

func TestURLCarriesThePictureWhole(t *testing.T) {
	t.Setenv("UNI_SWITCH_ICON_PROXY", "https://proxy.example/icon")
	pic := "https://github.com/uni-switch-community.png?size=96&x=a b#c"
	got := URL(pic)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme+"://"+u.Host+u.Path != "https://proxy.example/icon" {
		t.Fatalf("URL(%q) = %q, not on configured proxy", pic, got)
	}
	if q := u.Query(); len(q) != 1 || q.Get("url") != pic {
		t.Fatalf("URL(%q) = %q: query %v", pic, got, q)
	}
}

func TestURLUsesOriginalAddressWithoutAProxy(t *testing.T) {
	t.Setenv("UNI_SWITCH_ICON_PROXY", "")
	for u, want := range map[string]string{
		"https://github.com/dieqiyun/uni-switch/partners/x.png": "https://github.com/dieqiyun/uni-switch/partners/x.png",
		"https://images.example/x.png":                          "https://images.example/x.png",
		"https://images.example/?source=uni-switch":             "https://images.example/?source=uni-switch",
	} {
		if got := URL(u); got != want {
			t.Errorf("URL(%q) = %q, want %q", u, got, want)
		}
	}
}
