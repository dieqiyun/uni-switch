package gui

import (
	"mime"
	"testing"
)

func TestAssetTypesHoldAgainstTheRegistry(t *testing.T) {
	for ext, want := range assetTypes {
		if got := mime.TypeByExtension(ext); got != want {
			t.Errorf("%s served as %q, not %q", ext, got, want)
		}
	}
}
