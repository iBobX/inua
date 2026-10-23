package web

import (
	"time"

	"github.com/lyricat/inua/internal/apps/sysmon"
)

// uptime is how long this computer has been up.
func uptime() time.Duration { return sysmon.Uptime() }
