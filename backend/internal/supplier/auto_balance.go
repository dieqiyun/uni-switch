package supplier

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/contract"
)

type cachedStrategy struct {
	kind  string
	until time.Time
}
type probe struct {
	service  *Service
	ctx      context.Context
	site     *url.URL
	failures []*contract.Error
}

func (p *probe) get(path string, key, user *string) (object, *contract.Error) {
	if p.ctx.Err() != nil {
		return nil, contract.Fail("balance_timeout", "自动查询超时，请稍后刷新")
	}
	u, err := balanceURL(p.site, path)
	if err != nil {
		return nil, err
	}
	v, err := p.service.get(p.ctx, u, key, user, false, false, 4*time.Second)
	if err != nil {
		p.failures = append(p.failures, err)
	}
	return v, err
}
func (p *probe) limited() bool {
	for _, e := range p.failures {
		if e.Status == 429 {
			return true
		}
	}
	return false
}
func (p *probe) failure() *contract.Error {
	for _, e := range p.failures {
		if e.Status == 429 {
			return e
		}
	}
	if p.ctx.Err() != nil {
		return contract.Fail("balance_timeout", "自动查询超时，请检查网络后刷新")
	}
	for _, e := range p.failures {
		if e.Status == 401 || e.Status == 403 {
			return contract.Fail("balance_auth", "余额接口拒绝访问，请检查 API Key、IP 限制或查询权限")
		}
	}
	for _, e := range p.failures {
		if e.Code == "network" {
			return contract.Fail("network", "无法连接余额接口，请检查供应商地址和网络后刷新")
		}
	}
	return contract.Fail("balance_unsupported", "未找到可用的余额接口；供应商可能未开放 API Key 查询")
}
func autoQuery(adapter string) contract.BalanceQuery {
	return contract.BalanceQuery{Adapter: adapter, Unit: "USD", Divisor: 500_000}
}
func subShape(v object) bool {
	return successful(v) && (str(v["mode"]) == "unrestricted" || str(v["mode"]) == "quota_limited" || (v["isValid"] != nil || v["is_active"] != nil) && (v["remaining"] != nil || v["quota"] != nil || v["balance"] != nil))
}
func tokenShape(v object) bool {
	d := obj(v["data"])
	return successful(v) && d != nil && (str(d["object"]) == "token_usage" || d["total_available"] != nil && d["total_used"] != nil && d["unlimited_quota"] != nil)
}
func statusSettings(status object) (float64, string, float64) {
	d := obj(status["data"])
	divisor := 500_000.0
	if successful(status) {
		if n, ok := numeric(d["quota_per_unit"]); ok && n > 0 {
			divisor = n
		}
	} else {
		d = nil
	}
	switch str(d["quota_display_type"]) {
	case "TOKENS":
		return 1, "tokens", 1
	case "CNY":
		if n, ok := numeric(d["usd_exchange_rate"]); ok && n > 0 {
			return divisor, "CNY", n
		}
	}
	return divisor, "USD", 1
}
func tokenResult(v, status object, legacy *contract.BalanceQuery) (*contract.Balance, *contract.Error) {
	divisor, unit, rate := statusSettings(status)
	q := autoQuery("newapi_token")
	q.Divisor = divisor
	q.Unit = unit
	if status == nil && legacy != nil && legacy.Adapter == "newapi_token" {
		q.Divisor = legacy.Divisor
		q.Unit = legacy.Unit
	}
	r, err := parseAdapter(v, q)
	if err != nil {
		return nil, err
	}
	for _, v := range []*float64{r.Amount, r.Used, r.Total} {
		if v != nil {
			*v *= rate
			if math.IsNaN(*v) || math.IsInf(*v, 0) {
				return nil, contract.Fail("balance_response", "站点额度换算无效")
			}
		}
	}
	r.ProviderType = ptr("New API")
	return r, nil
}
func parseBilling(subscription, usage, status object, token *contract.Balance) (*contract.Balance, *contract.Error) {
	if !successful(subscription) || !successful(usage) || str(subscription["object"]) != "billing_subscription" {
		return nil, contract.Fail("balance_response", "站点计费接口格式不匹配")
	}
	total, err := number(subscription, "hard_limit_usd")
	if err != nil {
		return nil, err
	}
	used, err := number(usage, "total_usage")
	if err != nil {
		return nil, err
	}
	*used /= 100
	amount := *total - *used
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return nil, contract.Fail("balance_response", "站点额度无效")
	}
	unit := "USD"
	switch str(obj(status["data"])["quota_display_type"]) {
	case "CNY":
		unit = "CNY"
	case "TOKENS":
		unit = "tokens"
	}
	r := newBalance(autoQuery("auto"))
	r.Unit = unit
	r.Scope = "可用额度"
	r.Amount = &amount
	r.Total = total
	r.Used = used
	r.Unlimited = token != nil && token.Unlimited && *total == 100_000_000
	if r.Unlimited {
		r.Amount = nil
		r.Total = nil
	}
	if n, ok := integer(subscription["access_until"]); ok && n > 0 {
		r.ExpiresAt = ptr(strconv.FormatInt(n, 10))
	}
	r.ProviderType = ptr("兼容计费接口")
	if token != nil {
		r.ProviderType = ptr("New API")
	}
	r.Note = ptr("由站点决定返回账户余额或密钥额度")
	return r, nil
}

