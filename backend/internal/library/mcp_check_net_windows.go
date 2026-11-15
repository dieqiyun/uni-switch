package library

import "golang.org/x/sys/windows"

// TCP failures carry Winsock errors. syscall.ECONNREFUSED is a different,
// compatibility errno on Windows and does not match a refused connection.
const connectionRefused = windows.WSAECONNREFUSED
