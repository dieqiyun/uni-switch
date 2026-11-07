package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var prefillRejection = regexp.MustCompile(`(?i)does not support (?:assistant message )?prefill|requests ending with a model turn are not supported`)

func (r *Request) endsWithAssistant() bool {
	if len(r.Turns) == 0 || r.Turns[len(r.Turns)-1].Role != "assistant" {
		return false
	}
	for _, part := range r.Turns[len(r.Turns)-1].Parts {
		if part.Kind == "call" {
			return false
		}
	}
	return true
}

func (r *Request) modelLastAsk() string {
	if r.summarizing() {
		return "Write the summary of the conversation above that the system prompt asks for."
	}
	return "Continue."
}

// Protocol fallback needs evidence about the endpoint, not just a 404. Match
// decoded vendor words (including JSON escaped Unicode) like WrongEndpoint.
func wrongEndpoint(status int, body []byte) bool {
	if status < 400 || status >= 500 || status == 401 || status == 403 || status == 429 {
		return false
	}
	words := string(body)
	var decoded any
	if decode(body, &decoded) == nil {
		var walk func(any)
		walk = func(v any) {
			switch x := v.(type) {
			case string:
				words += "\n" + x
			case map[string]any:
				for _, v := range x {
					walk(v)
				}
			case []any:
				for _, v := range x {
					walk(v)
				}
			}
		}
		walk(decoded)
	}
	words = strings.ToLower(words)
	for _, phrase := range []string{"not a chat model", "not supported in the v1/chat/completions", "not supported in /v1/chat/completions", "not supported in the v1/responses", "not supported in /v1/responses", "only supported in v1/responses", "only supported in /v1/responses", "use v1/completions", "use /v1/completions", "use v1/responses", "use /v1/responses", "use v1/chat/completions", "use /v1/chat/completions", "not accessible via the", "unsupported_api_for_model", "no model endpoints available given user constraints", "is not supported for format", "does not support this protocol", "requires responses", "不支持 /v1/chat/completions 接口", "改用 /v1/responses"} {
		if strings.Contains(words, phrase) {
			return true
		}
	}
	return false
}

func prefillMode(base string, to Protocol, model string) string {
	if to == Messages {
		return "anthropic"
	}
	if to != Chat {
		return ""
	}
	u, e := url.Parse(base)
	if e != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "api.deepseek.com" || strings.HasSuffix(host, ".deepseek.com") {
		return "prefix"
	}
	if strings.Contains(host, "moonshot") || strings.Contains(host, "kimi") {
		return "partial"
	}
	ip := net.ParseIP(host)
	if host != "localhost" && !strings.HasSuffix(host, ".local") && (ip == nil || !ip.IsLoopback() && !ip.IsPrivate()) {
		return ""
	}
	model = strings.ToLower(model)
	if strings.Contains(model, "deepseek") {
		return "prefix"
	}
	if strings.Contains(model, "kimi") {
		return "partial"
	}
	return ""
}

// Resume only a transport cut with an actual prefix and no started tool. A
// refusal, malformed content or a normal Chat re-answer is never spliced in.
func resumable(err error) bool {
	var network net.Error
	return errors.Is(err, errTruncated) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &network)
}

