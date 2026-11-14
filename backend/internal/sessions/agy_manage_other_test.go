//go:build !unix

package sessions

import "os"

func syscallFlock(*os.File) error { return nil }
