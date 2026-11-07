package gateway

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Probe follows provider.tinyBody: one 'hi', sixteen output tokens. It tests
// availability only; it cannot establish image/tool/stream capabilities.
type Probe struct {
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	AuthMode string `json:"authMode"`
	Model    string `json:"model"`
	Endpoint string `json:"endpoint"`
	Consent  bool   `json:"consent"`
}
type Observation struct {
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	State     string `json:"state"`
	CheckedAt int64  `json:"checkedAt"`
	Message   string `json:"message"`
}

func probeBody(p Probe) (Protocol, Object, error) {
	body := Object{"model": p.Model, "max_tokens": 16, "messages": []any{Object{"role": "user", "content": "hi"}}}
	switch p.Endpoint {
	case "messages":
		return Messages, body, nil
	case "responses":
		return Responses, Object{"model": p.Model, "max_output_tokens": 16, "input": []any{Object{"type": "message", "role": "user", "content": []any{Object{"type": "input_text", "text": "hi"}}}}}, nil
	case "chat_completions":
		u, _ := url.Parse(p.BaseURL)
		host := ""
		if u != nil {
			host = strings.ToLower(u.Hostname())
		}
		if host == "api.openai.com" || strings.HasSuffix(host, ".openai.azure.com") || strings.HasSuffix(host, ".services.ai.azure.com") || strings.Contains(host, "bedrock-mantle.") {
			delete(body, "max_tokens")
			body["max_completion_tokens"] = 16
		}
		return Chat, body, nil
	}
	return "", nil, invalid("未知测试接口")
}
func (s *Service) TestConnection(parent context.Context, p Probe) (Observation, error) {
	out := Observation{Model: p.Model, Endpoint: p.Endpoint, State: "unknown", CheckedAt: time.Now().Unix()}
	if !p.Consent {
		return out, invalid("请先确认测试可能计费")
	}
	if strings.TrimSpace(p.Model) == "" || strings.TrimSpace(p.APIKey) == "" {
		return out, invalid("缺少模型或密钥")
	}
	to, body, e := probeBody(p)
	if e != nil {
		return out, e
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	response, e := s.post(ctx, Call{BaseURL: p.BaseURL, APIKey: p.APIKey, AuthMode: p.AuthMode}, to, body)
	if e != nil {
		out.Message = "连接失败或超时，请检查地址、网络和证书"
		return out, nil
	}
	defer response.Body.Close()
	if _, e = limited(response.Body); e != nil {
		out.Message = "连接已建立，但响应读取失败"
		return out, nil
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		out.State = "available"
		out.Message = "本次模型连接测试成功；不代表图片、工具或流式能力已验证"
	} else {
		out.State = "unavailable"
		out.Message = fmt.Sprintf("本次模型连接测试失败：上游 HTTP %d", response.StatusCode)
	}
	return out, nil
}
