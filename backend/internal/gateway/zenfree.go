package gateway

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// OpenCode Zen's free models (-free) are served to OpenCode alone: a
// request must carry OpenCode's headers (provider.OpenCodeClient), be
// streamed, and offer OpenCode's core tools, or Zen answers 403
// "OpenCode's free tier can only be used from within OpenCode"; be asked
// at OpenCode 2's paths (provider.OpenCodeFreeURL), with each core tool
// taking OpenCode's parameters (provider.OpenCodeToolSchemas); and since
// late October 2026 carry OpenCode 2's User-Agent and its environment
// block in the system prompt (provider.OpenCodeEnvironment), or Zen
// answers 429 "Free usage limit reached. Free models work in OpenCode"
// (#3491). The user allowed uni-switch to ask them as OpenCode does, on this
// route only. So a request to one is never relayed as it came: it is
// translated (which always streams upstream, and gathers the stream into
// one reply for a client that didn't ask for a stream), and on the way
//
//   - a tool of the client's named as a core tool in another case (Claude
//     Code's Read, Edit, Glob and Grep) is offered under the lowercase name,
//     and the model's calls of it go back to the client under the client's
//     own;
//   - a missing core tool (Claude Code's Bash is not shell) is offered as
//     a stub that says never to call it, with OpenCode's own parameters.
//     On Chat each tool says strict false, as OpenCode's do. tool_choice
//     is left as the client sent it. A call of a stub is never handed on:
//     it is left out of the reply, and a turn that called nothing else ends
//     there, with what the model said so far (a short note when it said
//     nothing), as a plain stop rather than a tool call;
//   - OpenCode's environment block is added to the system prompt, for the
//     folder the agent says it works in (the home folder when it says
//     none), unless the prompt has one already (OpenCode's own).

// zenTools is how one request's tools were offered to Zen's free tier.
type zenTools struct {
	back  map[string]string // name offered under → the client's name
	stubs map[string]bool   // names offered as stubs
}

// zenStubNote is the reply to a turn that called nothing but a stub.
const zenStubNote = "(The model asked for a tool this session doesn't have.)"

// zenFreeTools offers r's tools as Zen's free tier wants them, renaming
// them in the conversation so far too.
func zenFreeTools(r *Request) *zenTools {
	z := &zenTools{back: map[string]string{}, stubs: map[string]bool{}}
	tools := slices.Clone(r.Tools)
	for _, need := range provider.OpenCodeTools {
		i := slices.IndexFunc(tools, func(t Tool) bool { return t.Name == need })
		if i < 0 {
			i = slices.IndexFunc(tools, func(t Tool) bool { return strings.EqualFold(t.Name, need) })
		}
		if i >= 0 {
			if tools[i].Name != need {
				z.back[need] = tools[i].Name
				tools[i].Name = need
			}
			continue
		}
		tools = append(tools, Tool{Name: need, Description: provider.OpenCodeStub, Schema: json.RawMessage(provider.OpenCodeToolSchemas[need])})
		z.stubs[need] = true
	}
	r.Tools = tools
	r.strictSaid = true
	zenEnvironment(r)
	if len(z.back) == 0 {
		return z
	}
	to := map[string]string{}
	for up, own := range z.back {
		to[own] = up
	}
	msgs := slices.Clone(r.Messages)
	for i, m := range msgs {
		var parts []Part
		for j, p := range m.Parts {
			if up, ok := to[p.Name]; ok && p.Kind == ToolCall {
				if parts == nil {
					parts = slices.Clone(m.Parts)
				}
				parts[j].Name = up
			}
		}
		if parts != nil {
			msgs[i].Parts = parts
		}
	}
	r.Messages = msgs
	if name, ok := strings.CutPrefix(r.ToolChoice, "name:"); ok {
		if up, ok := to[name]; ok {
			r.ToolChoice = "name:" + up
		}
	}
	return z
}

// zenEnvironment adds OpenCode's environment block to r's system prompt,
// unless the prompt carries one already.
func zenEnvironment(r *Request) {
	if provider.OpenCodeHasEnvironment(r.System) {
		return
	}
	for _, m := range r.Messages {
		if m.Role != "system" && m.Role != "developer" {
			continue
		}
		for _, p := range m.Parts {
			if p.Kind == Text && provider.OpenCodeHasEnvironment(p.Text) {
				return
			}
		}
	}
	env := provider.OpenCodeEnvironment(openCodeDir(workDir(r)))
	if r.System == "" {
		r.System = env
	} else {
		r.System += "\n\n" + env
	}
}

// openCodeDir is dir when it is a folder OpenCode could name, an absolute
// path (or one under ~), else "" for the home folder.
func openCodeDir(dir string) string {
	if dir == "/" || dir == "~/" {
		return ""
	}
	if strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `\\`) ||
		len(dir) >= 3 && dir[1] == ':' && (dir[2] == '\\' || dir[2] == '/') {
		return dir
	}
	return ""
}

// zenReply turns the model's events back into the client's: its tools'
// names, and no call of a stub. One is made for each reply.
type zenReply struct {
	z        *zenTools
	dropping bool // the open tool call is a stub's
	stubbed  bool // a stub was called
	calls    int  // calls handed on
	said     bool // text handed on
}

func (f *zenReply) see(ev Event, emit func(Event)) {
	switch ev.Kind {
	case KToolStart:
		if f.dropping = f.z.stubs[ev.Name]; f.dropping {
			f.stubbed = true
			return
		}
		if own, ok := f.z.back[ev.Name]; ok {
			ev.Name = own
		}
		f.calls++
	case KToolArgs:
		if f.dropping {
			return
		}
	case KText:
		f.dropping = false
		f.said = f.said || ev.Text != ""
	case KThink, KSearch:
		f.dropping = false
	case KStop:
		f.dropping = false
		if f.stubbed && f.calls == 0 {
			f.end(emit)
			if ev.Stop == "tool" {
				ev.Stop = "stop"
			}
		}
	}
	emit(ev)
}

// end is the reply ending: one that called nothing but a stub, and said
// nothing, says so.
func (f *zenReply) end(emit func(Event)) {
	if f.stubbed && f.calls == 0 && !f.said {
		emit(Event{Kind: KText, Text: zenStubNote})
		f.said = true
	}
}

// zenSee hands ev to emit through f, or straight on when there is no f.
func zenSee(f *zenReply, emit func(Event)) func(Event) {
	if f == nil {
		return emit
	}
	return func(ev Event) { f.see(ev, emit) }
}

// zenRound is ask with each round's reply turned back into the client's,
// as zenReply does: a request that searches (searchReply) is asked in rounds.
func zenRound(z *zenTools, ask round) round {
	return func(ctx context.Context, req *Request) (<-chan Event, int, string) {
		in, status, msg := ask(ctx, req)
		if in == nil {
			return in, status, msg
		}
		out := make(chan Event, 16)
		go func() {
			defer close(out)
			f := &zenReply{z: z}
			send := func(ev Event) {
				select {
				case out <- ev:
				case <-ctx.Done():
				}
			}
			for ev := range in {
				f.see(ev, send)
			}
		}()
		return out, status, msg
	}
}
