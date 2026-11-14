package imagemcp

import "testing"

// Started for an agent in WSL (#900), the paths it gives are opened on
// Windows' side of the distro, and those answered are the distro's again.
func TestWSLPaths(t *testing.T) {
	w, err := options([]string{"--wsl", "Ubuntu", "--home", "/home/me"})
	if err != nil || w.distro != "Ubuntu" || w.mount != "/mnt/" || w.home != "/home/me" {
		t.Fatalf("options: %+v %v", w, err)
	}
	w.share = `\\wsl.localhost\`
	for in, want := range map[string]string{
		"/home/me/app":      `\\wsl.localhost\Ubuntu\home\me\app`,
		"~/app/hero.png":    `\\wsl.localhost\Ubuntu\home\me\app\hero.png`,
		"/mnt/d/work/a.png": `D:\work\a.png`,
		"/mnt/c":            `C:\`,
		"/mnt/wsl/x":        `\\wsl.localhost\Ubuntu\mnt\wsl\x`,
		"assets/hero.png":   "assets/hero.png",
		`D:\work\a.png`:     `D:\work\a.png`,
	} {
		if got := w.local(in); got != want {
			t.Errorf("local(%s) = %s, want %s", in, got, want)
		}
	}
	for in, want := range map[string]string{
		`\\wsl.localhost\Ubuntu\home\me\app\generated-images\a.png`: "/home/me/app/generated-images/a.png",
		`\\wsl$\ubuntu\home\me\a.png`:                               "/home/me/a.png",
		`D:\work\a.png`:                                             "/mnt/d/work/a.png",
		`\\wsl.localhost\Debian\home\x.png`:                         `\\wsl.localhost\Debian\home\x.png`,
	} {
		if got := w.shown(in); got != want {
			t.Errorf("shown(%s) = %s, want %s", in, got, want)
		}
	}
	// a distro mounting the drives elsewhere
	w, _ = options([]string{"--wsl", "Ubuntu", "--mount", "/win"})
	if got := w.local("/win/e/x.png"); got != `E:\x.png` {
		t.Errorf("custom mount: %s", got)
	}
	if got := w.shown(`E:\x.png`); got != "/win/e/x.png" {
		t.Errorf("custom mount shown: %s", got)
	}
	for _, bad := range [][]string{{"--wsl"}, {"--home", "/home/me"}, {"--nope", "x"}} {
		if _, err := options(bad); err == nil {
			t.Errorf("options(%v) took it", bad)
		}
	}
	if w, err := options(nil); w != nil || err != nil {
		t.Errorf("no options: %+v %v", w, err)
	}
}
