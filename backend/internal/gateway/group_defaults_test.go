package gateway

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func setGroupDefaults(t *testing.T, sink bool, firstToken, patience int) {
	t.Helper()
	if err := settings.SetGroupDefaults(sink, firstToken, patience); err != nil {
		t.Fatal(err)
	}
}
