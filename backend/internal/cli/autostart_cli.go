package cli

import (
	"fmt"
	"runtime"

	"github.com/dieqiyun/uni-switch/backend/internal/autostart"
)

// uni-switch autostart [on|off]: whether uni-switch opens at login, in the tray.
func autostartCmd(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: uni-switch autostart [on|off]")
	}
	if len(args) == 1 {
		switch args[0] {
		case "on", "off":
			if err := autostart.Set(args[0] == "on"); err != nil {
				return err
			}
		default:
			return fmt.Errorf("usage: uni-switch autostart [on|off]")
		}
	}
	switch autostart.Enabled() {
	case autostart.NeedsAllowing:
		// the record is there, and Set(true) can't undo the user's switch
		fmt.Println("uni-switch is set to open at login, but it is switched off in System Settings › General › Login Items › Allow in the Background: switch it on there")
	case autostart.On:
		if runtime.GOOS == "android" {
			fmt.Println(green.Render("✓"), "uni-switch starts the gateway at boot (requires Termux:Boot)")
		} else {
			fmt.Println(green.Render("✓"), "uni-switch opens at login, in the tray")
		}
	default:
		fmt.Println("uni-switch doesn't open at login")
	}
	return nil
}
