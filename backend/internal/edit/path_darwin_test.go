package edit

import "testing"

func TestMacOSSystemAliasesRequireExactPrivateTargets(t *testing.T) {
	for _, path := range []string{"/var", "/tmp", "/etc"} {
		if !systemAlias(path) {
			t.Fatal("macOS system alias was rejected", path)
		}
	}
	for _, path := range []string{"/Users", "/private/var", "/tmp/custom", "/var/folders/custom", "/var/../tmp"} {
		if systemAlias(path) {
			t.Fatal("non-system alias was trusted", path)
		}
	}
}
