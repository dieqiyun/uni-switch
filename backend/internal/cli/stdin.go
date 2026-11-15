package cli

import (
	"bufio"
	"os"
	"sync"
)

// stdin reads confirmations from the terminal handle active at the time of use.
var (
	stdinMu   sync.Mutex
	stdinFile *os.File
	stdinBuf  *bufio.Reader
)

func stdin() *bufio.Reader {
	stdinMu.Lock()
	defer stdinMu.Unlock()
	if stdinBuf == nil || stdinFile != os.Stdin {
		stdinFile, stdinBuf = os.Stdin, bufio.NewReader(os.Stdin)
	}
	return stdinBuf
}
