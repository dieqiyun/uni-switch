package library

import (
	"net/http"
	"sync"

	"github.com/dieqiyun/uni-switch/backend/internal/ghtoken"
)

// ---- asking GitHub's API with the user's token ----------------------------
//
// The token, where it may go and what a rate limit says are ghtoken's,
// shared with the Plugins page's checks of git plugins (#1942).

// GitHubTokenEnv names the environment variables a token is taken from when
// Settings has none, in the order they are looked at.
var GitHubTokenEnv = ghtoken.Env

// GitHubToken is the token requests to GitHub's API carry, and where it
// came from: "settings", the environment variable's name, or "" for none.
func GitHubToken() (token, from string) { return ghtoken.Token() }

// withGitHubToken has req carry the token, if there is one and req goes to
// GitHub's API (githubAPI's host); it says whether it did.
func withGitHubToken(req *http.Request) bool { return ghtoken.Authorize(req, githubAPI) }

// doGitHub sends req with send, carrying the token as withGitHubToken
// does, and says whether the answer is to a request that carried it: an
// environment token GitHub refuses is dropped and req sent again without
// it (ghtoken.Do).
func doGitHub(req *http.Request, send func(*http.Request) (*http.Response, error)) (*http.Response, bool, error) {
	return ghtoken.Do(req, githubAPI, send)
}

// errLimited is GitHub turning requests away for a while.
type errLimited = ghtoken.Limited

var limitedBy = ghtoken.LimitedBy

// etags are GitHub's answers kept by URL, for the next request for one to
// ask if it changed: a 304 doesn't count against the rate limit.
var etags = struct {
	sync.Mutex
	m map[string]etagged
}{m: map[string]etagged{}}

type etagged struct {
	etag string
	body []byte
}
