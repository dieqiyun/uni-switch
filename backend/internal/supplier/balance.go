package supplier

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dieqiyun/uni-switch/backend/internal/contract"
)

func ptr[T any](v T) *T { return &v }
func numeric(v any) (float64, bool) {
	var n float64
	var err error
	switch v := v.(type) {
	case json.Number:
		n, err = v.Float64()
	case string:
		n, err = strconv.ParseFloat(v, 64)
	default:
		return 0, false
	}
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
func number(v object, field string) (*float64, *contract.Error) {
	n, ok := numeric(v[field])
	if !ok {
		return nil, contract.Fail("balance_response", "接口缺少有效的余额或额度数值，请检查供应商接口")
	}
	return &n, nil
}
func text(v object, field string) *string {
	s := str(v[field])
	if s == "" {
		return nil
	}
	r := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s))
	if len(r) > 120 {
		r = r[:120]
	}
	return ptr(string(r))
}
func timestamp(v object, field string) *string {
	if s := text(v, field); s != nil {
		return s
	}
	if n, ok := integer(v[field]); ok && n > 0 {
		return ptr(strconv.FormatInt(n, 10))
	}
	return nil
}
func validateBalance(q contract.BalanceQuery) *contract.Error {
	switch q.Adapter {
	case "auto", "custom", "credit", "sub2api", "newapi_account", "newapi_token":
	default:
		return contract.Fail("invalid_balance_adapter", "余额查询类型无效")
	}
	if q.SiteURL != nil {
		if _, err := validateURL(*q.SiteURL); err != nil {
			return err
		}
	}
	if q.Adapter == "newapi_account" {
		valid := q.UserID != nil
		if valid {
			id := *q.UserID
			n, err := strconv.ParseUint(id, 10, 64)
			valid = len(id) > 0 && len(id) <= 20 && err == nil && n > 0
			for _, c := range id {
				valid = valid && c >= '0' && c <= '9'
			}
		}
		if !valid {
			return contract.Fail("balance_user_id", "请填写 New API 控制台的数字用户 ID")
		}
	}
	if q.Adapter == "custom" {
		if !strings.HasPrefix(q.Path, "/") || strings.HasPrefix(q.Path, "//") || strings.ContainsAny(q.Path, "?#\\") || strings.IndexFunc(q.Path, unicode.IsControl) >= 0 || len(q.Path) > 512 {
			return contract.Fail("invalid_balance_path", "余额接口请填写同一供应商的路径，以 / 开头，不含域名或查询参数")
		}
		valid := len(q.JSONPath) > 0 && len(q.JSONPath) <= 256
		for _, part := range strings.Split(q.JSONPath, ".") {
			valid = valid && part != ""
			for _, c := range part {
				valid = valid && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_')
			}
		}
		if !valid {
			return contract.Fail("invalid_balance_field", "余额字段请使用 data.balance 这样的 JSON 字段路径")
		}
	}
	if q.Divisor <= 0 || math.IsNaN(q.Divisor) || math.IsInf(q.Divisor, 0) || utf8.RuneCountInString(q.Unit) > 16 || strings.IndexFunc(q.Unit, unicode.IsControl) >= 0 {
		return contract.Fail("invalid_balance_unit", "余额换算除数必须大于 0，单位最多 16 个字符")
	}
	return nil
}
func balanceSite(base string, q contract.BalanceQuery) (*url.URL, *contract.Error) {
	if q.SiteURL != nil {
		base = *q.SiteURL
	}
	u, err := validateURL(base)
	if err != nil {
		return nil, err
	}
	path := strings.TrimRight(u.Path, "/")
	escaped := strings.TrimRight(u.EscapedPath(), "/")
	if q.SiteURL == nil {
		if strings.HasSuffix(path, "/backend-api/codex") {
			path = strings.TrimSuffix(path, "/backend-api/codex")
			escaped = strings.TrimSuffix(escaped, "/backend-api/codex")
		} else {
			path = strings.TrimSuffix(path, "/v1")
			escaped = strings.TrimSuffix(escaped, "/v1")
		}
	}
	u.Path = strings.TrimRight(path, "/") + "/"
	u.RawPath = strings.TrimRight(escaped, "/") + "/"
	return u, nil
}
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}
func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}
func balanceURL(site *url.URL, path string) (*url.URL, *contract.Error) {
	rel, err := url.Parse(path)
	if err != nil {
		return nil, contract.Fail("balance_path", "余额接口地址无效")
	}
	u := site.ResolveReference(rel)
	if !sameOrigin(site, u) || u.User != nil {
		return nil, contract.Fail("balance_path", "余额接口不能指向其他站点")
	}
	return u, nil
}
func successful(v object) bool {
	return v != nil && !isFalse(v["success"]) && !isFalse(v["code"]) && v["error"] == nil
}
func newBalance(q contract.BalanceQuery) *contract.Balance {
	return &contract.Balance{Unit: q.Unit, CheckedAt: time.Now().Unix(), Scope: "余额", Windows: []contract.Window{}}
}
func window(label string, v object, used, total, reset string) (*contract.Window, *contract.Error) {
	limit, ok := numeric(v[total])
	if !ok || limit <= 0 {
		return nil, nil
	}
	u, err := number(v, used)
	if err != nil {
		return nil, err
	}
	remaining := math.Max(limit-*u, 0)
	if math.IsInf(remaining, 0) {
		return nil, contract.Fail("balance_response", "余额换算结果无效")
	}
	return &contract.Window{Label: label, Remaining: remaining, Used: *u, Total: limit, ResetAt: timestamp(v, reset)}, nil
}
func minimumWindows(w []contract.Window) *float64 {
	if len(w) == 0 {
		return nil
	}
	n := w[0].Remaining
	for _, v := range w[1:] {
		n = math.Min(n, v.Remaining)
	}
	return &n
}

