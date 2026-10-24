package pty

import (
	"os/signal"
	"unsafe"

	"golang.org/x/sys/unix"
)

// defaultSignals sets those this process inherited as ignored (started in
// the background by a non-interactive shell, say) back to the default.
// Children inherit what is ignored, and Go leaves an ignored signal
// ignored; without this, Ctrl+C would not stop a program in the terminal.
func defaultSignals() {
	defaultOnce.Do(func() {
		var act struct { // the kernel's struct sigaction on Linux
			handler  uintptr // SIG_DFL
			flags    uint64
			restorer uintptr
			mask     uint64
		}
		for _, sig := range jobSignals {
			if signal.Ignored(sig) {
				unix.Syscall6(unix.SYS_RT_SIGACTION, uintptr(sig), uintptr(unsafe.Pointer(&act)), 0, unsafe.Sizeof(act.mask), 0, 0)
			}
		}
	})
}
