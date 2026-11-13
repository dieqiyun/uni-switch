package update

import (
	"strings"
	"testing"
)

// The script run as administrator sets what uni-switch's own environment
// adds (its proxy, the PATH npm's node goes first on), since an elevated
// process starts from the user's environment afresh; it keeps cmd.exe from
// reading a % as a variable, and passes npm's exit code on.
func TestElevatedScript(t *testing.T) {
	own := []string{"PATH=C:\\Windows", "USERPROFILE=C:\\Users\\me", "=C:=C:\\"}
	env := []string{"PATH=C:\\Program Files\\nodejs;C:\\Windows", "USERPROFILE=C:\\Users\\me", "=C:=C:\\",
		"HTTPS_PROXY=http://127.0.0.1:7890", "WEIRD=50%off", "BAD=a\"b", "NONINTERACTIVE=1"}
	got, err := elevatedScript(`C:\Users\me`, env, own, `C:\Temp\x\out.log`,
		[]string{`C:\Program Files\nodejs\npm.cmd`, "install", "-g", "--prefix", `C:\Program Files\nodejs`, "@google/gemini-cli@latest"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"set \"PATH=C:\\Program Files\\nodejs;C:\\Windows\"\r\n",
		"set \"HTTPS_PROXY=http://127.0.0.1:7890\"\r\n",
		"set \"WEIRD=50%%off\"\r\n",
		"set \"NONINTERACTIVE=1\"\r\n",
		"cd /d \"C:\\Users\\me\"\r\n",
		`call "C:\Program Files\nodejs\npm.cmd" "install" "-g" "--prefix" "C:\Program Files\nodejs" "@google/gemini-cli@latest" > "C:\Temp\x\out.log" 2>&1` + "\r\n",
		"exit /b %errorlevel%\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	for _, not := range []string{"USERPROFILE", "BAD", "=C:"} {
		if strings.Contains(got, "set \""+not) {
			t.Errorf("%s set:\n%s", not, got)
		}
	}
	if _, err := elevatedScript("", nil, nil, "log", []string{"npm", "a%PATH%b"}); err == nil {
		t.Error("a % passed to call")
	}
}
