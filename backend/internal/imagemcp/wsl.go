package imagemcp

// An agent in WSL starts this Windows uni-switch through WSL's interop (#900),
// with --wsl <distro> (and --mount, --home where the distro's differ): the
// paths it gives — its roots, a reference image, where to save — are the
// distro's, opened here through \\wsl.localhost\<distro> (or C:\ for
// /mnt/c), and the paths answered are the distro's again, which the agent
// can open. The gateway is this machine's, at 127.0.0.1, which a Windows
// program reaches from WSL as from anywhere on Windows.

import (
	"errors"
	"os"
	"runtime"
	"strings"
)

// wslSide is the distro the agent runs in: its name, where it mounts
// Windows' drives (/mnt/), its $HOME (/home/me) and the share its files
// are opened through (\\wsl.localhost\ or \\wsl$\).
type wslSide struct {
	distro, mount, home, share string
}

// wsl is the agent's distro, nil when it isn't in one.
var wsl *wslSide

// options reads what follows "image": --wsl <distro>, --mount <dir>,
// --home <dir>.
func options(args []string) (*wslSide, error) {
	if len(args) == 0 {
		return nil, nil
	}
	var w wslSide
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			return nil, errors.New("mcp image: " + args[i] + " needs a value")
		}
		switch v := args[i+1]; args[i] {
		case "--wsl":
			w.distro = v
		case "--mount":
			w.mount = v
		case "--home":
			w.home = v
		default:
			return nil, errors.New("mcp image: unknown option " + args[i])
		}
		i++
	}
	if w.distro == "" {
		return nil, errors.New("mcp image: --mount and --home go with --wsl")
	}
	if w.mount == "" {
		w.mount = "/mnt/"
	}
	if !strings.HasSuffix(w.mount, "/") {
		w.mount += "/"
	}
	w.share = `\\wsl$\`
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(`\\wsl.localhost\` + w.distro + `\`); err == nil {
			w.share = `\\wsl.localhost\`
		}
	}
	return &w, nil
}

// local is the distro's path p as this machine opens it: /mnt/c/x is
// C:\x, /home/me/x \\wsl.localhost\<distro>\home\me\x, ~/x is under the
// distro's home. What isn't the distro's path (C:\x, a relative path) is p.
func (w *wslSide) local(p string) string {
	if strings.HasPrefix(p, "~/") && w.home != "" {
		p = strings.TrimSuffix(w.home, "/") + p[1:]
	}
	if !strings.HasPrefix(p, "/") {
		return p
	}
	if rest, ok := strings.CutPrefix(p, w.mount); ok && len(rest) >= 1 && isLetter(rest[0]) && (len(rest) == 1 || rest[1] == '/') {
		return strings.ToUpper(rest[:1]) + `:\` + strings.ReplaceAll(strings.TrimPrefix(rest[1:], "/"), "/", `\`)
	}
	return w.share + w.distro + strings.ReplaceAll(p, "/", `\`)
}

// shown is the path p this machine has as the distro has it: the reverse
// of local.
func (w *wslSide) shown(p string) string {
	for _, pre := range []string{`\\wsl.localhost\`, `\\wsl$\`} {
		if len(p) > len(pre) && strings.EqualFold(p[:len(pre)], pre) {
			rest := p[len(pre):]
			i := strings.IndexByte(rest, '\\')
			if i < 0 {
				return "/"
			}
			if !strings.EqualFold(rest[:i], w.distro) {
				return p // another distro's: no path of this one
			}
			return strings.ReplaceAll(rest[i:], `\`, "/")
		}
	}
	if len(p) >= 2 && isLetter(p[0]) && p[1] == ':' {
		rest := strings.TrimLeft(strings.ReplaceAll(p[2:], `\`, "/"), "/")
		out := w.mount + strings.ToLower(p[:1])
		if rest != "" {
			out += "/" + rest
		}
		return out
	}
	return p
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// shownPath is p as the agent has it.
func shownPath(p string) string {
	if wsl == nil {
		return p
	}
	return wsl.shown(p)
}
