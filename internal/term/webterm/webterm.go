// Package webterm is the Web Terminal: inua-termd serve (internal/term/web)
// run in a systemd user unit of its own, turned on in inua-term's
// settings and started again at login while it is on, and its link. It
// holds the page's token, kept in the keyring. No GTK, and none of the
// page's files: the shell starts it too.
package webterm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/settings"
)

// Unit is the systemd user unit inua-termd serve runs in.
const Unit = "inua-term-web.service"

// Running says whether the unit runs.
func Running(ctx context.Context) bool {
	return exec.CommandContext(ctx, "systemctl", "--user", "is-active", "--quiet", Unit).Run() == nil
}

// Addr is where the Web Terminal listens: this computer, or its Tailscale
// address.
func Addr(w settings.Web) (string, error) {
	host := "127.0.0.1"
	if w.Tailscale {
		ip, err := TailscaleIP()
		if err != nil {
			return "", err
		}
		host = ip
	}
	return net.JoinHostPort(host, strconv.Itoa(w.Port)), nil
}

// Link is the page's address with its token: what a phone opens.
func Link(ctx context.Context, w settings.Web) (string, error) {
	addr, err := Addr(w)
	if err != nil {
		return "", err
	}
	token, err := Token(ctx, false)
	if err != nil {
		return "", err
	}
	return "http://" + addr + "/?t=" + url.QueryEscape(token), nil
}

// Start runs inua-termd serve in its unit, as w says, and waits for it to
// listen. One running already is stopped first: its address may have
// changed.
func Start(ctx context.Context, w settings.Web) error {
	addr, err := Addr(w)
	if err != nil {
		return err
	}
	// The token first: the unit has no one to ask for the keyring.
	if _, err := Token(ctx, false); err != nil {
		return err
	}
	self, err := host.Program("inua-termd")
	if err != nil {
		return err
	}
	Stop(ctx)
	exec.CommandContext(ctx, "systemctl", "--user", "reset-failed", Unit).Run()
	args := []string{"--user", "--unit=" + Unit, "--description=inua-term Web Terminal", "--collect", "--quiet",
		"--property=Restart=on-failure", "--property=RestartSec=3"}
	// The unit starts in the user manager's environment; inua-termd serve
	// needs the session's bus (the keyring) and runtime folder (the
	// host's socket), and the language.
	for _, k := range []string{"XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "PATH", "LANG", "LANGUAGE", "LC_ALL", "LC_MESSAGES"} {
		if v := os.Getenv(k); v != "" {
			args = append(args, "--setenv="+k+"="+v)
		}
	}
	args = append(args, "--", self, "serve", "--addr", addr)
	if out, err := exec.CommandContext(ctx, "systemd-run", args...).CombinedOutput(); err != nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return errors.New(s)
		}
		return err
	}
	for {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			c.Close()
			return nil
		}
		if !Running(ctx) {
			return fmt.Errorf("inua-termd serve stopped: journalctl --user -u %s says why", Unit)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Stop ends the unit.
func Stop(ctx context.Context) {
	exec.CommandContext(ctx, "systemctl", "--user", "stop", Unit).Run()
}

// AtLogin starts the Web Terminal when it was left on: the shell calls it
// once the session is there.
func AtLogin(ctx context.Context) error {
	set, _, err := settings.Load(settings.Path())
	if err != nil || !set.Web.On || Running(ctx) {
		return err
	}
	return Start(ctx, set.Web)
}

// tailscaleNet is Tailscale's range of addresses (100.64.0.0/10).
var tailscaleNet = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// ErrNoTailscale is a computer with no Tailscale address.
var ErrNoTailscale = errors.New("no Tailscale address: is Tailscale up?")

// TailscaleIP is this computer's Tailscale IPv4 address.
func TailscaleIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && tailscaleNet.Contains(n.IP) {
				return n.IP.String(), nil
			}
		}
	}
	return "", ErrNoTailscale
}
