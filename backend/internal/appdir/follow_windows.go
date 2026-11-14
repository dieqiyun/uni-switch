package appdir

import (
	"golang.org/x/sys/windows/registry"
)

var osRegEnv = winRegEnv

// winRegEnv is name as Windows builds a new process's variables: the
// user's (HKCU\Environment) over the machine's (Session Manager's), a
// REG_EXPAND_SZ one with its %…% expanded. An empty value is none.
func winRegEnv(name string) (string, bool) {
	for _, k := range []struct {
		root registry.Key
		path string
	}{
		{registry.CURRENT_USER, `Environment`},
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
	} {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, typ, err := key.GetStringValue(name)
		key.Close()
		if err != nil {
			continue
		}
		if typ == registry.EXPAND_SZ {
			if x, err := registry.ExpandString(v); err == nil {
				v = x
			}
		}
		return v, v != ""
	}
	return "", false
}
