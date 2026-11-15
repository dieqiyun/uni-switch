package library

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

// A check keeps each tool's name and what it says it does, for the
// Library page to list (nf1qaz2w on Discord: "只能看到有几个工具，看不到具体
// 的工具名和描述"). The reply is the one @modelcontextprotocol/server-filesystem
// gave to tools/list, byte for byte.
func TestCheckKeepsToolNames(t *testing.T) {
	file, err := filepath.Abs("testdata/filesystem-tools-list.json")
	if err != nil {
		t.Fatal(err)
	}
	h := CheckServer(context.Background(), fakeStdio(t, "ok", map[string]string{"UNI_SWITCH_FAKE_MCP_TOOLS": file}))
	if h.State != "ok" || h.Tools != 14 || len(h.ToolList) != 14 {
		t.Fatalf("got %+v, want ok with 14 tools, each listed", h)
	}
	want := map[int]Tool{
		0:  {"read_file", "Read the complete contents of a file as text. DEPRECATED: Use read_text_file instead."},
		13: {"list_allowed_directories", "Returns the list of directories that this server is allowed to access. Subdirectories within these allowed directories are also accessible. Use this to understand which directories and their nested paths are available before trying to access files."},
	}
	for i, w := range want {
		if h.ToolList[i] != w {
			t.Errorf("tool %d: got %+v, want %+v", i, h.ToolList[i], w)
		}
	}
	// the longest, 457 characters, whole
	if d := h.ToolList[1].Description; h.ToolList[1].Name != "read_text_file" || utf8.RuneCountInString(d) != 457 || strings.HasSuffix(d, "…") {
		t.Errorf("read_text_file: %+v", h.ToolList[1])
	}
}

// A tool with no description, one with only a title, one whose description
// runs on, and the pages tools/list is split into (nextCursor) all come
// through, in the JSON the page reads.
func TestCheckToolPages(t *testing.T) {
	long := strings.Repeat("word ", 200)
	var cursors []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			return
		}
		var m struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
			Params struct {
				Cursor string `json:"cursor"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&m)
		w.Header().Set("Content-Type", "application/json")
		switch m.Method {
		case "initialize":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}}`, *m.ID)
		case "tools/list":
			cursors = append(cursors, m.Params.Cursor)
			switch m.Params.Cursor {
			case "":
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[`+
					`{"name":"search","description":"Search the web.\n\nArgs:\n  q: the query","inputSchema":{"type":"object"}},`+
					`{"name":"ping","inputSchema":{"type":"object"}},`+
					`{"name":"fetch","title":"Fetch a page","description":"","inputSchema":{"type":"object"}}],"nextCursor":"p2"}}`, *m.ID)
			case "p2":
				fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"long","description":%q,"inputSchema":{"type":"object"}}]}}`, *m.ID, long)
			}
		default:
			w.WriteHeader(202)
		}
	}))
	defer srv.Close()
	h := CheckServer(context.Background(), &Server{Name: "r", Transport: "http", URL: srv.URL})
	if h.State != "ok" || h.Tools != 4 || len(h.ToolList) != 4 {
		t.Fatalf("got %+v, want ok with 4 tools over 2 pages", h)
	}
	if strings.Join(cursors, ",") != ",p2" {
		t.Errorf("cursors asked for: %q", cursors)
	}
	for i, w := range []Tool{{"search", "Search the web."}, {"ping", ""}, {"fetch", "Fetch a page"}} {
		if h.ToolList[i] != w {
			t.Errorf("tool %d: got %+v, want %+v", i, h.ToolList[i], w)
		}
	}
	if d := h.ToolList[3].Description; h.ToolList[3].Name != "long" || !strings.HasPrefix(d, "word word") || !strings.HasSuffix(d, "…") || utf8.RuneCountInString(d) > maxToolDesc+1 {
		t.Errorf("long: %q (%d runes)", d, utf8.RuneCountInString(d))
	}
	b, _ := json.Marshal(h)
	if !strings.Contains(string(b), `"toolList":[{"name":"search","description":"Search the web."},{"name":"ping"},`) {
		t.Errorf("JSON: %s", b)
	}
}

// A server that names a next page forever is asked maxToolPages times.
func TestCheckToolPagesEnd(t *testing.T) {
	var lists atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&m)
		w.Header().Set("Content-Type", "application/json")
		switch m.Method {
		case "initialize":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18"}}`, *m.ID)
		case "tools/list":
			n := lists.Add(1)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"t%d"}],"nextCursor":"c%d"}}`, *m.ID, n, n)
		default:
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(202)
		}
	}))
	defer srv.Close()
	h := CheckServer(context.Background(), &Server{Name: "r", Transport: "http", URL: srv.URL})
	if h.State != "ok" || h.Tools != maxToolPages || int(lists.Load()) != maxToolPages {
		t.Fatalf("got %+v after %d pages, want ok with %d", h, lists.Load(), maxToolPages)
	}
}
