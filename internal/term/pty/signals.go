package pty

import (
	"sync"
	"syscall"
)

// jobSignals are the signals a terminal's programs must get as usual:
// Ctrl+C, Ctrl+\, Ctrl+Z and the job-control stops.
var jobSignals = []syscall.Signal{syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU}

var defaultOnce sync.Once
