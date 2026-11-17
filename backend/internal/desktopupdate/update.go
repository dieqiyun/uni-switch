// Package desktopupdate checks and downloads this product's desktop releases.
// It never replaces the embedded core with the standalone CLI updater.
package desktopupdate

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

const repository = "https://github.com/dieqiyun/uni-switch"
const latestAPI = "https://api.github.com/repos/dieqiyun/uni-switch/releases/latest"
const maxAssetSize = 1 << 30

var releaseVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type release struct {
	Tag        string  `json:"tag_name"`
	Notes      string  `json:"body"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

type State struct {
	Current    string `json:"current"`
	State      string `json:"state"`
	Latest     string `json:"latest,omitempty"`
	URL        string `json:"url"`
	Notes      string `json:"notes,omitempty"`
	Error      string `json:"error,omitempty"`
	Task       string `json:"task,omitempty"`
	Done       int64  `json:"done"`
	Total      int64  `json:"total"`
	CheckedAt  string `json:"checkedAt,omitempty"`
	AutoCheck  bool   `json:"autoCheck"`
	InstallVia string `json:"installVia"`
}

type Updater struct {
	mu       sync.Mutex
	state    State
	client   *http.Client
	goos     string
	arch     string
	cache    string
	path     string
	expected asset
	ctx      context.Context
	close    context.CancelFunc
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	last     time.Time
	install  func(string) error
}

func New(current, cache string, install func(string) error) *Updater {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = netproxy.Func
	transport.ResponseHeaderTimeout = 30 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	u := &Updater{client: &http.Client{Transport: netproxy.Dispatch(transport)}, goos: runtime.GOOS, arch: runtime.GOARCH, cache: cache, ctx: ctx, close: cancel, install: install}
	u.client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !downloadHost(r.URL) {
			return errors.New("update redirect refused")
		}
		return nil
	}
	u.state = State{Current: current, State: "idle", URL: repository + "/releases", InstallVia: runtime.GOOS}
	if runtime.GOOS == "linux" && os.Getenv("APPIMAGE") != "" {
		u.state.InstallVia = "appimage"
	}
	return u
}

// Start checks at launch and every six hours. A failed check is retried after
// fifteen minutes. enabled is read again each minute so preference changes apply.
func (u *Updater) Start(enabled func() bool) {
	u.wg.Go(func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			auto := enabled()
			u.mu.Lock()
			u.state.AutoCheck = auto
			interval := 6 * time.Hour
			if u.state.Error != "" {
				interval = 15 * time.Minute
			}
			due := u.state.AutoCheck && time.Since(u.last) >= interval
			u.mu.Unlock()
			if due {
				u.Check()
			}
			select {
			case <-u.ctx.Done():
				return
			case <-tick.C:
			}
		}
	})
}

func (u *Updater) Close() {
	u.mu.Lock()
	u.close()
	u.mu.Unlock()
	u.wg.Wait()
	u.removeDownload()
}

func (u *Updater) Snapshot() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

func (u *Updater) begin(state string) (context.Context, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.ctx.Err() != nil || u.cancel != nil || u.state.State == "canceling" || u.state.State == "installing" || u.state.State == "handedoff" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(u.ctx, 30*time.Minute)
	u.cancel = cancel
	u.state.State, u.state.Error = state, ""
	u.wg.Add(1)
	return ctx, true
}

func (u *Updater) Check() {
	ctx, ok := u.begin("checking")
	if !ok {
		return
	}
	go func() {
		defer u.wg.Done()
		defer u.finishContext()
		r, err := u.latest(ctx)
		u.mu.Lock()
		defer u.mu.Unlock()
		u.last = time.Now()
		u.state.CheckedAt = u.last.UTC().Format(time.RFC3339)
		if err != nil {
			u.state.State, u.state.Error = "error", err.Error()
			return
		}
		u.accept(r)
		if u.path != "" && u.expected.Name == u.assetName(u.state.Latest) {
			u.state.State = "ready"
		}
	}()
}

func (u *Updater) finishContext() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cancel != nil {
		u.cancel()
		u.cancel = nil
	}
}

func (u *Updater) accept(r release) {
	u.state.Latest = strings.TrimPrefix(r.Tag, "v")
	u.state.URL = repository + "/releases/tag/" + r.Tag
	u.state.Notes = r.Notes
	u.state.State = "latest"
	if update.Newer(u.state.Latest, u.state.Current) {
		u.state.State = "available"
	}
}

func downloadHost(v *url.URL) bool {
	if v.Scheme != "https" || v.User != nil || (v.Port() != "" && v.Port() != "443") {
		return false
	}
	switch v.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}

func (u *Updater) get(ctx context.Context, address string) (*http.Response, error) {
	v, err := url.Parse(address)
	if err != nil || !downloadHost(v) {
		return nil, errors.New("update URL refused")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "uni-switch/"+u.state.Current)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("GitHub: %s", res.Status)
	}
	return res, nil
}

func (u *Updater) latest(ctx context.Context) (release, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var r release
	res, err := u.get(ctx, latestAPI)
	if err != nil {
		return r, err
	}
	defer res.Body.Close()
	err = json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&r)
	if err == nil && (r.Draft || r.Prerelease || !releaseVersion.MatchString(r.Tag)) {
		err = errors.New("GitHub returned an invalid stable release")
	}
	return r, err
}

func (u *Updater) assetName(version string) string {
	if u.goos == "darwin" && (u.arch == "arm64" || u.arch == "amd64") {
		return "uni-switch_" + version + "_universal.dmg"
	}
	if u.arch != "amd64" {
		return ""
	}
	switch u.goos {
	case "windows":
		return "uni-switch_" + version + "_x64-setup.exe"
	case "linux":
		if u.state.InstallVia == "appimage" {
			return "uni-switch_" + version + "_x86_64.AppImage"
		}
		return "uni-switch_" + version + "_amd64.deb"
	}
	return ""
}

func findAsset(r release, name string) (asset, error) {
	for _, a := range r.Assets {
		if a.Name == name && a.Size > 0 && a.Size <= maxAssetSize && a.URL == repository+"/releases/download/"+r.Tag+"/"+name {
			return a, nil
		}
	}
	return asset{}, errors.New("this release has no verified package for this platform; use GitHub download")
}

func (u *Updater) checksum(ctx context.Context, r release, a asset) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if strings.HasPrefix(a.Digest, "sha256:") {
		if b, err := hex.DecodeString(strings.TrimPrefix(a.Digest, "sha256:")); err == nil && len(b) == sha256.Size {
			return hex.EncodeToString(b), nil
		}
	}
	sums, err := findAsset(r, "SHA256SUMS.txt")
	if err != nil {
		return "", errors.New("release has no SHA-256 checksum")
	}
	res, err := u.get(ctx, sums.URL)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	if err != nil {
		return "", err
	}
	var digest string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == a.Name {
			b, err := hex.DecodeString(fields[0])
			if err != nil || len(b) != sha256.Size || digest != "" {
				return "", errors.New("invalid or duplicate release checksum")
			}
			digest = hex.EncodeToString(b)
		}
	}
	if digest == "" {
		return "", errors.New("package checksum is missing")
	}
	return digest, nil
}

// Download accepts a version, never a URL or path supplied by the page.
func (u *Updater) Download(version string) {
	ctx, ok := u.begin("downloading")
	if !ok {
		return
	}
	go func() {
		defer u.wg.Done()
		defer u.finishContext()
		u.removeDownload()
		u.mu.Lock()
		u.state.Done, u.state.Total = 0, 0
		u.mu.Unlock()
		err := u.download(ctx, version)
		u.mu.Lock()
		if err != nil {
			u.state.State, u.state.Error = "error", err.Error()
			if errors.Is(ctx.Err(), context.Canceled) {
				u.state.State, u.state.Error = "available", ""
			}
		} else if ctx.Err() != nil {
			u.state.State, u.state.Error = "error", ctx.Err().Error()
			if errors.Is(ctx.Err(), context.Canceled) {
				u.state.State, u.state.Error = "available", ""
			}
		} else {
			u.state.State = "ready"
		}
		remove := u.state.State != "ready"
		u.mu.Unlock()
		if remove {
			u.removeDownload()
		}
	}()
}

func (u *Updater) download(ctx context.Context, version string) error {
	r, err := u.latest(ctx)
	if err != nil {
		return err
	}
	if strings.TrimPrefix(r.Tag, "v") != version || !update.Newer(version, u.state.Current) {
		u.mu.Lock()
		u.accept(r)
		u.mu.Unlock()
		return errors.New("the latest release changed; review the version and download again")
	}
	u.mu.Lock()
	u.accept(r)
	u.state.State = "downloading"
	u.mu.Unlock()
	a, err := findAsset(r, u.assetName(version))
	if err != nil {
		return err
	}
	digest, err := u.checksum(ctx, r, a)
	if err != nil {
		return err
	}
	a.Digest = digest
	if err := os.MkdirAll(u.cache, 0o700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(u.cache, "download-")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, a.Name)
	u.mu.Lock()
	u.path, u.expected = path, a
	u.state.Done, u.state.Total, u.state.Task = 0, a.Size, ""
	u.mu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stall := time.AfterFunc(time.Minute, cancel)
	defer stall.Stop()
	res, err := u.get(ctx, a.URL)
	if err != nil {
		f.Close()
		return err
	}
	defer res.Body.Close()
	if res.ContentLength > 0 && res.ContentLength != a.Size {
		f.Close()
		return errors.New("package size differs from GitHub release metadata")
	}
	_, err = io.Copy(f, io.TeeReader(io.LimitReader(res.Body, a.Size+1), progressWriter{u, stall}))
	if syncErr := f.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = verify(path, a)
	}
	if err != nil {
		return err
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return err
	}
	u.mu.Lock()
	u.state.Task = hex.EncodeToString(token[:])
	u.mu.Unlock()
	return nil
}

type progressWriter struct {
	u     *Updater
	stall *time.Timer
}

func (p progressWriter) Write(data []byte) (int, error) {
	p.stall.Reset(time.Minute)
	p.u.mu.Lock()
	p.u.state.Done += int64(len(data))
	p.u.mu.Unlock()
	return len(data), nil
}

func verify(path string, a asset) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != a.Size {
		return errors.New("downloaded package has an unexpected size or file type")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != a.Digest {
		return errors.New("downloaded package failed SHA-256 verification")
	}
	var magic [8]byte
	if _, err := f.ReadAt(magic[:], 0); err != nil {
		return err
	}
	valid := strings.HasSuffix(a.Name, ".exe") && string(magic[:2]) == "MZ" || strings.HasSuffix(a.Name, ".deb") && string(magic[:]) == "!<arch>\n" || strings.HasSuffix(a.Name, ".AppImage") && string(magic[:4]) == "\x7fELF"
	if strings.HasSuffix(a.Name, ".dmg") && stat.Size() >= 512 {
		var tail [4]byte
		_, err := f.ReadAt(tail[:], stat.Size()-512)
		valid = err == nil && string(tail[:]) == "koly"
	}
	if !valid {
		return errors.New("downloaded package is not the expected installer format")
	}
	return nil
}

func (u *Updater) Cancel() {
	u.mu.Lock()
	if u.state.State == "downloading" {
		u.state.State = "canceling"
		u.cancel()
	} else if u.state.State == "ready" {
		u.state.State = "canceling"
		u.mu.Unlock()
		u.removeDownload()
		u.mu.Lock()
		u.state.State = "available"
	}
	u.mu.Unlock()
}

func (u *Updater) Install(task string) error {
	u.mu.Lock()
	if u.ctx.Err() != nil || u.state.State != "ready" || task == "" || task != u.state.Task {
		u.mu.Unlock()
		return errors.New("the verified download is no longer ready; download again")
	}
	u.state.State = "installing"
	u.wg.Add(1)
	defer u.wg.Done()
	path, a := u.path, u.expected
	u.mu.Unlock()
	err := verify(path, a)
	if err == nil {
		if u.install == nil {
			err = errors.New("installer handoff is unavailable")
		} else {
			err = u.install(path)
		}
	}
	u.mu.Lock()
	if err != nil {
		u.state.State, u.state.Error = "error", err.Error()
	} else {
		u.state.State, u.path = "handedoff", "" // Keep the package for the installer.
	}
	u.state.Task = ""
	u.mu.Unlock()
	if err != nil {
		u.removeDownload()
	}
	return err
}

func (u *Updater) removeDownload() {
	u.mu.Lock()
	path := u.path
	u.path, u.state.Task = "", ""
	u.mu.Unlock()
	if path != "" {
		// Only a private directory made by this updater, never a page's path.
		os.RemoveAll(filepath.Dir(path))
	}
}
