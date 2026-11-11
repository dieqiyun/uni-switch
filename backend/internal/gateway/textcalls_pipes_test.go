package gateway

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Which of these were seen and which were made up (OKURBITO on #2623):
//   - observedPipes is the one reply a model wrote on its own, in real
//     use: deepseek-flash, offered web_search, wrote the call as text with
//     two full-width pipes and a space before each tag's name;
//   - issuePipes is the issue's reproduction: the model was asked to echo
//     those bytes back (workbuddy/deepseek-v4.1-flash from a Responses
//     client), and the reply it got came back as text;
//   - every other spelling below (three pipes, ASCII, widths that
//     disagree, spaces after <, no pipes after DSML) is constructed: no
//     one has seen a model write it. They are here because the mark is
//     read as the parse reads it, not from a list of spellings.
const observedPipes = "<｜｜DSML｜｜ calls>\n<｜｜DSML｜｜ invoke name=\"web_search\">\n<｜｜DSML｜｜ parameter name=\"query\" string=\"true\">current Python version latest stable release</｜｜DSML｜｜ parameter>\n</｜｜DSML｜｜ invoke>\n</｜｜DSML｜｜ calls>"

const issuePipes = `<｜｜DSML｜｜invoke name="get_weather"><｜｜DSML｜｜parameter name="city" string="true">Beijing</｜｜DSML｜｜parameter></｜｜DSML｜｜invoke>`

const weatherTool = `"tools":[{"type":"function","name":"get_weather","description":"Get the weather of a city","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]`

// #2623: DSML written with two pipes went to a Responses client as text,
// the issue's prompted reply,
// streamed or whole, while one pipe of either width was made the call.
func TestTextWrittenDSMLDoublePipe(t *testing.T) {
	// the reply seen in real use, its bytes as they came: a function
	// web_search offered, it is the call, the calls wrapper gone with it
	for _, stream := range []string{"true", "false"} {
		body := `{"model":"a/m","stream":` + stream + `,"messages":[{"role":"user","content":"You MUST call the web_search tool first"}],"tools":[{"type":"function","function":{"name":"web_search","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}}]}`
		var text *string
		var calls map[string]string
		var finish, raw string
		if stream == "true" {
			var s string
			s, calls, finish, raw = relayedChat(t, body, chunkOf(observedPipes), stopChunk)
			text = &s
			if s == "" {
				text = nil
			}
		} else {
			var cs []wholeCall
			text, cs, finish, raw = wholeChat(t, body, "application/json", wholeChatUp(observedPipes))
			calls = map[string]string{}
			for _, c := range cs {
				calls[c.Function.Name] = c.Function.Arguments
			}
		}
		if text != nil || len(calls) != 1 || calls["web_search"] != `{"query":"current Python version latest stable release"}` || finish != "tool_calls" || strings.Contains(raw, "DSML") {
			t.Fatalf("observed, stream=%s: text %q calls %v finish %q\n%s", stream, strOr(text), calls, finish, raw)
		}
	}

	for _, stream := range []string{"true", "false"} {
		items, raw := responsesItems(t, `{"model":"a/m","stream":`+stream+`,"input":"weather","max_output_tokens":1200,`+weatherTool+`}`,
			chunkOf(issuePipes), stopChunk)
		var call map[string]any
		for _, it := range items {
			switch it["type"] {
			case "function_call":
				call = it
			case "message":
				t.Errorf("stream=%s: a message item beside the call: %v", stream, it)
			}
		}
		if call == nil || call["name"] != "get_weather" || call["arguments"] != `{"city":"Beijing"}` || strings.Contains(raw, "DSML") {
			t.Fatalf("stream=%s: %v\n%s", stream, call, raw)
		}
	}
	text, calls, finish, raw := wholeChat(t, `{"model":"a/m","stream":false,"messages":[{"role":"user","content":"weather"}],`+chatWeatherTool+`}`,
		"application/json", wholeChatUp(issuePipes))
	if text != nil || len(calls) != 1 || calls[0].Function.Name != "get_weather" || calls[0].Function.Arguments != `{"city":"Beijing"}` ||
		finish != "tool_calls" || strings.Contains(raw, "DSML") {
		t.Fatalf("whole: text %q calls %+v finish %q\n%s", strOr(text), calls, finish, raw)
	}
}

const chatWeatherTool = `"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]`

// Every spelling of DSML's pipes is read as the one-pipe spelling is: a
// run of them, ASCII or full-width or both, with spaces between, as
// DeepSeek wrote <｜｜DSML｜｜ invoke in a live session. Each reply goes
// up one character to a chunk, so a mark is always cut across chunks; a
// reply that names no offered tool, is cut short, is empty or only looks
// like a mark ends as the one-pipe reply ends.
func TestTextWrittenDSMLAnyPipes(t *testing.T) {
	body := `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"weather"}],` + chatWeatherTool + `}`
	// written with ｜ standing for the pipes; text, call and finish are
	// what the one-pipe reply gives
	cases := []struct{ name, reply, text, args, finish string }{
		{"the issue's", strings.ReplaceAll(issuePipes, "｜｜", "｜"), "", `{"city":"Beijing"}`, "tool_calls"},
		{"wrapped, text before", "Checking.\n\n<｜DSML｜function_calls>\n<｜DSML｜invoke name=\"get_weather\">\n<｜DSML｜parameter name=\"city\" string=\"true\">Beijing</｜DSML｜parameter>\n</｜DSML｜invoke>\n</｜DSML｜function_calls>", "Checking.\n\n", `{"city":"Beijing"}`, "tool_calls"},
		{"a space before invoke", "<｜DSML｜ invoke name=\"get_weather\">\n<｜DSML｜ parameter name=\"city\" string=\"true\">Beijing</｜DSML｜ parameter>\n</｜DSML｜ invoke>", "", `{"city":"Beijing"}`, "tool_calls"},
		{"no parameters", "<｜DSML｜invoke name=\"get_weather\"></｜DSML｜invoke>", "", `{}`, "tool_calls"},
		{"cut short", "<｜DSML｜invoke name=\"get_weather\"><｜DSML｜parameter name=\"city\" string=\"true\">Beij", "", `{}`, "tool_calls"},
		{"tags only, the last cut short", "<｜DSML｜function_calls>\n</｜DSML｜function_calls>\n</｜DS", "", "", "stop"},
		{"a tool not offered", "<｜DSML｜ invoke name=\"web_search\">\n<｜DSML｜ parameter name=\"query\">x</｜DSML｜ parameter>\n</｜DSML｜ invoke>", "<｜DSML｜ invoke name=\"web_search\">\n<｜DSML｜ parameter name=\"query\">x</｜DSML｜ parameter>\n</｜DSML｜ invoke>", "", "stop"},
		{"a mark's start at the end", "a <｜DS", "a <｜DS", "", "stop"},
		{"pipes and no DSML", "a <｜ b ｜> c", "a <｜ b ｜> c", "", "stop"},
	}
	for _, pipes := range []string{"｜", "|", "｜｜", "||", "｜|", "|｜", "｜｜｜", "|||", "｜ ｜"} {
		for _, c := range cases {
			spell := func(s string) string { return strings.ReplaceAll(s, "｜", pipes) }
			var up []string
			for _, r := range spell(c.reply) {
				up = append(up, chunkOf(string(r)))
			}
			text, calls, finish, raw := relayedChat(t, body, append(up, stopChunk)...)
			var want map[string]string
			if c.args != "" {
				want = map[string]string{"get_weather": c.args}
			}
			if text != spell(c.text) || finish != c.finish || len(calls) != len(want) || calls["get_weather"] != want["get_weather"] {
				t.Errorf("%q %s: text %q calls %v finish %q\n%s", pipes, c.name, text, calls, finish, raw)
			}
		}
	}
}

// The issue's reply, cut anywhere and its parts sent as chunks of their
// own, is the same call at every cut, a full-width pipe's bytes too.
func TestTextWrittenDSMLDoublePipeEveryCut(t *testing.T) {
	body := `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"weather"}],` + chatWeatherTool + `}`
	s := "Sure.\n" + issuePipes
	for i := 1; i < len(s); i++ {
		if !utf8.ValidString(s[:i]) {
			continue // a chunk's JSON carries whole characters only
		}
		text, calls, finish, raw := relayedChat(t, body, chunkOf(s[:i]), chunkOf(s[i:]), stopChunk)
		if text != "Sure.\n" || calls["get_weather"] != `{"city":"Beijing"}` || finish != "tool_calls" {
			t.Fatalf("cut at %d: text %q calls %v finish %q\n%s", i, text, calls, finish, raw)
		}
	}
	// and the scanner, which may see a character's first bytes alone
	for i := 1; i < len("<｜｜DSML｜"); i++ {
		at, tail := textCallAt("x" + issuePipes[:i])
		if at >= 0 || tail != i {
			t.Fatalf("%q: at %d tail %d, want the %d bytes held", issuePipes[:i], at, tail, i)
		}
	}
	if at, _ := textCallAt("x" + issuePipes); at != 1 {
		t.Fatalf("the whole mark: at %d", at)
	}
}

// The mark is found as the parse reads it, not from a list of spellings
// (OKURBITO on #2623): widths that differ within a tag and between its
// open and close, spaces after < and around /, no pipes after DSML. A
// reply ending in a < that starts no pipes keeps it.
func TestTextWrittenDSMLMarkAsParsed(t *testing.T) {
	body := `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"weather"}],` + chatWeatherTool + `}`
	for _, c := range []struct{ name, reply, text, args string }{
		{"widths that disagree", `<｜|DSML|｜invoke name="get_weather"><||DSML｜｜parameter name="city" string="true">Beijing</｜｜DSML||parameter></|DSML｜invoke>`, "", `{"city":"Beijing"}`},
		{"spaces after < and around /", `< ｜DSML｜invoke name="get_weather">< ｜DSML｜parameter name="city" string="true">Beijing< / ｜DSML｜parameter></｜DSML｜invoke>`, "", `{"city":"Beijing"}`},
		{"no pipes after DSML", `<｜DSMLinvoke name="get_weather"><｜DSMLparameter name="city" string="true">Beijing</｜DSMLparameter></｜DSMLinvoke>`, "", `{"city":"Beijing"}`},
		{"a value ending in <", `<｜DSML｜invoke name="get_weather"><｜DSML｜parameter name="city" string="true">a < </｜DSML｜parameter></｜DSML｜invoke>`, "", `{"city":"a \u003c "}`},
		{"a cut-short value ending in <", "<tool_call>{\"name\":\"get_weather\">\n<parameter name=\"city\">a < ", "", `{"city":"a \u003c "}`},
		{"text with < and no mark", "x < y and a <", "x < y and a <", ""},
	} {
		var up []string
		for _, r := range c.reply {
			up = append(up, chunkOf(string(r)))
		}
		text, calls, finish, raw := relayedChat(t, body, append(up, stopChunk)...)
		want, wantFinish := 1, "tool_calls"
		if c.args == "" {
			want, wantFinish = 0, "stop"
		}
		if text != c.text || finish != wantFinish || len(calls) != want || calls["get_weather"] != c.args {
			t.Errorf("%s: text %q calls %v finish %q\n%s", c.name, text, calls, finish, raw)
		}
	}
}
