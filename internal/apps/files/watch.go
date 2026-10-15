package files

import (
	"context"
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// settle is how long a watcher waits for a burst of changes to end before
// it reports them as one: an extract or a copy of many files is one
// reload, not thousands.
const settle = 80 * time.Millisecond

// maxWait is the longest a watcher holds changes back while they keep
// coming, so a folder written to without pause still shows its changes.
const maxWait = 600 * time.Millisecond

// Watch calls changed (on its own goroutine) after a folder's entries
// change, until ctx ends: something is made, deleted, renamed, written
// or has its attributes changed. When the folder itself is deleted or
// moved, gone is called once and the watch ends. An error is returned
// when the folder cannot be watched (a network folder, or too many
// watches): the caller reloads by hand.
func Watch(ctx context.Context, dir string, changed func(), gone func()) error {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return err
	}
	const mask = unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO |
		unix.IN_CLOSE_WRITE | unix.IN_ATTRIB | unix.IN_MODIFY |
		unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ONLYDIR
	if _, err := unix.InotifyAddWatch(fd, dir, mask); err != nil {
		unix.Close(fd)
		return err
	}
	// A pipe wakes the poll when ctx ends.
	var wake [2]int
	if err := unix.Pipe2(wake[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		unix.Close(fd)
		return err
	}
	go func() {
		<-ctx.Done()
		unix.Write(wake[1], []byte{0})
	}()
	go func() {
		defer unix.Close(fd)
		defer unix.Close(wake[0])
		defer unix.Close(wake[1])
		buf := make([]byte, 64*1024)
		var pending, first time.Time
		for {
			timeout := -1
			if !pending.IsZero() {
				due := min(pending.Add(settle).Sub(time.Now()), first.Add(maxWait).Sub(time.Now()))
				timeout = max(0, int(due/time.Millisecond))
			}
			fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}, {Fd: int32(wake[0]), Events: unix.POLLIN}}
			n, err := unix.Poll(fds, timeout)
			if ctx.Err() != nil {
				return
			}
			if err != nil && !errors.Is(err, unix.EINTR) {
				return
			}
			if n == 0 && !pending.IsZero() {
				pending, first = time.Time{}, time.Time{}
				changed()
				continue
			}
			if fds[0].Revents&unix.POLLIN == 0 {
				continue
			}
			m, err := unix.Read(fd, buf)
			if err != nil || m <= 0 {
				continue
			}
			selfGone := false
			for off := 0; off+unix.SizeofInotifyEvent <= m; {
				ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
				if ev.Mask&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF|unix.IN_IGNORED) != 0 {
					selfGone = true
				}
				off += unix.SizeofInotifyEvent + int(ev.Len)
			}
			if selfGone {
				if gone != nil {
					gone()
				}
				return
			}
			now := time.Now()
			if pending.IsZero() {
				first = now
			}
			pending = now
		}
	}()
	return nil
}
