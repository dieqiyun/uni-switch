//go:build !windows

package configuration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func credentialCommand(input string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	b, err := cmd.Output()
	return strings.TrimSuffix(string(b), "\n"), err
}
func readCredential(endpoint string) (*string, error) {
	var out string
	var err error
	if runtime.GOOS == "darwin" {
		out, err = credentialCommand("", "security", "find-internet-password", "-s", endpoint, "-a", "Bearer", "-w")
	} else {
		out, err = credentialCommand("", "secret-tool", "lookup", "url", endpoint, "username", "Bearer")
	}
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok && (runtime.GOOS == "darwin" && e.ExitCode() == 44 || runtime.GOOS != "darwin" && e.ExitCode() == 1 && len(strings.TrimSpace(string(e.Stderr))) == 0) {
			return nil, nil
		}
		return nil, fail("credential_store", "无法访问系统凭据库；Linux 需要已解锁的 Secret Service 和 secret-tool")
	}
	return &out, nil
}
func writeCredential(endpoint string, key *string) error {
	var err error
	if runtime.GOOS == "darwin" {
		if key == nil {
			_, err = credentialCommand("", "security", "delete-internet-password", "-s", endpoint, "-a", "Bearer")
		} else {
			home, _ := os.UserHomeDir()
			app := ""
			for _, p := range []string{filepath.Join(home, "Applications/Zed.app"), "/Applications/Zed.app", "/Applications/Zed Preview.app"} {
				if _, e := os.Stat(p); e == nil {
					app = p
					break
				}
			}
			if app == "" {
				return fail("credential_store", "找不到 Zed.app，无法授权钥匙串访问")
			}
			_, err = credentialCommand("", "security", "add-internet-password", "-U", "-T", app, "-T", "/usr/bin/security", "-s", endpoint, "-a", "Bearer", "-w", *key)
		}
	} else if key == nil {
		_, err = credentialCommand("", "secret-tool", "clear", "url", endpoint, "username", "Bearer")
	} else {
		_, err = credentialCommand(*key, "secret-tool", "store", "--label=zed-github-account", "url", endpoint, "username", "Bearer")
	}
	if err != nil {
		return fail("credential_store", "系统凭据库拒绝了 Zed 密钥操作，请解锁后重试")
	}
	return nil
}