func parseAdapter(v object, q contract.BalanceQuery) (*contract.Balance, *contract.Error) {
	if err := validateBalance(q); err != nil {
		return nil, err
	}
	if !successful(v) || isFalse(v["isValid"]) || isFalse(v["is_active"]) {
		message := "供应商返回查询失败，请检查密钥和余额查询权限"
		if q.Adapter == "newapi_account" {
			message = "New API 账户查询失败，请检查控制台 Access Token、用户 ID 和账户访问权限"
		}
		return nil, contract.Fail("balance_response", message)
	}
	r := newBalance(q)
	switch q.Adapter {
	case "custom":
		var value any = v
		for _, part := range strings.Split(q.JSONPath, ".") {
			if list, ok := value.([]any); ok {
				i, err := strconv.ParseUint(part, 10, 64)
				if err != nil || i >= uint64(len(list)) {
					value = nil
				} else {
					value = list[i]
				}
			} else {
				value = obj(value)[part]
			}
		}
		n, ok := numeric(value)
		if !ok {
			return nil, contract.Fail("balance_field", "响应中没有有效的余额数值，请检查余额字段路径")
		}
		r.Amount = ptr(n / q.Divisor)
	case "credit":
		n, err := number(v, "total_available")
		if err != nil {
			return nil, err
		}
		r.Amount = n
	case "newapi_account", "newapi_token":
		d := obj(v["data"])
		if d == nil {
			return nil, contract.Fail("balance_response", "New API 接口未返回 data，请检查站点地址和查询类型")
		}
		account := q.Adapter == "newapi_account"
		used, amount, name := "total_used", "total_available", "name"
		r.Scope = "密钥额度"
		if account {
			used, amount, name = "used_quota", "quota", "group"
			r.Scope = "账户余额"
		}
		r.Unlimited = !account && isTrue(d["unlimited_quota"])
		n, err := number(d, used)
		if err != nil {
			return nil, err
		}
		r.Used = ptr(*n / q.Divisor)
		if !r.Unlimited {
			n, err = number(d, amount)
			if err != nil {
				return nil, err
			}
			r.Amount = ptr(*n / q.Divisor)
			if account {
				r.Total = ptr(*r.Amount + *r.Used)
			} else {
				n, err = number(d, "total_granted")
				if err != nil {
					return nil, err
				}
				r.Total = ptr(*n / q.Divisor)
			}
		}
		r.PlanName = text(d, name)
		r.ExpiresAt = timestamp(d, "expires_at")
	case "sub2api":
		r.Unit = "USD"
		r.PlanName = text(v, "planName")
		r.ExpiresAt = timestamp(v, "expires_at")
		if quota := obj(v["quota"]); quota != nil {
			r.Scope = "密钥额度"
			n, err := number(quota, "remaining")
			if err != nil {
				return nil, err
			}
			r.Amount = n
			n, err = number(quota, "used")
			if err != nil {
				return nil, err
			}
			r.Used = n
			n, err = number(quota, "limit")
			if err != nil {
				return nil, err
			}
			r.Total = n
		} else if n, ok := numeric(v["balance"]); ok {
			r.Scope = "账户余额"
			r.Amount = &n
		} else if sub := obj(v["subscription"]); sub != nil {
			r.Scope = "订阅额度"
			r.ExpiresAt = timestamp(sub, "expires_at")
			for _, pair := range [][2]string{{"daily", "每日"}, {"weekly", "每周"}, {"monthly", "每月"}} {
				w, err := window(pair[1], sub, pair[0]+"_usage_usd", pair[0]+"_limit_usd", pair[0]+"_reset_at")
				if err != nil {
					return nil, err
				}
				if w != nil {
					r.Windows = append(r.Windows, *w)
				}
			}
			if len(r.Windows) == 0 {
				n, ok := numeric(v["remaining"])
				r.Unlimited = ok && n == -1
				if !r.Unlimited {
					p, err := number(v, "remaining")
					if err != nil {
						return nil, err
					}
					r.Amount = p
				}
			} else {
				r.Amount = minimumWindows(r.Windows)
			}
		} else if str(v["mode"]) != "quota_limited" {
			n, err := number(v, "remaining")
			if err != nil {
				return nil, err
			}
			r.Unlimited = *n == -1
			r.Scope = "可用额度"
			if r.PlanName != nil && *r.PlanName == "钱包余额" {
				r.Scope = "账户余额"
			}
			if !r.Unlimited {
				r.Amount = n
			}
		}
		for i, v := range arr(v["rate_limits"]) {
			if i >= 20 {
				break
			}
			item := obj(v)
			label := "周期额度"
			switch str(item["window"]) {
			case "5h":
				label = "5 小时额度"
			case "1d":
				label = "每日额度"
			case "7d":
				label = "每周额度"
			}
			w, err := window(label, item, "used", "limit", "reset_at")
			if err != nil {
				return nil, err
			}
			if w != nil {
				r.Windows = append(r.Windows, *w)
			}
		}
		if r.Amount == nil && len(r.Windows) > 0 && !r.Unlimited {
			r.Scope = "周期额度"
			r.Amount = minimumWindows(r.Windows)
		}
		if r.Amount == nil && !r.Unlimited {
			return nil, contract.Fail("balance_response", "Sub2API 未返回余额或订阅额度，请检查站点版本和订阅状态")
		}
	default:
		return nil, contract.Fail("balance_response", "自动探测需要查询供应商接口")
	}
	for _, v := range []*float64{r.Amount, r.Used, r.Total} {
		if v != nil && (math.IsInf(*v, 0) || math.IsNaN(*v)) {
			return nil, contract.Fail("balance_response", "余额换算结果无效")
		}
	}
	return r, nil
}

