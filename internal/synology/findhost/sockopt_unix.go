//go:build !windows

package findhost

import "syscall"

// setSocketOptions enables address reuse (so the well-known discovery port can
// be bound even across quick re-runs or alongside another listener) and
// broadcast sending, before the socket is bound.
func setSocketOptions(c syscall.RawConn) error {
	var opErr error
	if err := c.Control(func(fd uintptr) {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
			opErr = err
			return
		}
		opErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return opErr
}
