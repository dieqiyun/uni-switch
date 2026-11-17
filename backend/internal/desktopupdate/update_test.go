package desktopupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(r *http.Request, data []byte) *http.Response {
	return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Header: make(http.Header), Request: r}
}

type fixture struct {
	mu        sync.Mutex
	release   release
	data      []byte
	fail      bool
	checks    int
	downloads int
	sums      string
}

func newFixture(t *testing.T) (*Updater, *fixture) {
	t.Helper()
	u := New("0.5.30", t.TempDir(), nil)
	u.goos, u.arch = "windows", "amd64"
	f := &fixture{data: append([]byte("MZ"), bytes.Repeat([]byte{42}, 1024)...)}
	sum := sha256.Sum256(f.data)
	name := "uni-switch_0.5.32_x64-setup.exe"
	f.release = release{Tag: "v0.5.32", Notes: "修复更新提示\n\nRelease notes", Assets: []asset{{Name: name, URL: repository + "/releases/download/v0.5.32/" + name, Size: int64(len(f.data)), Digest: "sha256:" + hex.EncodeToString(sum[:])}}}
	u.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "" || r.URL.Host != "api.github.com" && r.URL.Host != "github.com" {
			t.Error("update request leaked authentication or used another host")
		}
		if r.URL.String() == latestAPI {
			f.checks++
			if f.fail {
				return nil, errors.New("fixture GitHub offline")
			}
			b, _ := json.Marshal(f.release)
			return response(r, b), nil
		}
		if strings.HasSuffix(r.URL.Path, "/SHA256SUMS.txt") {
			return response(r, []byte(f.sums)), nil
		}
		f.downloads++
		return response(r, f.data), nil
	})
	t.Cleanup(u.Close)
	return u, f
}

