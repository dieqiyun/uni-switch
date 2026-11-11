//go:build darwin && cgo && !nogui

package gui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Command-Q on the Mac is the app menu's Quit, which AppKit sends to
// terminate: and which never reached uni-switch's question (Aisiverto on X).
// askingQuit puts uni-switch's own item in its place, with Command-Q, and no
// item with the Quit role is left to send terminate:.
func TestCommandQAsksFirst(t *testing.T) {
	out, err := runAppKit(t, 30*time.Second, []string{"UNI_SWITCH_TEST_QUIT_MENU=1"})
	if err != nil || !strings.Contains(string(out), "quit menu: ok") {
		t.Fatalf("quit menu: %v\n%s", err, out)
	}
}

func init() {
	if os.Getenv("UNI_SWITCH_TEST_QUIT_MENU") != "1" {
		return
	}
	fail := func(format string, a ...any) {
		fmt.Printf("quit menu: "+format+"\n", a...)
		os.Exit(1)
	}
	application.New(application.Options{Name: "uni-switch"})
	menu := application.DefaultApplicationMenu()
	if menu.FindByRole(application.Quit) == nil {
		fail("the default app menu has no Quit to replace; the cause isn't what this test is about")
	}
	item := askingQuit(menu, "Quit uni-switch", func() {})
	if item == nil {
		fail("no item was put in the app menu")
	}
	if menu.FindByRole(application.Quit) != nil {
		fail("the app menu still has the Quit role, which sends terminate: past the question")
	}
	if item.Label() != "Quit uni-switch" || item.GetAccelerator() == "" {
		fail("the item is %q with accelerator %q, want Quit uni-switch with Command-Q", item.Label(), item.GetAccelerator())
	}
	sub := menu.FindByRole(application.AppMenu).GetSubmenu()
	var last *application.MenuItem
	for i := 0; sub.ItemAt(i) != nil; i++ {
		last = sub.ItemAt(i)
	}
	if last != item {
		fail("the item isn't last in the app menu, where Quit was")
	}
	fmt.Println("quit menu: ok")
	os.Exit(0)
}
