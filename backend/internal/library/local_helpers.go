package library

import "os"

func orNone(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
