package web

import (
	"time"

	"golang.org/x/sys/unix"
)

// uptime is how long this computer has been up, from when it booted. On
// macOS the CPU and the memory are not read yet (there is no /proc): the
// page says they are unknown.
func uptime() time.Duration {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return 0
	}
	return time.Since(time.Unix(tv.Sec, int64(tv.Usec)*1000))
}
