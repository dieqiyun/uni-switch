//go:build nogui || (!windows && !darwin && !linux) || (!windows && !cgo)

package fonts

import "errors"

const Available = false

func installed() ([]Face, error) {
	return nil, errors.New("installed fonts are available only in the desktop app")
}
