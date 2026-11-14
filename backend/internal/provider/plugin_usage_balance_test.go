package provider

import (
	"testing"
	"time"
)

// Left is the least balance of the windows that count the model, and
// none when no window told one.
func TestAllowanceLeftIsTheLeastBalance(t *testing.T) {
	b := func(f float64) *float64 { return &f }
	now := time.Now()
	a := allowanceOf([]QuotaWindow{
		{Name: "Wallet", Balance: b(800), Unit: "pts"},
		{Name: "Opus wallet", Balance: b(30), Unit: "pts", Model: "opus"},
		{Name: "Daily", Used: 20, ResetSecs: 3600},
	}, now)
	if left, _, _ := a.Left("sonnet"); left != 800 {
		t.Errorf("sonnet's: %v", left)
	}
	if left, _, _ := a.Left("opus"); left != 30 {
		t.Errorf("opus's: %v", left)
	}
	if _, _, ok := allowanceOf([]QuotaWindow{{Name: "Daily", Used: 20}}, now).Left(""); ok {
		t.Error("no balance told, yet one is left")
	}
}
