//go:build !darwin

package sessions

import "errors"

func cloneFile(from, to string) error { return errors.ErrUnsupported }
