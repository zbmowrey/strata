//go:build unix

package render

import "golang.org/x/sys/unix"

func termWidth(fd uintptr) int {
	ws, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ) //nolint:gosec // G115: fds fit in int
	if err != nil {
		return 0
	}
	return int(ws.Col)
}
