package edit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// A value holding a byte TOML has no escape for must still leave a file a
// TOML parser can read. Go's strconv.Quote spells a control byte as \x01,
// and TOML defines no \x escape, so a preserving edit wrote a config.toml
// that no longer parsed. Both paths spell a string the same way now, and
// the file is parsed here rather than compared to a literal, so an escape
// TOML rejects is caught rather than hard-coded.
func TestTOMLPreservingKeepsAValueTOMLCanParse(t *testing.T) {
	// Control bytes Go escapes with \x and TOML defines no escape for.
	values := []string{
		"a" + string(rune(1)) + "b",    // SOH
		"a" + string(rune(0x7f)) + "b", // DEL
		"a" + string(rune(0x0b)) + "b", // VT
		"tab\there",
		"nl\nhere",
		"quote\"here",
		`back\slash`,
		"cr\rhere",
		"bell" + string(rune(7)),
	}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			before := "default_model = \"native/a\" # keep\n"
			if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := SetTOMLTopPreserving(path, KV{Path: "default_model", Value: value}); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "# keep") {
				t.Errorf("the trailing comment was not kept: %s", raw)
			}
			var got struct {
				DefaultModel string `toml:"default_model"`
			}
			if err := toml.Unmarshal(raw, &got); err != nil {
				t.Fatalf("the file no longer parses as TOML: %v\n%s", err, raw)
			}
			if got.DefaultModel != value {
				t.Errorf("read back %q, want %q", got.DefaultModel, value)
			}
		})
	}
}

// The same value through both paths, so the preserving one cannot drift
// from SetTOMLTop again: a caller switching between them writes the same
// bytes.
func TestTOMLPreservingAndPlainAgreeOnEncoding(t *testing.T) {
	for _, value := range []string{
		"a" + string(rune(1)) + "b",
		"a" + string(rune(0x7f)) + "b",
		"plain",
		"",
	} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			plain, keeping := filepath.Join(dir, "plain.toml"), filepath.Join(dir, "keeping.toml")
			for _, p := range []string{plain, keeping} {
				if err := os.WriteFile(p, []byte("default_model = \"native/a\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := SetTOMLTop(plain, KV{Path: "default_model", Value: value}); err != nil {
				t.Fatal(err)
			}
			if err := SetTOMLTopPreserving(keeping, KV{Path: "default_model", Value: value}); err != nil {
				t.Fatal(err)
			}
			a, err := os.ReadFile(plain)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(keeping)
			if err != nil {
				t.Fatal(err)
			}
			if string(a) != string(b) {
				t.Errorf("the two paths wrote different bytes:\nSetTOMLTop:           %q\nSetTOMLTopPreserving: %q", a, b)
			}
		})
	}
}
