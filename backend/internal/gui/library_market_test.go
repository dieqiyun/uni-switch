package gui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/library"
)

// A market search one source of which didn't answer tells the page which
// source and why, apart from the whole error, for it to say the list is
// short rather than that the search failed (#1589); any other error is the
// error alone.
func TestMarketOfNamesTheFailedSource(t *testing.T) {
	err := fmt.Errorf("x: %w", &library.SearchError{Source: "skills.sh", Err: errors.New("www.skills.sh answered 503 Service Unavailable")})
	m := marketOf([]int{1}, err)
	if m.Failed != "skills.sh" || m.Cause != "www.skills.sh answered 503 Service Unavailable" || m.Error != err.Error() {
		t.Fatalf("%+v", m)
	}
	if m := marketOf([]int{}, errors.New("disk full")); m.Failed != "" || m.Cause != "" || m.Error != "disk full" {
		t.Fatalf("%+v", m)
	}
	if m := marketOf([]int{}, nil); m.Failed != "" || m.Error != "" {
		t.Fatalf("%+v", m)
	}
}