func (s *Service) translateStream(ctx context.Context, c Call, to Protocol, r *Request, model Model, response *http.Response, emit func(Event) error) error {
	mode := prefillMode(c.BaseURL, to, model.ID)
	var text, thought strings.Builder
	tools := false
	nextIndex := 0
	previousUsage := Object{}
	for attempt := 0; ; attempt++ {
		currentUsage := Object{}
		indices := map[int]int{}
		parts := map[int]Part{}
		closed := map[int]bool{}
		order := []int{}
		suppressed := map[int]bool{}
		echo, seen := "", ""
		if attempt > 0 {
			echo = strings.TrimRightFunc(text.String(), unicode.IsSpace)
		}
		unecho := func(delta string) string {
			if echo == "" {
				return delta
			}
			seen += delta
			if len(seen) < len(echo) && strings.HasPrefix(echo, seen) {
				return ""
			}
			out := seen
			if strings.HasPrefix(seen, echo) {
				out = strings.TrimPrefix(seen, echo)
			}
			echo, seen = "", ""
			return out
		}
		timer := time.AfterFunc(90*time.Second, func() { response.Body.Close() })
		err := DecodeStream(to, &idleReader{response.Body, timer}, func(ev Event) error {
			if ev.Kind == "usage" || ev.Kind == "finish" {
				currentUsage = ev.Usage
				merged := Object{}
				for _, key := range []string{"input_tokens", "output_tokens", "cached_tokens", "cache_write_tokens", "reasoning_tokens"} {
					merged[key] = integer(previousUsage[key]) + integer(ev.Usage[key])
				}
				ev.Usage = merged
			}
			if ev.Kind == "part" {
				if attempt > 0 && ev.Part.Kind == "thinking" {
					suppressed[ev.Index] = true
					return nil
				}
				indices[ev.Index] = nextIndex
				nextIndex++
				order = append(order, ev.Index)
				p := ev.Part
				if p.Kind == "text" {
					p.Text = unecho(p.Text)
					text.WriteString(p.Text)
				}
				if p.Kind == "thinking" {
					thought.WriteString(p.Text)
				}
				if p.Kind == "call" {
					tools = true
				}
				parts[ev.Index] = p
				ev.Part = p
			}
			if suppressed[ev.Index] && (ev.Kind == "delta" || ev.Kind == "part_done" || ev.Kind == "part_update") {
				return nil
			}
			if ev.Kind == "delta" {
				p := parts[ev.Index]
				if ev.Field == "text" {
					if p.Kind == "text" {
						ev.Text = unecho(ev.Text)
						text.WriteString(ev.Text)
					} else if p.Kind == "thinking" {
						thought.WriteString(ev.Text)
					}
					p.Text += ev.Text
				} else if ev.Field == "signature" {
					p.Signature += ev.Text
				}
				parts[ev.Index] = p
			}
			if ev.Kind == "part_done" {
				closed[ev.Index] = true
				if attempt > 0 && ev.Part.Kind == "text" {
					ev.Part.Text = parts[ev.Index].Text
				}
			}
			if ev.Kind == "part" || ev.Kind == "delta" || ev.Kind == "part_done" || ev.Kind == "part_update" {
				ev.Index = indices[ev.Index]
			}
			return emit(ev)
		})
		timer.Stop()
		response.Body.Close()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt >= 2 || mode == "" || tools || text.Len()+thought.Len() == 0 || !resumable(err) {
			return err
		}
		for _, key := range []string{"input_tokens", "output_tokens", "cached_tokens", "cache_write_tokens", "reasoning_tokens"} {
			previousUsage[key] = integer(previousUsage[key]) + integer(currentUsage[key])
		}
		// Close the prefix's blocks, retaining a single client reply envelope.
		for _, i := range order {
			if !closed[i] {
				if e := emit(Event{Kind: "part_done", Index: indices[i], Part: parts[i]}); e != nil {
					return e
				}
			}
		}
		resume := *r
		resume.Resume = true
		resume.Turns = append([]Turn(nil), r.Turns...)
		ps := []Part{}
		if thought.Len() > 0 {
			ps = append(ps, Part{Kind: "thinking", Text: thought.String()})
		}
		if prefix := strings.TrimRightFunc(text.String(), unicode.IsSpace); prefix != "" {
			ps = append(ps, Part{Kind: "text", Text: prefix})
		}
		resume.Turns = append(resume.Turns, Turn{Role: "assistant", Parts: ps})
		body, e := Build(&resume, to, model, c.BaseURL)
		if e != nil {
			return e
		}
		next := c
		if mode == "prefix" {
			u, _ := url.Parse(c.BaseURL)
			u.Path = "/beta"
			next.BaseURL = u.String()
		}
		response, e = s.post(ctx, next, to, body)
		if e != nil {
			return e
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return errors.New("上游拒绝断流续接，回复未完整结束")
		}
		if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
			response.Body.Close()
			return errors.New("上游续接没有返回 SSE 流")
		}
	}
}
