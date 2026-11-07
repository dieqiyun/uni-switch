package accounts

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func ClaudeBinary() (string, error) {
	if p := os.Getenv("UNI_SWITCH_CLAUDE_BINARY"); p != "" {
		if filepath.IsAbs(p) {
			return p, nil
		}
		return "", errors.New("Claude Code 可执行文件路径必须是绝对路径")
	}
	if home, err := os.UserHomeDir(); err == nil {
		name := "claude"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		candidate := filepath.Join(home, ".local", "bin", name)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("claude.exe"); err == nil {
			return p, nil
		}
	}
	p, e := exec.LookPath("claude")
	if e != nil {
		return "", errors.New("请先安装 Claude Code，再添加 Claude 订阅账号")
	}
	if runtime.GOOS == "windows" && (strings.EqualFold(filepath.Ext(p), ".cmd") || strings.EqualFold(filepath.Ext(p), ".bat")) {
		return "", errors.New("请安装原生 Claude Code（claude.exe），订阅桥接不通过命令脚本启动")
	}
	return p, nil
}
func ClaudeEnv(dir string) []string {
	out := []string{}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "ANTHROPIC_") || strings.HasPrefix(upper, "CLAUDE_") || upper == "CLAUDECODE" || upper == "BROWSER" || upper == "UNI_SWITCH_LOGIN_URL" {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "CLAUDE_CONFIG_DIR="+dir)
}
func (m *Manager) claudeLogin(ctx context.Context, f *flow) {
	defer f.cancel()
	binary, e := ClaudeBinary()
	if e != nil {
		f.set("failed", e.Error())
		return
	}
	dir := filepath.Join(m.Directory, "subscriptions", "claude", f.status.ID)
	if e = edit.PrivateDirectory(dir); e != nil {
		f.set("failed", "无法创建独立 Claude 登录目录")
		return
	}
	urlFile := filepath.Join(dir, ".login-url")
	executable, _ := os.Executable()
	cmd := exec.CommandContext(ctx, binary, "auth", "login")
	hide(cmd)
	cmd.Dir = dir
	cmd.Env = append(ClaudeEnv(dir), "BROWSER="+executable, "UNI_SWITCH_LOGIN_URL="+urlFile)
	stdin, e := cmd.StdinPipe()
	if e != nil {
		f.set("failed", "无法启动 Claude 登录")
		return
	}
	defer stdin.Close()
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		f.set("failed", "无法启动 Claude Code")
		return
	}
	go func() {
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			for _, word := range strings.Fields(scanner.Text()) {
				word = strings.Trim(word, "\x1b[]()\"'")
				if validLoginURL(word) {
					f.mu.Lock()
					f.status.URL = word
					f.mu.Unlock()
				}
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			f.set("failed", "Claude 登录已过期")
			<-done
			return
		case code := <-f.paste:
			if len(code) > 4096 || strings.ContainsAny(code, "\r\n\x00") {
				f.set("failed", "登录代码格式无效")
				return
			}
			_, _ = io.WriteString(stdin, code+"\n")
		case <-ticker.C:
			if b, err := os.ReadFile(urlFile); err == nil && len(b) < 16384 && validLoginURL(string(b)) {
				f.mu.Lock()
				f.status.URL = string(b)
				f.mu.Unlock()
			}
		case err := <-done:
			if err != nil {
				f.set("failed", "Claude Code 登录未成功，请重新登录")
				return
			}
			probe := exec.CommandContext(ctx, binary, "auth", "status", "--json")
			hide(probe)
			probe.Env = ClaudeEnv(dir)
			probe.Dir = dir
			b, err := probe.Output()
			var info map[string]any
			if err != nil || json.Unmarshal(b, &info) != nil || info["loggedIn"] != true {
				f.set("failed", "Claude Code 未确认登录成功")
				return
			}
			user := s(info["email"])
			if user == "" {
				user = s(info["accountUuid"])
			}
			if user == "" {
				f.set("failed", "Claude 登录缺少账号身份")
				return
			}
			name := f.name
			if name == "" {
				name = user
			}
			a := Account{ID: f.status.ID, Kind: "claude", Name: name, User: user, Enabled: true, ConfigDir: dir}
			if ctx.Err() != nil {
				return
			}
			if err = m.store(a); err != nil {
				f.set("failed", err.Error())
			} else {
				f.set("done", "登录成功，Claude Code 会自动续期")
			}
			return
		}
	}
}
