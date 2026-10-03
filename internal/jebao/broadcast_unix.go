//go:build !windows

package jebao

import (
	"net"
	"syscall"
)

func enableBroadcast(c *net.UDPConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var e error
	err = raw.Control(func(fd uintptr) { e = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1) })
	if err != nil {
		return err
	}
	return e
}
