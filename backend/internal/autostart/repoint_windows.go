package autostart

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// repoint rewrites the uni-switch Run value when it opens from. Task Manager's
// switch for it is kept by the value's name, which stays.
func repoint(from, to string) error {
	k, err := openRunKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer k.Close()
	v, err := readRunValue(k)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	nv, ok := repointed(v, from, to)
	if !ok {
		return nil
	}
	return k.SetStringValue(name, nv)
}
