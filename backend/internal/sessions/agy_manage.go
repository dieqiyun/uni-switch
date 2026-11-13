package sessions

import "path/filepath"

// agyDataDir is the agy data folder a conversation's files are in: a
// transcript is brain/<id>/.system_generated/logs/transcript_full.jsonl in
// it, a conversation listed from its row alone conversations/<id>.db.
func agyDataDir(fs []file) string {
	for _, f := range fs {
		if !f.main {
			continue
		}
		up := 5
		if agyRowOnly(f.path) {
			up = 2
		}
		d := f.path
		for range up {
			d = filepath.Dir(d)
		}
		return d
	}
	return ""
}
