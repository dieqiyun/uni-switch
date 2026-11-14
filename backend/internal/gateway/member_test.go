package gateway

import (
	"bufio"

	"encoding/json"
	"fmt"

	"net/http/httptest"

	"regexp"
	"strings"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/tidwall/gjson"
)

// modelsNamed are the models a reply's body, or each of its events, names.
func modelsNamed(body string, stream bool) []string {
	var datas []string
	if !stream {
		datas = []string{body}
	} else {
		sc := bufio.NewScanner(strings.NewReader(body))
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			if d, ok := strings.CutPrefix(sc.Text(), "data:"); ok {
				datas = append(datas, strings.TrimSpace(d))
			}
		}
	}
	var out []string
	for _, d := range datas {
		for _, path := range memberPaths {
			if v := gjson.Get(d, path); v.Type == gjson.String {
				out = append(out, v.Str)
			}
		}
	}
	return out
}

// An event's model is put in place, the rest of the line as it was; a
// line that names none, or isn't JSON, is untouched.
func TestMemberLine(t *testing.T) {
	for in, want := range map[string]string{
		"data: {\"model\":\"glm\",\"x\":1}\n":                   "data: {\"model\":\"b/glm\",\"x\":1}\n",
		"data:{\"message\":{\"model\":\"glm\"}}\r\n":            "data:{\"message\":{\"model\":\"b/glm\"}}\r\n",
		"data: {\"response\":{\"id\":\"r\",\"model\":\"glm\"}}": "data: {\"response\":{\"id\":\"r\",\"model\":\"b/glm\"}}",
		"data: {\"modelVersion\":\"glm\"}\n":                    "data: {\"modelVersion\":\"b/glm\"}\n",
		"data: [DONE]\n":                                        "data: [DONE]\n",
		"event: message_start\n":                                "event: message_start\n",
		"data: {\"delta\":{\"text\":\"\\\"model\\\":\"}}\n":     "data: {\"delta\":{\"text\":\"\\\"model\\\":\"}}\n",
	} {
		if got := string(memberLine([]byte(in), "b/glm")); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// A reply to a request that came in under one of uni-switch's own aliases for
// Claude Desktop or Claude Science keeps the vendor's own name (#4667):
// the alias — uni-switch-<n>.anthropic.claude-opus-5-5 among them — is
// uni-switch's deployment name, not one the vendor knows, and naming the reply
// with it put that name in Claude Code's usage records, where ccusage
// counts it as a model of its own instead of the one the subscription
// served. Before the asked-name rule these ids were served the vendor's
// name, as every other agent still is.
func TestReplyToUniSwitchAliasKeepsVendorName(t *testing.T) {
	const served = "opus-5-5" // what the subscription's reply says
	aliases := []string{
		"uni-switch-" + aliasNumber("sub/claude-opus-5-5") + desktopClaudeInfix + "claude-opus-5-5",
		"uni-switch-" + aliasNumber("sub/claude-opus-5-5") + desktopClaudeInfix + "claude-opus-5-5[1m]",
		aliasFor("sub/claude-opus-5-5"),
		desktopEffortAlias + aliasNumber("sub/claude-opus-5-5"),
		scienceAlias + aliasNumber("sub/claude-opus-5-5"),
	}
	for _, stream := range []bool{false, true} {
		for _, asked := range aliases {
			name := asked
			if stream {
				name += "/stream"
			}
			t.Run(name, func(t *testing.T) {
				fresh(t)
				up := httptest.NewServer(servedBy(provider.Anthropic, served))
				t.Cleanup(up.Close)
				if err := provider.Save(provider.Provider{ID: "sub", Name: "Sub", Key: "k", Anthropic: up.URL, Models: []string{"claude-opus-5-5"}}); err != nil {
					t.Fatal(err)
				}
				m, _ := json.Marshal(asked)
				body := `{"model":` + string(m) + `,"max_tokens":100,"messages":[{"role":"user","content":"hi"}]`
				if stream {
					body += `,"stream":true`
				}
				rec := httptest.NewRecorder()
				s := New()
				s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body+"}")))
				if rec.Code != 200 || !strings.Contains(rec.Body.String(), "hi") {
					t.Fatalf("%d %s", rec.Code, rec.Body)
				}
				names := modelsNamed(rec.Body.String(), stream)
				if len(names) == 0 {
					t.Fatalf("no model named in %s", rec.Body)
				}
				for _, n := range names {
					if n != served {
						t.Fatalf("the reply names %q, want the vendor's %q:\n%s", n, served, rec.Body)
					}
				}
				if r := lastRoute(s); r.Served != served {
					t.Errorf("the route says %q served it, want the vendor's %q", r.Served, served)
				}
				if calls := s.Recent(); len(calls) == 0 || calls[0].Usage.Served != served {
					t.Errorf("the log says %+v served it, want the vendor's %q", calls, served)
				}
			})
		}
	}
}

