package provider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func grokSignedIn(t *testing.T, home, email string) {
	t.Helper()
	writeFile(t, filepath.Join(home, "auth.json"), map[string]any{
		"https://auth.x.ai": map[string]any{"key": "k-" + email, "email": email, "expires_at": time.Now().Add(time.Hour)},
	})
}

// newGrokHome and addGrokLogin keep a further Grok account the way the
// retired built-in kept one, in a home of its own, so the move onto the
// plugin is tested against what users who signed in there still have.
func newGrokHome() (string, error) {
	home := filepath.Join(filepath.Dir(Path()), "grok", strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(randomToken(9))))
	return home, os.MkdirAll(home, 0o700)
}

func addGrokLogin(home string) (string, error) {
	c, ok := readGrokCredential(home)
	if !ok || c.Email == "" {
		return "", errors.New("grok login finished without an account")
	}
	own, _ := readGrokCredential(GrokHome())
	return c.Email, addSideLogin(savedLogin{Agent: "grok", User: c.Email, Home: home}, own.Email, func(savedLogin) {})
}