// The hash binds cache entries to every input affecting endpoint, credential or
// currency selection. Neither credentials nor hashes are written to disk/logs.
func cacheKey(c contract.Connection) [32]byte { data, _ := json.Marshal(c); return sha256.Sum256(data) }
func (s *Service) AutoBalance(ctx context.Context, c contract.Connection) (*contract.Balance, *contract.Error) {
	q := autoQuery("auto")
	if c.Query != nil {
		q = *c.Query
	}
	if err := validateBalance(q); err != nil {
		return nil, err
	}
	site, err := balanceSite(c.BaseURL, q)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	fingerprint := cacheKey(c)
	s.mu.Lock()
	cached, ok := s.cache[fingerprint]
	s.mu.Unlock()
	if ok && cached.until.After(time.Now()) && c.AccountToken == nil {
		cacheCtx, stop := context.WithTimeout(ctx, 12*time.Second)
		p := &probe{service: s, ctx: cacheCtx, site: site}
		r, failure := p.cached(c, cached.kind)
		limited := p.limited()
		stop()
		if limited {
			return nil, p.failure()
		}
		if failure == nil {
			return r, nil
		}
		s.mu.Lock()
		delete(s.cache, fingerprint)
		s.mu.Unlock()
	}
	probeCtx, stop := context.WithTimeout(ctx, 22*time.Second)
	defer stop()
	p := &probe{service: s, ctx: probeCtx, site: site}
	r, err := p.discover(c)
	if err != nil {
		return nil, err
	}
	kind := ""
	if r.ProviderType != nil {
		switch *r.ProviderType {
		case "Sub2API":
			kind = "sub2api"
		case "New API":
			if r.Scope == "密钥额度" {
				kind = "token"
			} else if r.Scope == "可用额度" && !r.Unlimited {
				kind = "billing_newapi"
			}
		case "兼容计费接口":
			if !r.Unlimited {
				kind = "billing"
			}
		case "兼容余额接口":
			kind = "credit"
		}
	}
	if kind != "" {
		s.mu.Lock()
		for key, entry := range s.cache {
			if !entry.until.After(time.Now()) {
				delete(s.cache, key)
			}
		}
		if len(s.cache) >= 100 {
			clear(s.cache)
		}
		s.cache[fingerprint] = cachedStrategy{kind: kind, until: time.Now().Add(15 * time.Minute)}
		s.mu.Unlock()
	}
	return r, nil
}
func (p *probe) publicStatus() object {
	if p.limited() {
		return nil
	}
	v, err := p.get("api/status", nil, nil)
	if err != nil || !successful(v) {
		return nil
	}
	return v
}
func (p *probe) cached(c contract.Connection, kind string) (*contract.Balance, *contract.Error) {
	switch kind {
	case "sub2api":
		v, err := p.get("v1/usage", &c.APIKey, nil)
		if err != nil {
			return nil, err
		}
		if !subShape(v) {
			return nil, contract.Fail("balance_response", "余额接口已变化")
		}
		r, err := parseAdapter(v, autoQuery("sub2api"))
		if err == nil {
			r.ProviderType = ptr("Sub2API")
		}
		return r, err
	case "token":
		v, err := p.get("api/usage/token/", &c.APIKey, nil)
		status := p.publicStatus()
		if err != nil {
			return nil, err
		}
		if !tokenShape(v) {
			return nil, contract.Fail("balance_response", "余额接口已变化")
		}
		r, err := tokenResult(v, status, c.Query)
		if err == nil {
			r.Note = ptr("当前为密钥额度；密钥不限额不代表账户余额无限")
		}
		return r, err
	case "billing", "billing_newapi":
		sub, err := p.get("v1/dashboard/billing/subscription", &c.APIKey, nil)
		if err != nil {
			return nil, err
		}
		usage, err := p.get("v1/dashboard/billing/usage", &c.APIKey, nil)
		if err != nil {
			return nil, err
		}
		status := p.publicStatus()
		var token *contract.Balance
		if kind == "billing_newapi" && !p.limited() {
			if v, err := p.get("api/usage/token/", &c.APIKey, nil); err == nil {
				token, _ = tokenResult(v, status, c.Query)
			}
		}
		return parseBilling(sub, usage, status, token)
	case "credit":
		v, err := p.get("v1/dashboard/billing/credit_grants", &c.APIKey, nil)
		if err != nil {
			return nil, err
		}
		r, err := parseAdapter(v, autoQuery("credit"))
		if err == nil {
			r.ProviderType = ptr("兼容余额接口")
		}
		return r, err
	}
	return nil, contract.Fail("balance_response", "余额接口已变化")
}
func (p *probe) discover(c contract.Connection) (*contract.Balance, *contract.Error) {
	if v, err := p.get("v1/usage", &c.APIKey, nil); err == nil && subShape(v) {
		r, err := parseAdapter(v, autoQuery("sub2api"))
		if err == nil {
			r.ProviderType = ptr("Sub2API")
		}
		return r, err
	}
	if p.limited() {
		return nil, p.failure()
	}
	raw, err := p.get("api/usage/token/", &c.APIKey, nil)
	if err != nil || !tokenShape(raw) {
		raw = nil
	}
	if p.limited() {
		return nil, p.failure()
	}
	status := p.publicStatus()
	if p.limited() {
		return nil, p.failure()
	}
	var token *contract.Balance
	if raw != nil {
		token, err = tokenResult(raw, status, c.Query)
		if err != nil {
			return nil, err
		}
	}
	if q := c.Query; q != nil && q.Adapter == "newapi_account" && c.AccountToken != nil {
		if v, err := p.get("api/user/self", c.AccountToken, q.UserID); err == nil {
			if r, err := parseAdapter(v, *q); err == nil {
				r.ProviderType = ptr("New API")
				return r, nil
			}
		}
	}
	if p.limited() {
		return nil, p.failure()
	}
	if sub, err := p.get("v1/dashboard/billing/subscription", &c.APIKey, nil); err == nil && successful(sub) && str(sub["object"]) == "billing_subscription" {
		if usage, err := p.get("v1/dashboard/billing/usage", &c.APIKey, nil); err == nil {
			if r, err := parseBilling(sub, usage, status, token); err == nil {
				return r, nil
			}
		}
	}
	if p.limited() {
		return nil, p.failure()
	}
	if token != nil {
		token.Note = ptr("当前为密钥额度；账户余额接口未返回可用结果")
		return token, nil
	}
	if v, err := p.get("v1/dashboard/billing/credit_grants", &c.APIKey, nil); err == nil && successful(v) {
		if _, ok := numeric(v["total_available"]); ok {
			r, err := parseAdapter(v, autoQuery("credit"))
			if err == nil {
				r.ProviderType = ptr("兼容余额接口")
			}
			return r, err
		}
	}
	if p.limited() {
		return nil, p.failure()
	}
	if q := c.Query; q != nil && q.Adapter == "custom" {
		if v, err := p.get(q.Path, &c.APIKey, nil); err == nil {
			if r, err := parseAdapter(v, *q); err == nil {
				r.ProviderType = ptr("已保存的查询接口")
				return r, nil
			}
		}
	}
	return nil, p.failure()
}
