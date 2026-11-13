package appdir

import (
	"errors"
	"os"
	"path/filepath"
)

// uniSwitchRoot is set once, before the core starts any service. Keeping the
// application namespace here lets every imported subsystem use the same paths
// without redirecting the user's home or sharing another application's state.
var uniSwitchRoot string

func UseUniSwitchDirectory(directory string) error {
	if !filepath.IsAbs(directory) {
		return errors.New("uni-switch data directory must be absolute")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	uniSwitchRoot = filepath.Clean(directory)
	return nil
}
