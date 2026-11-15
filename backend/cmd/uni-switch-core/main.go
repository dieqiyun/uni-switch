// uni-switch's own compiled core. Business implementations are imported as
// source in this module; this never launches or calls a uni-switch installation.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/cli"
	"github.com/dieqiyun/uni-switch/backend/internal/notices"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	_ "github.com/dieqiyun/uni-switch/backend/plugins/all"
)

var version = "0.5.29-dev"

//go:embed THIRD_PARTY_NOTICES
var thirdParty string

func main() {
	directory := os.Getenv("UNI_SWITCH_DATA_DIR")
	if directory == "" {
		if exe, err := os.Executable(); err == nil {
			b, _ := os.ReadFile(filepath.Join(filepath.Dir(exe), "data-root"))
			directory = strings.TrimSpace(string(b))
		}
	}
	if err := appdir.UseUniSwitchDirectory(directory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	notices.Text = thirdParty
	if len(os.Args) > 1 && os.Args[1] == "__uni_switch_desktop" {
		os.Unsetenv("UNI_SWITCH_ADDR")
		os.Unsetenv("UNI_SWITCH_PUBLIC_URL")
		if addr := os.Getenv("UNI_SWITCH_GATEWAY_ADDR"); addr != "" {
			os.Setenv("UNI_SWITCH_ADDR", addr)
		}
		if _, err := os.Stat(settings.Path()); os.IsNotExist(err) {
			s := settings.Load()
			s.Port, s.Lang, s.Theme = 18425, "zh", "light"
			s.NoAutoUpdate, s.NoBird = true, true
			if err := settings.Save(s); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		} else if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	// The imported updater must never replace our bundle with another product.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "update", "install", "uninstall":
			fmt.Fprintln(os.Stderr, "请通过 uni-switch 安装程序管理应用版本")
			os.Exit(1)
		}
	}
	cli.Main(version)
}
