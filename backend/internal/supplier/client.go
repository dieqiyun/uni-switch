// Package supplier implements uni-switch's provider metadata requests. It does
// not import, execute, or depend on Magpie.
package supplier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/dieqiyun/uni-switch/backend/internal/contract"
)

type object = map[string]any

type Service struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[[32]byte]cachedStrategy
}

func New() *Service {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 8 * time.Second
	return &Service{
		client: &http.Client{Transport: transport, Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		cache: make(map[[32]byte]cachedStrategy),
	}
}

func validateURL(raw string) (*url.URL, *contract.Error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, contract.Fail("invalid_url", "API 地址必须是完整的 http 或 https URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || strings.Contains(raw, "\\") || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return nil, contract.Fail("invalid_url", "API 地址不能包含账号、密码、查询参数或片段")
	}
	return u, nil
}

// Error messages never contain URLs, response bodies, credentials, or low-level
// transport errors, which can embed userinfo and request headers.
func (s *Service) get(ctx context.Context, u *url.URL, key, user *string, anthropic, versionHeader bool, timeout time.Duration) (object, *contract.Error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, contract.Fail("network", "无法建立供应商连接")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "uni-switch-core/1")
	if versionHeader || anthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	if key != nil {
		if anthropic {
			req.Header.Set("x-api-key", *key)
		} else {
			req.Header.Set("Authorization", "Bearer "+*key)
		}
	}
	if user != nil {
		req.Header.Set("New-Api-User", *user)
	}
	response, err := s.client.Do(req)
	if err != nil {
		var network net.Error
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
			return nil, contract.Fail("network", "供应商查询超时，请稍后重试")
		}
		return nil, contract.Fail("network", "无法连接供应商，请检查地址、网络和证书")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		hint := "供应商接口暂时无法查询"
		switch response.StatusCode {
		case 401, 403:
			hint = "密钥无效或没有此查询接口的权限"
		case 404, 405:
			hint = "供应商不支持此接口，请检查查询路径"
		case 429:
			hint = "供应商限制了查询频率，请稍后重试"
		default:
			if response.StatusCode >= 300 && response.StatusCode < 400 {
				hint = "接口要求重定向，请填写最终地址后重试"
			}
		}
		return nil, &contract.Error{Code: "supplier_http", Message: hint + "（HTTP " + itoa(response.StatusCode) + "）", Status: response.StatusCode}
	}
	const limit = 2 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, contract.Fail("network", "读取供应商响应失败")
	}
	if len(data) > limit {
		return nil, contract.Fail("response_limit", "供应商响应超过 2 MB，无法读取")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value object
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, contract.Fail("supplier_json", "供应商返回了无效 JSON，请检查接口路径")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, contract.Fail("supplier_json", "供应商返回了无效 JSON，请检查接口路径")
	}
	return value, nil
}
