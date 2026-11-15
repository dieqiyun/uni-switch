package library

import "testing"

// A server taken out of the library takes the icon it was added with (#300),
// unless another server in it runs the same thing.
func TestRemoveServerDropsItsIcon(t *testing.T) {
	sandbox(t)
	const key = "npx @playwright/mcp"
	icon := func() string {
		t.Helper()
		l, err := load()
		if err != nil {
			t.Fatal(err)
		}
		return l.Icons[key]
	}
	ok(t)(InstallServer("playwright", nil, nil))
	if icon() == "" {
		t.Fatal("added without its icon")
	}
	ok(t)(SaveServer("", Server{Name: "pw", Transport: "stdio", Command: "npx", Args: []string{"@playwright/mcp"}}))
	ok(t)(RemoveServer("playwright"))
	if icon() == "" {
		t.Fatal("icon dropped while pw still runs it")
	}
	ok(t)(RemoveServer("pw"))
	if u := icon(); u != "" {
		t.Fatalf("icon left behind: %q", u)
	}
}
