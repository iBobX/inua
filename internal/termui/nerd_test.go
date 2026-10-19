package termui

import (
	"testing"
	"time"
)

func TestUptime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		12 * time.Minute: "12m",
		5*time.Hour + 12*time.Minute + 40*time.Second: "5h 12m",
		3*24*time.Hour + 2*time.Hour + 5*time.Minute:  "3d 2h",
	} {
		if got := uptime(d); got != want {
			t.Errorf("uptime(%v) = %q, want %q", d, got, want)
		}
	}
}
