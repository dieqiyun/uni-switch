package library

import (
	"testing"
)

func serverIn(t *testing.T, name string) *Server {
	t.Helper()
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range v.Servers {
		if s.Name == name {
			return s.Server
		}
	}
	return nil
}
