package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/daemon"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/gui"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

const daemonUsage = "usage: uni-switch daemon [install|uninstall|start|stop|restart|status] · uni-switch daemon install [--web] [--addr host:port] [--no-web] [--tls-cert <file> --tls-key <file>|--no-tls] · uni-switch daemon web [on|off] [--addr host:port]"

// daemonWait is how long install, start and restart wait for the gateway
// to answer before saying where its log is; a var for the tests.
var daemonWait = 5 * time.Second

// daemonWeb is what a daemon command says of the page the service serves
// beside the gateway (`uni-switch serve --web`, HuanCheng on Discord).
type daemonWeb struct {
	set  bool   // the command says whether it is served; not set, it stays as it is
	on   bool   // served
	addr string // where, "" for the default
}

// parseDaemon reads uni-switch daemon's arguments: the command, and what it
// says of the page.
func parseDaemon(args []string) (cmd string, w daemonWeb, err error) {
	cmd = "status"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if cmd == "web" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "on":
			w.set, w.on = true, true
		case "off":
			w.set = true
		case "status":
		default:
			return "", w, errors.New(daemonUsage)
		}
		args = args[1:]
	}
	if len(args) > 0 && cmd != "install" && cmd != "web" {
		return "", w, errors.New(daemonUsage)
	}
	off := false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--web" && cmd == "install":
			w.set, w.on = true, true
		case a == "--no-web" && cmd == "install":
			off = true
		case a == "--addr" && i+1 < len(args):
			i++
			w.addr = args[i]
		case strings.HasPrefix(a, "--addr="):
			w.addr = strings.TrimPrefix(a, "--addr=")
		default:
			return "", w, fmt.Errorf("uni-switch daemon %s: unknown %q · %s", cmd, a, daemonUsage)
		}
	}
	if w.addr != "" {
		if _, err := settings.LocalAddr(w.addr); err != nil {
			return "", w, fmt.Errorf("uni-switch daemon %s: %w", cmd, err)
		}
	}
	// --addr say where the page is: it is served
	if w.addr != "" {
		if cmd == "web" && w.set && !w.on || off {
			return "", w, fmt.Errorf("uni-switch daemon %s: --addr are for a page that is served", cmd)
		}
		w.set, w.on = true, true
	}
	if off {
		if w.on {
			return "", w, errors.New("uni-switch daemon install: --web and --no-web both")
		}
		w.set = true
	}
	if w.on && w.addr == "" {
		w.addr = webAddr
	}
	return cmd, w, nil
}

// webOptions is what the service is to serve: what w asks, or what it
// serves now (prevAddr, prevKey) when w says nothing. The page keeps its
// key across installs, so a browser stays signed in: UNI_SWITCH_WEB_KEY
// (envKey) when set, else the key the service has, else a new one.
func webOptions(w daemonWeb, prevAddr, prevKey, envKey string) (daemon.Options, error) {
	if !w.set {
		if prevAddr != "" {
			if _, err := settings.LocalAddr(prevAddr); err != nil {
				return daemon.Options{}, err
			}
		}
		return daemon.Options{Web: prevAddr, Key: prevKey}, nil
	}
	if !w.on {
		return daemon.Options{}, nil
	}
	if _, err := settings.LocalAddr(w.addr); err != nil {
		return daemon.Options{}, err
	}
	key := envKey
	if key == "" {
		key = prevKey
	}
	if key == "" {
		key = gui.NewWebKey()
	}
	if err := gui.CheckWebKey(key); err != nil {
		return daemon.Options{}, err
	}
	return daemon.Options{Web: w.addr, Key: key}, nil
}

