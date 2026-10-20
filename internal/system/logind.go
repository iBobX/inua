package system

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// CanHibernate asks logind whether the machine can hibernate: set up for
// it and with room to resume from ("yes", or "challenge": allowed once the
// user is asked). Over its own connection, at most callLimit long: call it
// off the main loop.
func CanHibernate(dial Dialer) (bool, error) {
	conn, err := dial()
	if err != nil {
		return false, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callLimit)
	defer cancel()
	var answer string
	err = conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		CallWithContext(ctx, "org.freedesktop.login1.Manager.CanHibernate", 0).Store(&answer)
	if err != nil {
		return false, fmt.Errorf("logind: CanHibernate: %w", err)
	}
	return answer == "yes" || answer == "challenge", nil
}

// Buttons are what logind does when the lid is closed, on battery and on
// mains power, and when the power key is pressed (its Handle… settings):
// "suspend", "lock", "poweroff", "hibernate", "ignore" and the like.
type Buttons struct {
	Lid, LidAC, Power string
}

// ButtonActions are the actions Settings → Power offers, in its order.
var ButtonActions = []string{"suspend", "hibernate", "lock", "poweroff", "ignore"}

// ReadButtons reads what logind does with the lid and the power key now,
// over its own connection; an unset mains-power lid is the battery's, as
// logind takes it.
func ReadButtons(dial Dialer) (Buttons, error) {
	conn, err := dial()
	if err != nil {
		return Buttons{}, err
	}
	defer conn.Close()
	obj := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1")
	get := func(name string) (string, error) {
		v, err := obj.GetProperty("org.freedesktop.login1.Manager." + name)
		if err != nil {
			return "", fmt.Errorf("logind: %s: %w", name, err)
		}
		s, _ := v.Value().(string)
		return s, nil
	}
	var b Buttons
	if b.Lid, err = get("HandleLidSwitch"); err != nil {
		return Buttons{}, err
	}
	if b.LidAC, err = get("HandleLidSwitchExternalPower"); err != nil {
		return Buttons{}, err
	}
	if b.Power, err = get("HandlePowerKey"); err != nil {
		return Buttons{}, err
	}
	if b.LidAC == "" {
		b.LidAC = b.Lid
	}
	return b, nil
}

// LogindDropIn is where Settings → Power keeps logind's settings: a
// drop-in of its own, so logind.conf itself is never edited.
const LogindDropIn = "/etc/systemd/logind.conf.d/50-inua.conf"

// DropIn is the drop-in for these buttons; an action Settings does not
// offer is refused, so nothing but the three settings can be written.
func (b Buttons) DropIn() (string, error) {
	for _, a := range []string{b.Lid, b.LidAC, b.Power} {
		if !slices.Contains(ButtonActions, a) {
			return "", fmt.Errorf("logind: %q is not an action Settings sets", a)
		}
	}
	return "# Written by inua: Settings → Power.\n[Login]\n" +
		"HandleLidSwitch=" + b.Lid + "\n" +
		"HandleLidSwitchExternalPower=" + b.LidAC + "\n" +
		"HandlePowerKey=" + b.Power + "\n", nil
}

// ApplyButtons writes the drop-in and has logind read it again, as root
// through pkexec: the session's polkit agent asks for an administrator's
// password once. The file goes in on standard input to a fixed script, so
// nothing of it is ever read as a command; logind is reloaded, not
// restarted, which would end the session.
func ApplyButtons(b Buttons) error {
	conf, err := b.DropIn()
	if err != nil {
		return err
	}
	script := "set -e; mkdir -p " + filepath.Dir(LogindDropIn) + "; cat > " + LogindDropIn +
		"; chmod 644 " + LogindDropIn + "; systemctl reload systemd-logind"
	cmd := exec.Command("pkexec", "/bin/sh", "-c", script)
	cmd.Stdin = strings.NewReader(conf)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && (exit.ExitCode() == 126 || exit.ExitCode() == 127) {
			return ErrNotAuthorized // the password asked was not given
		}
		return fmt.Errorf("logind: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ErrNotAuthorized is an administrator's password asked and not given.
var ErrNotAuthorized = errors.New("not authorized")
