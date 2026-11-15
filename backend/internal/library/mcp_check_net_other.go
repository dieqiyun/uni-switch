//go:build !windows

package library

import "syscall"

const connectionRefused = syscall.ECONNREFUSED
