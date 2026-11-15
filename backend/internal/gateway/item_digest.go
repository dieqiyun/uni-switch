package gateway

import "crypto/sha256"

// itemSum identifies an input item in a local Codex session.
type itemSum [sha256.Size]byte

func sumsEqual(a, b []itemSum) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
