package gui

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// Only OS-window operations cross the inherited pipes. All business APIs and
// inference traffic use the imported implementations in this compiled core.
type uniSwitchHost struct {
	quit     func()
	encode   *json.Encoder
	mu       sync.Mutex
	pending  map[uint64]chan string
	sequence atomic.Uint64
}

// Also marks optional browser-access pages served by this same application.
var uniSwitchApplication atomic.Bool

func (h *uniSwitchHost) send(v any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.encode.Encode(v)
}
func (h *uniSwitchHost) HidePanel()                     { h.send(map[string]string{"type": "host", "method": "hide"}) }
func (h *uniSwitchHost) ShowMain(string)                { h.send(map[string]string{"type": "host", "method": "show"}) }
func (h *uniSwitchHost) Quit()                          { h.quit() }
func (*uniSwitchHost) OpenURL(u string) error           { return proc.OpenBrowser(u) }
func (*uniSwitchHost) Copy(string) bool                 { return false }
func (*uniSwitchHost) FitPanel(int, Glide)              {}
func (*uniSwitchHost) TintPanel([4]uint8, int) bool     { return false }
func (*uniSwitchHost) TintTitleBar([4]uint8, bool) bool { return false }
func (*uniSwitchHost) SetTextSize(int)                  {}

func (h *uniSwitchHost) ChooseFolder(title string) (string, error) {
	id := h.sequence.Add(1)
	reply := make(chan string, 1)
	h.mu.Lock()
	h.pending[id] = reply
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.pending, id); h.mu.Unlock() }()
	if err := h.send(map[string]any{"type": "host", "method": "chooseFolder", "id": id, "title": title}); err != nil {
		return "", err
	}
	select {
	case path := <-reply:
		return path, nil
	case <-backendCtx.Done():
		return "", errors.New("应用正在退出")
	case <-time.After(10 * time.Minute):
		return "", errors.New("选择文件夹超时，请重试")
	}
}

func (*uniSwitchHost) OpenFolder(path string) error {
	switch runtime.GOOS {
	case "windows":
		return proc.Command("explorer.exe", path).Start()
	case "darwin":
		return proc.Command("open", "--", path).Run()
	default:
		return proc.Command("xdg-open", path).Run()
	}
}

// Distribution endpoints belong to our desktop bundle. Provider, plugin,
// agent, model, protocol and gateway handlers pass through unchanged.
func uniSwitchPage(next http.Handler, version string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/api/update" && r.Method == "GET" {
			writeJSON(w, map[string]any{"current": version, "version": version})
			return
		}
		if p == "/api/update" || strings.HasPrefix(p, "/api/update/") || strings.HasPrefix(p, "/api/uninstall") || p == "/api/cli" || p == "/api/settings/login" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "请通过 uni-switch 安装程序管理桌面应用；应用版本请查看本项目发布页"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func RunUniSwitchCore(version string) error {
	Version = version
	uniSwitchApplication.Store(true)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	key := NewWebKey()
	quit := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(quit) }) }
	host := &uniSwitchHost{quit: stop, encode: json.NewEncoder(os.Stdout), pending: make(map[uint64]chan string)}
	// A separate authenticated management port never serves inference routes.
	gw := startBackend()
	server := &http.Server{Handler: webHandler(port, key, 0, Handler(host, gw)), ReadHeaderTimeout: 30 * time.Second}
	startAppPage(host, gw)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			stop()
		}
	}()
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if scanner.Text() == "quit" {
				break
			}
			var message struct {
				ID     uint64 `json:"id"`
				Result string `json:"result"`
			}
			if json.Unmarshal(scanner.Bytes(), &message) == nil {
				host.mu.Lock()
				if ch := host.pending[message.ID]; ch != nil {
					select {
					case ch <- message.Result:
					default:
					}
				}
				host.mu.Unlock()
			}
		}
		stop()
	}()
	link := "http://" + listener.Addr().String() + "/?k=" + url.QueryEscape(key)
	if err := host.send(map[string]string{"type": "ready", "url": link, "version": version}); err != nil {
		server.Close()
		return err
	}
	<-quit
	server.Close()
	stopServing()
	proc.EndProbes()
	return nil
}