// uni-switch daemon …: `uni-switch serve` as a service of the user's, started at
// boot or login and again when it stops (systemd --user, launchd).
func daemonCmd(args []string) error {
	// install takes the gateway's TLS files, written into the service as
	// UNI_SWITCH_TLS_CERT and UNI_SWITCH_TLS_KEY (daemon.carried); a pair that
	// doesn't load is refused before anything is written. --no-tls takes
	// the service's away.
	noTLS := false
	if len(args) > 1 && args[0] == "install" {
		rest := args[1:]
		for i, a := range rest {
			if a == "--no-tls" {
				noTLS = true
				rest = append(append([]string{}, rest[:i]...), rest[i+1:]...)
				break
			}
		}
		rest, err := tlsFlags(rest)
		if err != nil {
			return err
		}
		if noTLS && gateway.TLSOn() {
			return errors.New("uni-switch daemon install: --no-tls and a certificate both")
		}
		args = append([]string{"install"}, rest...)
	}
	cmd, web, err := parseDaemon(args)
	if err != nil {
		return err
	}
	if cmd == "install" || cmd == "web" {
		keepTLS(noTLS)
	}
	switch cmd {
	case "status":
		return daemonStatus()
	case "install":
		err = daemonInstall(web)
	case "web":
		if !web.set {
			return daemonWebStatus()
		}
		if st, _ := daemon.Get(); !st.Installed && !web.on {
			fmt.Println("the uni-switch service isn't installed ·", muted.Render("uni-switch daemon install"))
			return nil
		}
		err = daemonInstall(web)
	case "uninstall", "remove":
		if err := daemon.Uninstall(); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "the uni-switch service is removed")
		return nil
	case "start":
		err = daemon.Start()
	case "stop":
		if err := daemon.Stop(); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "the uni-switch service is stopped")
		return nil
	case "restart":
		err = daemon.Restart()
	default:
		return errors.New(daemonUsage)
	}
	if err != nil {
		return err
	}
	st, _ := daemon.Get()
	switch cmd {
	case "install", "web":
		fmt.Println(green.Render("✓"), "uni-switch serves the gateway as a service, from boot or login, started again when it stops")
		fmt.Println(muted.Render("  file   "), st.File)
		if c, _ := gateway.TLSFiles(); c != "" && gateway.TLSOn() {
			fmt.Println(muted.Render("  TLS    "), c, muted.Render("· https:// from other computers"))
		}
		if a, k := daemon.Web(); a != "" {
			printDaemonWeb(a, k)
		} else if cmd == "web" {
			fmt.Println(muted.Render("  page    not served"))
		}
		if a := daemon.Linger(); a != "" {
			fmt.Println(amber.Render("!"), a)
		}
		if daemon.Node() == "" {
			fmt.Println(amber.Render("!"), "no node on the service's PATH: plugins, and the MCP servers they start, need Node.js; install it, then run `uni-switch daemon install` again")
		}
	default:
		fmt.Println(green.Render("✓"), "the uni-switch service is "+cmd+"ed")
	}
	deadline := time.Now().Add(daemonWait)
	for {
		if o := gateway.ServedBy(); o.Running {
			fmt.Println(green.Render("●"), "the gateway answers on", bold.Render(gateway.URL()))
			return nil
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	fmt.Println(amber.Render("!"), "the gateway doesn't answer on", gateway.URL(), "yet ·", st.Logs)
	return nil
}

// keepTLS: the service installed again (a new PATH, the page turned on)
// keeps the certificate it was installed with when this shell names none,
// as the page keeps its key; noTLS (--no-tls) takes it away.
func keepTLS(noTLS bool) {
	if noTLS {
		os.Unsetenv(gateway.TLSCertEnv)
		os.Unsetenv(gateway.TLSKeyEnv)
		return
	}
	if c, k := gateway.TLSFiles(); c != "" || k != "" {
		return
	}
	if c, k := daemon.TLS(); c != "" {
		os.Setenv(gateway.TLSCertEnv, c)
		os.Setenv(gateway.TLSKeyEnv, k)
	}
}

// daemonInstall installs the service, serving the page as w says.
func daemonInstall(w daemonWeb) error {
	a, k := daemon.Web()
	o, err := webOptions(w, a, k, os.Getenv("UNI_SWITCH_WEB_KEY"))
	if err != nil {
		return err
	}
	return daemon.Install(o)
}

// printDaemonWeb prints the service's page: its link, and on the network
// its links there and what that lets anyone with one do.
func printDaemonWeb(addr, key string) {
	fmt.Println(muted.Render("  page   "), webLink(addr, key))

}

// daemonWebStatus: uni-switch daemon web — the page the service serves.
func daemonWebStatus() error {
	st, err := daemon.Get()
	if err != nil {
		return err
	}
	if !st.Installed {
		fmt.Println("the uni-switch service isn't installed ·", muted.Render("uni-switch daemon install --web"))
		return nil
	}
	a, k := daemon.Web()
	if a == "" {
		fmt.Println("the uni-switch service serves no page ·", muted.Render("uni-switch daemon web on"))
		return nil
	}
	printDaemonWeb(a, k)
	return nil
}

func daemonStatus() error {
	st, err := daemon.Get()
	if err != nil {
		return err
	}
	served := gateway.ServedBy()
	if !st.Installed {
		fmt.Println("the uni-switch service isn't installed ·", muted.Render("uni-switch daemon install"))
		if served.Running {
			fmt.Println(muted.Render("  another uni-switch (the app, uni-switch serve) answers on"), gateway.URL())
		}
		return nil
	}
	mark, word := amber.Render("○"), "not running"
	if st.Running {
		mark, word = green.Render("●"), "running"
	}
	line := "the uni-switch service is " + word
	if st.PID > 0 {
		line += " · pid " + strconv.Itoa(st.PID)
	}
	fmt.Println(mark, line)
	if st.State != "" {
		fmt.Println(muted.Render("  state  "), st.State)
	}
	if st.LastExit != "" {
		fmt.Println(muted.Render("  last exit"), st.LastExit)
	}
	start := "at boot or login"
	if !st.Enabled {
		start = "only when started"
	}
	fmt.Println(muted.Render("  start  "), start)
	fmt.Println(muted.Render("  file   "), st.File)
	fmt.Println(muted.Render("  logs   "), st.Logs)
	if a, k := daemon.Web(); a != "" {
		printDaemonWeb(a, k)
	}
	if served.Running {
		fmt.Println(muted.Render("  gateway"), "answers on", gateway.URL(), muted.Render(served.Version))
	} else {
		fmt.Println(muted.Render("  gateway"), "doesn't answer on", gateway.URL())
	}
	for _, a := range st.Advice {
		fmt.Println(amber.Render("!"), a)
	}
	return nil
}
