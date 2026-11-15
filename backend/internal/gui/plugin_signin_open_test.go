package gui

import (
	"sync"
)

// openCounter is a desktop that counts the pages it was asked to open.
type openCounter struct {
	webHost
	mu     sync.Mutex
	opened []string
}

func (w *openCounter) OpenURL(u string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.opened = append(w.opened, u)
	return nil
}