// A Gemini agent asks in the URL, and the reply's modelVersion names that
// model when the request went out under another name.
func TestGeminiReplyNamesAskedModel(t *testing.T) {
	for _, c := range []struct{ reply, want string }{
		{"", "deepseek-v4.1-flash"},
		{"member", "d/deepseek-v4.1-flash"},
		{"vendor", "deepseek-v4.1-flash-1017"},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprint(c.reply, " stream ", stream), func(t *testing.T) {
				fresh(t)
				up := httptest.NewServer(servedBy(provider.Chat, "deepseek-v4.1-flash-1017"))
				t.Cleanup(up.Close)
				if err := provider.Save(provider.Provider{ID: "d", Name: "D", Key: "k", Chat: up.URL + "/v1", Models: []string{"deepseek-v4.1-flash"}}); err != nil {
					t.Fatal(err)
				}
				if err := settings.Save(settings.Settings{ReplyModel: c.reply, ModelIDs: map[string]string{"d/deepseek-v4.1-flash": "deepseek-v4.1-flash-1017"}}); err != nil {
					t.Fatal(err)
				}
				path := "/v1beta/models/deepseek-v4.1-flash:generateContent"
				if stream {
					path = "/v1beta/models/deepseek-v4.1-flash:streamGenerateContent?alt=sse"
				}
				rec := httptest.NewRecorder()
				New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)))
				if rec.Code != 200 || !strings.Contains(rec.Body.String(), "hi") {
					t.Fatalf("%d %s", rec.Code, rec.Body)
				}
				names := modelsNamed(rec.Body.String(), stream)
				if len(names) == 0 {
					t.Fatalf("no model named in %s", rec.Body)
				}
				for _, n := range names {
					if n != c.want {
						t.Fatalf("the reply names %q, want %q:\n%s", n, c.want, rec.Body)
					}
				}
			})
		}
	}
}

// A run of a stream's lines is named line by line, and a run that names
// no model, or a name of "", is the same slice back.
func TestMemberLines(t *testing.T) {
	in := []byte("event: response.created\ndata: {\"response\":{\"model\":\"v\"}}\n\nevent: response.output_text.delta\ndata: {\"delta\":\"hi\"}\n\ndata: {\"model\":\"v\"}\n")
	want := "event: response.created\ndata: {\"response\":{\"model\":\"asked\"}}\n\nevent: response.output_text.delta\ndata: {\"delta\":\"hi\"}\n\ndata: {\"model\":\"v\"}\n"
	if got := string(memberLines(in, "asked", memberPathsOf(provider.Responses)[1:])); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	plain := []byte("data: {\"delta\":\"hi\"}\n\n")
	if got := memberLines(plain, "asked", memberPaths); &got[0] != &plain[0] {
		t.Error("a run with no model was copied")
	}
	if got := memberLines(in, "", memberPaths); &got[0] != &in[0] {
		t.Error("no name copied the run")
	}
}

// namedAs is a reply as an agent that asked for asked gets it: every model
// it names is that one (memberWriter), the rest as the vendor wrote it.
func namedAs(reply, asked string) string {
	return replyModels.ReplaceAllString(reply, `"$1"$2"`+asked+`"`)
}

var replyModels = regexp.MustCompile(`"(model|modelVersion)"(\s*:\s*)"[^"]*"`)
