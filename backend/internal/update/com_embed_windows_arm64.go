//go:build comlauncher

package update

import _ "embed"

// The launcher uni-switch.com is made of (com.go), built for windows/arm64 by
// `make release-windows` before the app; not kept in git.
//
//go:embed launcher-arm64.com
var launcherarm64 []byte

func init() { comLauncher = launcherarm64 }
