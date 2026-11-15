package cli

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/daemon"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/gui"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/stats"
)

// webAddr is where `uni-switch web` listens unless told: beside the gateway.
const webAddr = "127.0.0.1:3430"

// webCmd: uni-switch web [--addr host:port] [--no-open] [--gateway]
// [--tls-cert <file> --tls-key <file>] —
// the app's window in a browser, for a computer that can't show the app;
// --gateway shows it in gateway mode (gui.WebGateway) unless Settings says
// otherwise. Both listeners accept loopback addresses only.
func webCmd(args []string) error {
	args, err := tlsFlags(args)
	if err != nil {
		return err
	}
	addr, open := webAddr, true
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--addr" && i+1 < len(args):
			i++
			addr = args[i]
		case strings.HasPrefix(a, "--addr="):
			addr = strings.TrimPrefix(a, "--addr=")
		case a == "--no-open":
			open = false
		case a == "--gateway":
			gui.WebGateway.Store(true)
		default:
			return fmt.Errorf("uni-switch web: unknown %q · uni-switch web [--addr host:port] [--no-open] [--gateway] [--tls-cert <file> --tls-key <file>], UNI_SWITCH_WEB_KEY to keep one key", a)
		}
	}
	addr, err = settings.LocalAddr(addr)
	if err != nil {
		return err
	}
	// the service serves the page already: its own, beside the gateway
	// it serves, is the one to open
	if a, k := daemon.Web(); a != "" && gateway.ServedBy().Running {
		link := webLink(a, k)
		fmt.Println(green.Render("●"), "the uni-switch service serves this page:", bold.Render(link))
		fmt.Println(muted.Render("  uni-switch daemon web off turns it off"))
		if open {
			openInBrowser(link)
		}
		return nil
	}
	// the app serves this page on this port already (Settings › Network
	// and sharing › Browser access): its own is the one to open
	if a, k := gui.AppPage(); a != "" && samePort(a, addr) && answers(a) {
		link := webLink(a, k)
		fmt.Println(green.Render("●"), "the uni-switch app serves this page:", bold.Render(link))
		fmt.Println(muted.Render("  Settings › Network › Browser access turns it off"))
		if open {
			openInBrowser(link)
		}
		return nil
	}
	// what this page's gateway starts (agents, MCP servers) gets the node a
	// version manager put on PATH only in the shell's profile, as the app's
	// does: a service or autostart has none of it (#3803)
	proc.UserPath()
	w, err := gui.StartWeb(addr, version)
	if err != nil {
		return err
	}
	if !w.ServesGateway() {
		// another uni-switch has the gateway (the service, uni-switch serve, the
		// app): this page shows its settings and accounts, read from the
		// same files, and takes the gateway up when that one stops
		o := gateway.ServedBy()
		who := "another uni-switch"
		if o.Version != "" {
			who += " (" + o.Version + ")"
		}
		fmt.Println(muted.Render("  the gateway on " + gateway.URL() + " is served by " + who + "; this page shows the same settings and accounts, and serves the gateway once that one stops"))
		if st, err := daemon.Get(); err == nil && st.Running {
			fmt.Println(muted.Render("  to have the service serve this page itself: uni-switch daemon web on"))
		}
	}
	printWeb(w)
	if open {
		openInBrowser(w.Link)
	}
	return w.Wait()
}

// samePort: a and b (host:port) name one port.
func samePort(a, b string) bool {
	_, pa, ea := net.SplitHostPort(a)
	_, pb, eb := net.SplitHostPort(b)
	return ea == nil && eb == nil && pa == pb
}

// answers: something listens on addr's port on this computer.
func answers(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// printWeb prints where a started page is, and what its link carries.
func printWeb(w *gui.Web) {
	fmt.Println(green.Render("●"), "uni-switch web on", bold.Render(w.Link))
	if l := tlsLine(); l != "" {
		fmt.Println(l)
	}
	carries := "this run's key (UNI_SWITCH_WEB_KEY keeps one across runs)"
	if os.Getenv("UNI_SWITCH_WEB_KEY") != "" {
		carries = "UNI_SWITCH_WEB_KEY"
	}
	fmt.Println(muted.Render("  the link carries " + carries + " · gateway " + gateway.URL() + " · Ctrl-C to stop"))
}

// webLink is the link to a page served on addr with key, from this
// computer.
func webLink(addr, key string) string {
	local, err := settings.LocalAddr(addr)
	if err != nil {
		return ""
	}
	return "http://" + local + "/?k=" + url.QueryEscape(key)
}

// serveCmd: uni-switch serve [--web[=host:port]] — the gateway, and with
// --web uni-switch web's page from the same process: what the service runs
// (`uni-switch daemon install --web`), so the page shows the gateway that
// serves the agents, its routing included (HuanCheng on Discord).
func serveCmd(args []string) error {
	args, err := tlsFlags(args)
	if err != nil {
		return err
	}
	web := ""
	for _, a := range args {
		switch {
		case a == "--web":
			web = webAddr
		case strings.HasPrefix(a, "--web="):
			web = strings.TrimPrefix(a, "--web=")
			if _, _, err := net.SplitHostPort(web); err != nil {
				return fmt.Errorf("uni-switch serve: --web=%s isn't host:port", web)
			}
		default:
			return fmt.Errorf("uni-switch serve: unknown %q · uni-switch serve [--web[=host:port]] [--tls-cert <file> --tls-key <file>]", a)
		}
	}
	if web != "" {
		web, err = settings.LocalAddr(web)
		if err != nil {
			return err
		}
	}
	proc.UserPath() // as uni-switch web's (#3803)
	if web == "" {
		return serve()
	}
	go stats.Run(version, "serve")
	w, err := gui.StartWeb(web, version)
	if err != nil {
		return err
	}
	public := gateway.URL()
	fmt.Println(green.Render("●"), "uni-switch gateway on", bold.Render(gateway.URL()))
	fmt.Println(muted.Render("  OpenAI  "), public+"/v1/chat/completions", muted.Render("·"), public+"/v1/responses")
	fmt.Println(muted.Render("  Anthropic"), public+"/v1/messages")
	if l := tlsLine(); l != "" {
		fmt.Println(l)
	}
	if !w.ServesGateway() {
		fmt.Println(amber.Render("!"), "another uni-switch serves the gateway there: this one serves it once that one stops")
	}
	printWeb(w)
	return w.Wait()
}
