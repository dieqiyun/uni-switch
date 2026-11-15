// Package all registers every built-in plugin. Only uni-switch's main imports
// it: core never imports a plugin, and a plugin never imports another.
package all

import (
	_ "github.com/dieqiyun/uni-switch/backend/plugins/budget"
	_ "github.com/dieqiyun/uni-switch/backend/plugins/gateway"
	_ "github.com/dieqiyun/uni-switch/backend/plugins/library"
	_ "github.com/dieqiyun/uni-switch/backend/plugins/rotation"
	_ "github.com/dieqiyun/uni-switch/backend/plugins/search"
	_ "github.com/dieqiyun/uni-switch/backend/plugins/sessions"
	_ "github.com/dieqiyun/uni-switch/backend/plugins/tools"
	_ "github.com/dieqiyun/uni-switch/backend/plugins/warmups"
)
