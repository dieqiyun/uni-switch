package update

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// lsregister is the tool that edits LaunchServices' list of apps; -u on a
// path takes that path off the list, whether or not it still exists.
const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

// unregister has LaunchServices forget an app at path, as best it can: a
// missing tool or a failure leaves things as they were, and never holds
// an update up for long. Tests stand in for it.
var unregister = func(path string) {
	if runtime.GOOS != "darwin" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	proc.CommandContext(ctx, lsregister, "-u", path).Run()
}

// forgotMarker, in uni-switch's cache, records that ForgetOldStaging has run.
const forgotMarker = "launchservices-forgot-staging"

// ForgetOldStaging has LaunchServices forget the apps updates before
// #3965 left on its list: the new version unpacked as
// .uni-switch-update/app/uni-switch.app and the old one moved aside to
// .uni-switch-update/old.app, beside bundle or in uni-switch's cache. Their
// folders are gone, but Spotlight listed each as another uni-switch. It runs
// once, and nothing waits on it.
func ForgetOldStaging(bundle string) {
	if runtime.GOOS != "darwin" || bundle == "" {
		return
	}
	marker := filepath.Join(appdir.Cache(), forgotMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	dirs := []string{filepath.Join(filepath.Dir(bundle), ".uni-switch-update")}
	if cache, err := appdir.SystemCache(); err == nil {
		dirs = append(dirs, filepath.Join(cache, "update"))
	}
	for _, d := range dirs {
		unregister(filepath.Join(d, "app", "uni-switch.app"))
		unregister(filepath.Join(d, "old.app"))
	}
	os.MkdirAll(filepath.Dir(marker), 0o755)
	os.WriteFile(marker, nil, 0o644)
}
