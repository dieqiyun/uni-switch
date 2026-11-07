package main

import (
	"context"
	"github.com/dieqiyun/uni-switch/backend/internal/accounts"
	"github.com/dieqiyun/uni-switch/backend/internal/claudesub"
	"github.com/dieqiyun/uni-switch/backend/internal/localgateway"
	"os"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/rpc"
)

func main() {
	if accounts.CaptureBrowser(os.Args[1:]) {
		return
	}
	defer accounts.CloseAll()
	defer claudesub.CloseAll()
	defer localgateway.CloseAll(context.Background())
	// stdout is reserved for protocol frames; never print raw errors or secrets.
	if len(os.Args) == 2 && os.Args[1] == "--gateway" {
		if err := gateway.ServePipe(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := rpc.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}
