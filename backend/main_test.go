package main

import (
	"os"
	"strings"
	"testing"
)

func TestUniSwitchHasNoExtensionEngineDependency(t *testing.T) {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "github.com/MoeForge/moejs") {
		t.Fatal("third-party extension engine is still required")
	}
	if !strings.Contains(thirdParty, "Copyright") {
		t.Fatal("distribution must retain its third-party notices")
	}
}
