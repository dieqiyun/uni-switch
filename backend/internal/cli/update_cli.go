package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// updateExecutable, updateCanElevate and updateInContainer are
// update.Executable, update.CanElevate and gateway.InContainer; tests stand
// in for them.
var (
	updateExecutable  = update.Executable
	updateCanElevate  = update.CanElevate
	updateInContainer = gateway.InContainer
	updatePackagedBy  = update.PackagedBy
)

// updateEveries are the intervals uni-switch update auto takes, by their
// minutes in settings.UpdateEveries.
var updateEveries = map[string]int{"30m": 30, "1h": 60, "6h": 360, "24h": 1440}

// updateAutoCmd is `uni-switch update auto [on|off] [download|notify]
// [30m|1h|6h|24h]`: whether the app asks for a newer version by itself, how
// often (#472), and whether one found is downloaded at once or only said to
// be out, downloaded on a click (notify, #2296). With none, it says how it
// is set.
func updateAutoCmd(args []string) error {
	s := settings.Load()
	for _, a := range args {
		switch a {
		case "on", "off":
			s.NoAutoUpdate = a == "off"
		case "download", "notify":
			s.NoAutoDownload = a == "notify"
		default:
			m, ok := updateEveries[a]
			if !ok {
				return fmt.Errorf("uni-switch update auto takes on, off, download, notify or how often (30m, 1h, 6h or 24h), not %q", a)
			}
			s.UpdateEvery = m
		}
	}
	if len(args) > 0 {
		if err := settings.Save(s); err != nil {
			return err
		}
	}
	every := "6h"
	for k, m := range updateEveries {
		if m == s.UpdateEvery {
			every = k
		}
	}
	if s.NoAutoUpdate {
		fmt.Println("automatic updates are off", muted.Render("· uni-switch update or Settings' Check looks for one (every "+every+" when on)"))
		if s.NoAutoDownload {
			fmt.Println(muted.Render("  a newer version found is only said to be out; Download update or uni-switch update downloads it"))
		}
		return nil
	}
	if s.NoAutoDownload {
		fmt.Println("automatic updates are on", muted.Render("· uni-switch looks for a newer version every "+every+" and says it is out; Download update or uni-switch update downloads it (uni-switch update auto download: at once)"))
		return nil
	}
	fmt.Println("automatic updates are on", muted.Render("· uni-switch looks for a newer version every "+every+" and downloads it (uni-switch update auto notify: only says it is out)"))
	return nil
}

// updateMirrorCmd is `uni-switch update mirror [<prefix>|off]`: the GitHub
// download mirror every update goes through, the app's own among them
// (settings.UpdateMirror). With nothing, it says which.
func updateMirrorCmd(args []string) error {
	s := settings.Load()
	if len(args) > 1 {
		return fmt.Errorf("uni-switch update mirror takes one prefix, or off")
	}
	if len(args) == 1 {
		switch m := strings.TrimSpace(args[0]); m {
		case "off", "none", "":
			s.UpdateMirror = ""
		default:
			if err := update.CheckMirror(m); err != nil {
				return err
			}
			s.UpdateMirror = m
		}
		if err := settings.Save(s); err != nil {
			return err
		}
	}
	if s.UpdateMirror == "" {
		fmt.Println("updates are downloaded from GitHub", muted.Render("· uni-switch update mirror <prefix> has them come through a mirror; the checksum is still io.uni-switch.desktop's"))
		return nil
	}
	fmt.Println("updates are downloaded through", bold.Render(s.UpdateMirror), muted.Render("· each is checked against uni-switch release checksums · uni-switch update mirror off goes back to GitHub"))
	return nil
}

