package webterm

import (
	"net"
	"testing"

	"github.com/lyricat/inua/internal/term/settings"
)

// On this computer only, the Web Terminal listens on the loopback
// address at its port.
func TestAddr(t *testing.T) {
	addr, err := Addr(settings.Web{Port: 7681})
	if err != nil || addr != "127.0.0.1:7681" {
		t.Errorf("Addr = %q, %v", addr, err)
	}
	if ip, err := TailscaleIP(); err == nil && !tailscaleNet.Contains(net.ParseIP(ip)) {
		t.Errorf("Tailscale address %s outside 100.64.0.0/10", ip)
	}
}
