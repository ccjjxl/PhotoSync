//go:build !windows

package main

import (
	"syscall"
)

// setBroadcast enables SO_BROADCAST so we may send to 255.255.255.255.
func setBroadcast(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