// updateChannelCmd is `uni-switch update channel [stable|every]`: which
// versions an update is offered (settings.UpdateChannel, ssharpcc on X):
// Stable, one a week and an urgent one at once, or every version as it
// comes out. With none, it says which.
func updateChannelCmd(args []string) error {
	s := settings.Load()
	if len(args) > 1 {
		return fmt.Errorf("uni-switch update channel takes stable or every")
	}
	if len(args) == 1 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "stable":
			s.UpdateChannel = settings.UpdateStable
		case "every", "all":
			s.UpdateChannel = ""
		default:
			return fmt.Errorf("uni-switch update channel takes stable or every, not %q", args[0])
		}
		if err := settings.Save(s); err != nil {
			return err
		}
	}
	if s.UpdateChannel == settings.UpdateStable {
		fmt.Println("update channel: stable", muted.Render("· a version a week, the newest out three days as the week began, and an urgent one at once · uni-switch update channel every: each one as it comes out"))
		return nil
	}
	fmt.Println("update channel: every version", muted.Render("· each one as it comes out · uni-switch update channel stable: one a week, an urgent one at once"))
	return nil
}

// updateFlags takes --proxy and --mirror (each as --flag value or
// --flag=value) out of args.
func updateFlags(args []string) (rest []string, proxy, mirror string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, eq := strings.Cut(a, "=")
		if name != "--proxy" && name != "--mirror" {
			rest = append(rest, a)
			continue
		}
		if !eq {
			if i+1 >= len(args) {
				return nil, "", "", fmt.Errorf("%s needs a value", name)
			}
			i++
			val = args[i]
		}
		if name == "--proxy" {
			if err := update.CheckProxy(val); err != nil {
				return nil, "", "", err
			}
			proxy = val
			continue
		}
		if val != "off" {
			if err := update.CheckMirror(val); err != nil {
				return nil, "", "", err
			}
		}
		mirror = val
	}
	return rest, proxy, mirror, nil
}

// updateCmd is `uni-switch update [check] [--proxy <url>] [--mirror <prefix>]`:
// the app replaces its bundle, the terminal build its binary.
func updateCmd(args []string) error {
	args, proxy, mirror, err := updateFlags(args)
	if err != nil {
		return err
	}
	if len(args) > 1 && args[1] == "auto" {
		return updateAutoCmd(args[2:])
	}
	if len(args) > 1 && args[1] == "mirror" {
		return updateMirrorCmd(args[2:])
	}
	if len(args) > 1 && args[1] == "channel" {
		return updateChannelCmd(args[2:])
	}
	if len(args) > 2 || len(args) == 2 && args[1] != "check" {
		return fmt.Errorf("usage: uni-switch update [check] [--proxy <url>] [--mirror <prefix>]")
	}
	// the check is bounded; the download after it is not: a slow one
	// still moving goes on, and one that stops sending is ended by
	// update's stall timer (#5572)
	ctx := update.WithMirror(update.WithProxy(context.Background(), proxy), mirror)
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	rel, err := update.Latest(cctx)
	if err != nil {
		return err
	}
	if !update.Newer(rel.Version, version) {
		if update.Released(version) {
			if update.Stable() {
				fmt.Println(green.Render("✓"), "uni-switch", version, muted.Render("is up to date on the stable channel (stable is "+rel.Version+")"))
			} else {
				fmt.Println(green.Render("✓"), "uni-switch", version, muted.Render("is the latest"))
			}
		} else {
			fmt.Println("uni-switch", version, muted.Render("was built from source; the latest release is "+rel.Version))
		}
		return nil
	}
	fmt.Println("uni-switch", bold.Render(rel.Version), "is out", muted.Render("(you have "+version+") · "+rel.URL))
	if len(args) > 1 && args[1] == "check" {
		return nil
	}
	if !update.Released(version) {
		return fmt.Errorf("this uni-switch was built from source; update it the way you built it, or get the release from %s", update.Site)
	}
	if app := update.Bundle(); app != "" {
		if update.Stuck(app) != "" {
			return fmt.Errorf("%s can't be replaced where it is; move uni-switch to Applications, or download the new version from %s", tilde(app), update.Site)
		}
		fmt.Println(muted.Render("  downloading " + update.AppAsset() + via(ctx) + " …"))
		staged, err := update.Stage(ctx, rel, app)
		if err != nil {
			return err
		}
		err = update.Install(staged, app)
		if update.NeedsAdmin(err) {
			err = update.InstallAsAdmin(staged, app)
		}
		if err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "updated", tilde(app), "to", rel.Version)
		fmt.Println(muted.Render("  " + restartRunning(gateway.URL(), rel.Version)))
		if stale := update.StaleCLI(rel.Version); stale != "" {
			// The copy won't follow the app; the fix is the link install.sh
			// makes. `uni-switch update` run from the copy can't do it (it
			// replaces the copy with another copy), so name the real step.
			bin := filepath.Join(app, "Contents", "MacOS", "uni-switch")
			fmt.Println(muted.Render("  the `uni-switch` command at " + tilde(stale) + " is a copy behind this app; re-run the installer, or link it: ln -sf " + tilde(bin) + " " + tilde(stale)))
		}
		return nil
	}
	// the Docker image's /uni-switch, run as nonroot: the image is what is
	// updated, and a replace would only fail with "permission denied"
	if exe, err := updateExecutable(); err == nil && !update.Writable(filepath.Dir(exe)) && !updateCanElevate() && updateInContainer() {
		return fmt.Errorf("this uni-switch runs in a container and can't replace itself; update the image used by your deployment and recreate the container")
	}
	// a package manager's binary (Homebrew's, pacman's uni-switch-bin from
	// the AUR, a .deb's): replaced under it, the manager's database names
	// the old version and its next upgrade writes over this one
	if exe, err := updateExecutable(); err == nil {
		if p := updatePackagedBy(exe); p != nil {
			return fmt.Errorf("%s", p.Say())
		}
	}
	fmt.Println(muted.Render("  downloading " + update.BinaryAsset() + via(ctx) + " …"))
	if err := update.ReplaceBinary(ctx, rel); err != nil {
		return err
	}
	fmt.Println(green.Render("✓"), "updated to", rel.Version)
	fmt.Println(muted.Render("  " + restartRunning(gateway.URL(), rel.Version)))
	return nil
}

