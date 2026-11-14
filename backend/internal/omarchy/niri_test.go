package omarchy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The JSON below is niri's own, as niri-ipc 26.4.0 (niri v26.04,
// niri-ipc/src/lib.rs) serializes it with serde's defaults: Reply is a
// Result ({"Ok": …} / {"Err": "…"}), Response, Event, Action and
// PositionChange are externally tagged enums, Window's Option fields are
// null when unset and WindowLayout's pairs are arrays.

// panelJSON is a Window as niri-ipc's Window and WindowLayout have it.
func panelJSON(id, pid int, title string, floating bool) string {
	return `{"id":` + itoa(id) + `,"title":` + quote(title) + `,"app_id":"uni-switch","pid":` + itoa(pid) +
		`,"workspace_id":1,"is_focused":true,"is_floating":` + map[bool]string{true: "true", false: "false"}[floating] +
		`,"is_urgent":false,"layout":{"pos_in_scrolling_layout":null,"tile_size":[440.0,520.0],"window_size":[440,520],` +
		`"tile_pos_in_workspace_view":[633.0,273.5],"window_offset_in_tile":[0.0,0.0]},"focus_timestamp":{"secs":12,"nanos":5000}}`
}

// focusedOutput is niri's reply to "FocusedOutput": the reporter's 1.5
// scale, a 2560x1600 panel at 1707x1067 logical.
const focusedOutput = `{"Ok":{"FocusedOutput":{"name":"eDP-1","make":"BOE","model":"0x0BCA","serial":null,` +
	`"physical_size":[300,190],"modes":[{"width":2560,"height":1600,"refresh_rate":60000,"is_preferred":true}],` +
	`"current_mode":0,"is_custom_mode":false,"vrr_supported":false,"vrr_enabled":false,` +
	`"logical":{"x":0,"y":0,"width":1707,"height":1067,"scale":1.5,"transform":"Normal"}}}}`

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestPanelSpot(t *testing.T) {
	for _, c := range []struct {
		name   string
		click  int
		known  bool
		x, y   float64
		width  float64
		output float64
	}{
		{"under the icon", 1200, true, 1200 - 220, 6, 440, 1707},
		{"icon near the right edge", 1690, true, 1707 - 6 - 440, 6, 440, 1707},
		{"icon near the left edge", 30, true, 6, 6, 440, 1707},
		{"not clicked: top right", 0, false, 1707 - 6 - 440, 6, 440, 1707},
		{"a click off the output: top right", 2100, true, 1707 - 6 - 440, 6, 440, 1707},
		{"a negative click: top right", -4, true, 1707 - 6 - 440, 6, 440, 1707},
		{"an output narrower than the panel", 200, true, 6, 6, 440, 400},
		{"half pixels round", 1001, true, 781, 6, 441, 1707},
	} {
		x, y := panelSpot(c.click, c.known, c.width, c.output)
		if x != c.x || y != c.y {
			t.Errorf("%s: %v,%v; want %v,%v", c.name, x, y, c.x, c.y)
		}
	}
}

// The panel is placed once as it opens, whether it opened before the
// stream started or after, and not again while it stays open; other
// windows, another process's and the main window are left alone.
func TestWatchWindows(t *testing.T) {
	const pid = 4242
	stream := strings.Join([]string{
		`{"Ok":"Handled"}`,
		`{"WorkspacesChanged":{"workspaces":[]}}`,
		`{"WindowsChanged":{"windows":[` + panelJSON(3, pid, "uni-switch", false) + `,` + panelJSON(7, pid, "uni-switch panel", true) + `,` + panelJSON(8, 99, "uni-switch panel", true) + `]}}`,
		`{"WindowFocusChanged":{"id":7}}`,
		`{"WindowOpenedOrChanged":{"window":` + panelJSON(7, pid, "uni-switch panel", true) + `}}`,
		`{"WindowLayoutsChanged":{"changes":[[7,{"pos_in_scrolling_layout":null,"tile_size":[440.0,520.0],"window_size":[440,520],"tile_pos_in_workspace_view":[1000.0,6.0],"window_offset_in_tile":[0.0,0.0]}]]}}`,
		`{"WindowClosed":{"id":7}}`,
		`{"WindowOpenedOrChanged":{"window":` + panelJSON(9, pid, "uni-switch", false) + `}}`,
		`{"WindowOpenedOrChanged":{"window":` + panelJSON(9, pid, "uni-switch panel", false) + `}}`,
		`{"WindowOpenedOrChanged":{"window":` + panelJSON(9, pid, "uni-switch panel", false) + `}}`,
		`{"SomeEventNiriAddsLater":{"x":1}}`,
		`{"WindowsChanged":{"windows":[` + panelJSON(9, pid, "uni-switch panel", false) + `]}}`,
		``,
	}, "\n")
	var sent bytes.Buffer
	c := struct {
		io.Reader
		io.Writer
	}{strings.NewReader(stream), &sent}
	var placed []uint64
	if err := watchWindows(c, "uni-switch panel", pid, func(w niriWindow) { placed = append(placed, w.ID) }); err != io.EOF {
		t.Fatalf("watchWindows: %v, want EOF at the stream's end", err)
	}
	if sent.String() != "\"EventStream\"\n" {
		t.Fatalf("asked %q, want niri-ipc's Request::EventStream", sent.String())
	}
	if want := []uint64{7, 9}; len(placed) != 2 || placed[0] != want[0] || placed[1] != want[1] {
		t.Fatalf("placed %v, want %v", placed, want)
	}
}