func waitFinished(t *testing.T, u *Updater) State {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		u.mu.Lock()
		finished := u.cancel == nil
		s := u.state
		u.mu.Unlock()
		if finished {
			return s
		}
		select {
		case <-deadline:
			t.Fatalf("updater did not finish: %+v", s)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestStartupChecksGitHubWithoutDownloading(t *testing.T) {
	u, f := newFixture(t)
	u.Start(func() bool { return true })
	deadline := time.After(5 * time.Second)
	for u.Snapshot().Latest == "" {
		select {
		case <-deadline:
			t.Fatal("startup did not discover the GitHub release")
		case <-time.After(time.Millisecond):
		}
	}
	s := waitFinished(t, u)
	if s.State != "available" || s.Latest != "0.5.32" || !s.AutoCheck || s.CheckedAt == "" {
		t.Fatalf("no visible update: %+v", s)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.checks != 1 || f.downloads != 0 {
		t.Fatalf("startup requests: checks=%d downloads=%d", f.checks, f.downloads)
	}
}

func TestFailedCheckKeepsKnownReleaseAndCanRetry(t *testing.T) {
	u, f := newFixture(t)
	u.Check()
	waitFinished(t, u)
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	u.Check()
	s := waitFinished(t, u)
	if s.State != "error" || s.Latest != "0.5.32" || !strings.Contains(s.Error, "offline") {
		t.Fatalf("failed check lost release or error: %+v", s)
	}
	f.mu.Lock()
	f.fail = false
	f.mu.Unlock()
	u.Check()
	if s = waitFinished(t, u); s.State != "available" || s.Error != "" {
		t.Fatalf("check cannot recover: %+v", s)
	}
}

func TestVerifiedDownloadRequiresExplicitInstallAndRehash(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		t.Run(map[bool]string{false: "install", true: "tampered"}[tamper], func(t *testing.T) {
			u, _ := newFixture(t)
			var installs atomic.Int32
			u.install = func(path string) error {
				installs.Add(1)
				if _, err := os.Stat(path); err != nil {
					t.Error(err)
				}
				return nil
			}
			u.Download("0.5.32")
			s := waitFinished(t, u)
			if s.State != "ready" || s.Task == "" || s.Done != s.Total || installs.Load() != 0 {
				t.Fatalf("download did not stop at verified confirmation: %+v", s)
			}
			if err := u.Install("wrong-token"); err == nil {
				t.Fatal("accepted a different download task")
			}
			if tamper {
				data, _ := os.ReadFile(u.path)
				data[len(data)-1] ^= 1
				if err := os.WriteFile(u.path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := u.Install(s.Task)
			if tamper {
				if err == nil || installs.Load() != 0 || u.Snapshot().State != "error" {
					t.Fatal("modified installer was handed to the OS")
				}
			} else if err != nil || installs.Load() != 1 || u.Snapshot().State != "handedoff" {
				t.Fatalf("verified installer was not handed off: %v", err)
			}
			if err := u.Install(s.Task); err == nil {
				t.Fatal("download task was reused")
			}
		})
	}
}

func TestDownloadRefusesInvalidReleaseOrPackage(t *testing.T) {
	for _, kind := range []string{"draft", "prerelease", "changed", "foreign-url", "zero-size", "bad-size", "bad-hash", "bad-magic", "missing-checksum", "duplicate-checksum"} {
		t.Run(kind, func(t *testing.T) {
			u, f := newFixture(t)
			switch kind {
			case "draft":
				f.release.Draft = true
			case "prerelease":
				f.release.Prerelease = true
			case "changed":
				f.release.Tag = "v0.5.33"
			case "foreign-url":
				f.release.Assets[0].URL = "https://example.com/installer.exe"
			case "zero-size":
				f.release.Assets[0].Size = 0
			case "bad-size":
				f.release.Assets[0].Size++
			case "bad-hash":
				f.data[len(f.data)-1] ^= 1
			case "bad-magic":
				f.data[0] = 'X'
				sum := sha256.Sum256(f.data)
				f.release.Assets[0].Digest = "sha256:" + hex.EncodeToString(sum[:])
			case "missing-checksum":
				f.release.Assets[0].Digest = ""
			case "duplicate-checksum":
				f.sums = strings.Repeat(strings.TrimPrefix(f.release.Assets[0].Digest, "sha256:")+"  "+f.release.Assets[0].Name+"\n", 2)
				f.release.Assets[0].Digest = ""
				f.release.Assets = append(f.release.Assets, asset{Name: "SHA256SUMS.txt", Size: int64(len(f.sums)), URL: repository + "/releases/download/v0.5.32/SHA256SUMS.txt"})
			}
			u.Download("0.5.32")
			s := waitFinished(t, u)
			if s.State != "error" || s.Error == "" || s.Task != "" {
				t.Fatalf("unsafe package was offered for installation: %+v", s)
			}
			entries, _ := os.ReadDir(u.cache)
			if len(entries) != 0 {
				t.Fatal("failed package left in cache")
			}
		})
	}
}

func TestChecksumFileAndPlatformPackages(t *testing.T) {
	u, f := newFixture(t)
	f.sums = strings.TrimPrefix(f.release.Assets[0].Digest, "sha256:") + "  " + f.release.Assets[0].Name + "\n"
	f.release.Assets[0].Digest = ""
	f.release.Assets = append(f.release.Assets, asset{Name: "SHA256SUMS.txt", Size: int64(len(f.sums)), URL: repository + "/releases/download/v0.5.32/SHA256SUMS.txt"})
	u.Download("0.5.32")
	if s := waitFinished(t, u); s.State != "ready" {
		t.Fatalf("checksum file was not accepted: %+v", s)
	}
	for _, tc := range []struct{ os, arch, via, suffix string }{{"windows", "amd64", "windows", "_x64-setup.exe"}, {"darwin", "arm64", "darwin", "_universal.dmg"}, {"darwin", "amd64", "darwin", "_universal.dmg"}, {"linux", "amd64", "linux", "_amd64.deb"}, {"linux", "amd64", "appimage", "_x86_64.AppImage"}, {"windows", "arm64", "windows", ""}} {
		u.goos, u.arch, u.state.InstallVia = tc.os, tc.arch, tc.via
		got := u.assetName("0.5.32")
		if tc.suffix == "" && got != "" || tc.suffix != "" && got != "uni-switch_0.5.32"+tc.suffix {
			t.Errorf("package for %+v: %s", tc, got)
		}
	}
}

type heldBody struct {
	ctx   context.Context
	first bool
}

func (b *heldBody) Read(data []byte) (int, error) {
	if !b.first {
		b.first = true
		return copy(data, "MZpartial"), nil
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (*heldBody) Close() error { return nil }

func TestCancelStopsDownloadAndRemovesOnlyItsCache(t *testing.T) {
	u, _ := newFixture(t)
	original := u.client.Transport
	u.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		res, err := original.RoundTrip(r)
		if err == nil && strings.HasSuffix(r.URL.Path, ".exe") {
			res.Body = &heldBody{ctx: r.Context()}
		}
		return res, err
	})
	sentinel := filepath.Join(u.cache, "keep.txt")
	os.WriteFile(sentinel, []byte("unrelated"), 0o600)
	u.Download("0.5.32")
	deadline := time.After(5 * time.Second)
	for u.Snapshot().Done == 0 {
		select {
		case <-deadline:
			t.Fatal("no partial progress was reported")
		case <-time.After(time.Millisecond):
		}
	}
	u.Cancel()
	if s := waitFinished(t, u); s.State != "available" || s.Task != "" || s.Error != "" {
		t.Fatalf("canceled download cannot retry: %+v", s)
	}
	entries, _ := os.ReadDir(u.cache)
	if len(entries) != 1 || entries[0].Name() != "keep.txt" {
		t.Fatal("cancel left a package or deleted another file")
	}
}
