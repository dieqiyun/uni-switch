package cli

import "github.com/dieqiyun/uni-switch/backend/internal/gui"

func uniSwitchDesktop() error { return gui.RunUniSwitchCore(version) }