func TestWatchWindowsRefused(t *testing.T) {
	c := struct {
		io.Reader
		io.Writer
	}{strings.NewReader(`{"Err":"unknown request"}` + "\n"), io.Discard}
	if err := watchWindows(c, "uni-switch panel", 1, func(niriWindow) { t.Fatal("placed") }); err == nil || !strings.Contains(err.Error(), "unknown request") {
		t.Fatalf("watchWindows: %v, want niri's error", err)
	}
}

// fakeNiri answers on NIRI_SOCKET as niri does, one request a line, and
// keeps the requests.
func fakeNiri(t *testing.T, answer func(req string) string) (sent func() []string) {
	dir, err := os.MkdirTemp("", "niri") // a socket path fits in 104 bytes
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "niri.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			sc := bufio.NewScanner(c)
			for sc.Scan() {
				mu.Lock()
				got = append(got, sc.Text())
				mu.Unlock()
				io.WriteString(c, answer(sc.Text())+"\n")
			}
			c.Close()
		}
	}()
	t.Cleanup(func() {
		l.Close()
		wg.Wait()
		os.RemoveAll(dir)
	})
	t.Setenv("NIRI_SOCKET", sock)
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

func TestPlacePanel(t *testing.T) {
	answer := func(req string) string {
		if req == `"FocusedOutput"` {
			return focusedOutput
		}
		return `{"Ok":"Handled"}`
	}
	x := 1200
	for _, c := range []struct {
		name     string
		click    *int
		floating bool
		want     []string
	}{
		{"clicked, already floating", &x, true, []string{
			`"FocusedOutput"`,
			`{"Action":{"MoveFloatingWindow":{"id":7,"x":{"SetFixed":980},"y":{"SetFixed":6}}}}`,
		}},
		{"opened by uni-switch panel, tiled", nil, false, []string{
			`"FocusedOutput"`,
			`{"Action":{"MoveWindowToFloating":{"id":7}}}`,
			`{"Action":{"MoveFloatingWindow":{"id":7,"x":{"SetFixed":1261},"y":{"SetFixed":6}}}}`,
		}},
	} {
		got := fakeNiri(t, answer)
		var w niriWindow
		if err := json.Unmarshal([]byte(panelJSON(7, 1, "uni-switch panel", c.floating)), &w); err != nil {
			t.Fatal(err)
		}
		var p NiriPanel
		p.Opening(c.click)
		p.place(w)
		if sent := strings.Join(got(), "\n"); sent != strings.Join(c.want, "\n") {
			t.Errorf("%s: sent\n%s\nwant\n%s", c.name, sent, strings.Join(c.want, "\n"))
		}
		// the click is used once: the next opening, not clicked, goes top right
		if _, known := p.take(); known {
			t.Errorf("%s: the click was kept for the next opening", c.name)
		}
	}
}

// A niri that can't say its focused output gets no move at all.
func TestPlacePanelNoOutput(t *testing.T) {
	got := fakeNiri(t, func(req string) string {
		if req == `"FocusedOutput"` {
			return `{"Ok":{"FocusedOutput":null}}`
		}
		return `{"Ok":"Handled"}`
	})
	var p NiriPanel
	p.place(niriWindow{ID: 7})
	if sent := got(); len(sent) != 1 {
		t.Fatalf("sent %q, want only the question", sent)
	}
}
