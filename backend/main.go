// uni-switch — one place to pick every agent's model. The commands live in
// internal/cli; this is only where the binary starts.
package main

import (
	_ "embed"

	"github.com/dieqiyun/uni-switch/backend/internal/cli"
	"github.com/dieqiyun/uni-switch/backend/internal/notices"
	// the built-in plugins, registered with the runtime (internal/pluginrt)
	_ "github.com/dieqiyun/uni-switch/backend/plugins/all"
)

// version is set at build time: -ldflags "-X main.version=…" (Makefile,
// Dockerfile, uni-switch-releases), so it stays in package main.
var version = "dev"

// the notices of what is compiled into every build (moejs's Intl data and
// code), carried by the binary itself, since most release assets are a
// bare binary
//
//go:embed THIRD_PARTY_NOTICES
var thirdParty string

func main() {
	notices.Text = thirdParty
	cli.Main(version)
}