func (s *Service) QueryBalance(ctx context.Context, c contract.Connection) (*contract.Balance, *contract.Error) {
	if c.Query == nil || c.Query.Adapter == "auto" {
		return s.AutoBalance(ctx, c)
	}
	q := *c.Query
	if err := validateBalance(q); err != nil {
		return nil, err
	}
	site, err := balanceSite(c.BaseURL, q)
	if err != nil {
		return nil, err
	}
	path := map[string]string{"sub2api": "v1/usage", "newapi_account": "api/user/self", "newapi_token": "api/usage/token/", "credit": "v1/dashboard/billing/credit_grants"}[q.Adapter]
	if q.Adapter == "custom" {
		path = q.Path
	}
	u, err := balanceURL(site, path)
	if err != nil {
		return nil, err
	}
	key := c.APIKey
	var user *string
	if q.Adapter == "newapi_account" {
		if c.AccountToken == nil || *c.AccountToken == "" {
			return nil, contract.Fail("balance_auth", "此查询需要控制台 Access Token 和用户 ID，API Key 无法替代")
		}
		key = *c.AccountToken
		user = q.UserID
	}
	v, err := s.get(ctx, u, &key, user, false, false, 15*time.Second)
	if err != nil {
		return nil, err
	}
	return parseAdapter(v, q)
}