// restartRunning asks the uni-switch serving the gateway at base to restart
// into version, which this command put in (gateway.RestartPath), and
// says what came of it. Before, the app ran on as the old version until
// it was quit and reopened by hand (gaubeebangeel on X: an agent uni-switch
// serves ran the update, and restarting uni-switch cut it off). The app
// restarts as its own update does, handing the gateway over on a Mac or
// Linux, so this command's own agent isn't cut short.
var restartRunning = func(base, version string) string {
	c := &http.Client{Timeout: 10 * time.Second}
	res, err := c.Post(base+gateway.RestartPath, "application/json", strings.NewReader(`{"version":`+strconv.Quote(version)+`}`))
	if err != nil {
		return "uni-switch isn't running; it starts as " + version + " next time"
	}
	defer res.Body.Close()
	var r gateway.RestartReply
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusMethodNotAllowed {
		return "the uni-switch running is too old to restart by itself: quit and reopen uni-switch to use it"
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&r); err != nil || res.StatusCode != http.StatusOK {
		return fmt.Sprintf("the uni-switch running didn't restart (%s): quit and reopen uni-switch to use it", res.Status)
	}
	carried := ""
	if r.Handover {
		carried = ", handing its gateway over: nothing in flight is cut"
	}
	switch r.State {
	case "restarting":
		return "uni-switch " + r.Version + " restarts into " + version + " now" + carried
	case "waiting":
		n := 0
		if r.Busy != nil {
			n = r.Busy.Requests + r.Busy.Tools
		}
		return fmt.Sprintf("uni-switch %s restarts into %s once the %d request(s) it would cut short are done%s", r.Version, version, n, carried)
	case "unsupported":
		return "the uni-switch serving the gateway (" + r.Version + ", uni-switch serve) doesn't restart by itself: restart it the way you started it to use " + version
	}
	return "the uni-switch serving the gateway (" + r.Version + ") runs from another copy, which wasn't updated: update that one, or restart it"
}

// via says the mirror a download goes through, if any.
func via(ctx context.Context) string {
	if m := update.Mirror(ctx); m != "" {
		return " through " + m
	}
	return ""
}
