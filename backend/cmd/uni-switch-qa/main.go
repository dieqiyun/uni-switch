// Isolated, opt-in QA launcher. Never distributed as the desktop core.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 || len(os.Args) > 3 || !filepath.IsAbs(os.Args[1]) {
		panic("usage: uni-switch-qa <absolute executable> [--background]")
	}
	home, err := testenv.Isolate()
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(home)
	defer testenv.RemovePrograms()
	os.Setenv("UNI_SWITCH_DATA_DIR", filepath.Join(home, "uni-switch"))
	os.Setenv("WEBVIEW2_USER_DATA_FOLDER", filepath.Join(home, "webview"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	os.Setenv("UNI_SWITCH_GATEWAY_ADDR", listener.Addr().String())
	listener.Close()
	// A desktop launch must leave a disconnected client's file untouched.
	os.MkdirAll(filepath.Join(home, ".codex"), 0o700)
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("# isolated QA; disconnected client\n"), 0o600)
	json.NewEncoder(os.Stdout).Encode(map[string]string{"type": "qa-home", "path": home})
	// Every non-loopback request is refused by a local, unused proxy during QA.
	os.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	os.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	os.Setenv("NO_PROXY", "127.0.0.1,localhost")
	args := []string{"__uni_switch_desktop"}
	if len(os.Args) == 3 {
		args = []string{os.Args[2]}
	}
	cmd := proc.Command(os.Args[1], args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
