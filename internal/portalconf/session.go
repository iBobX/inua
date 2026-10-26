package portalconf

import (
	"cmp"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/portal"
)

// DirEnv is how the frontend is told to read inua's directory.
const DirEnv = "XDG_DESKTOP_PORTAL_DIR"

// Apply gives the running session the directory, or takes it away, when
// that session is inua's (the shell runs), and restarts the frontend,
// which reads its backends only when it starts. It says what it did.
func (d Dirs) Apply(on bool) (string, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return "", err
	}
	conn.BusObject().Call("org.freedesktop.DBus.ReloadConfig", 0) // the service files
	inua := HasOwner(conn, portal.ShellName) || shellRuns()
	conn.Close()
	if !inua {
		return "This is not an inua session; the change takes effect in the next one.", nil
	}
	set := []string{"--user", "unset-environment", DirEnv}
	if on {
		set = []string{"--user", "set-environment", DirEnv + "=" + d.Own}
	}
	for _, args := range [][]string{set, {"--user", "try-restart", "xdg-desktop-portal.service"}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("systemctl %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return "Restarted xdg-desktop-portal; apps started from now on see the change.", nil
}

// shellRuns is whether the shell answers on its control socket: a shell
// from before the portals takes no portal name, yet the session is inua's.
func shellRuns() bool {
	conn, err := net.Dial("unix", ipc.SocketPath(cmp.Or(os.Getenv("INUA_INSTANCE"), "default")))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// RestartXdph restarts xdg-desktop-portal-hyprland, which reads its
// config only when it starts, so the share picker changes.
func RestartXdph() error {
	if out, err := exec.Command("systemctl", "--user", "try-restart", "xdg-desktop-portal-hyprland.service").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SessionDir is the directory the user's systemd gives the frontend, or
// "".
func SessionDir() string {
	out, _ := exec.Command("systemctl", "--user", "show-environment").Output()
	for line := range strings.Lines(string(out)) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), DirEnv+"="); ok {
			return v
		}
	}
	return ""
}

// HasOwner is whether a process holds name on the bus.
func HasOwner(conn *dbus.Conn, name string) bool {
	var has bool
	conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&has)
	return has
}

// activationEnv are the variables naming the session's display and
// compositor.
var activationEnv = []string{"WAYLAND_DISPLAY", "HYPRLAND_INSTANCE_SIGNATURE"}

// ActivationDisplay gives D-Bus and the user's systemd this process's
// display, so what they start (inua-files for a file chooser,
// xdg-desktop-portal-hyprland) opens on it: a nested Hyprland gives them
// its own as it starts, and they keep it after it is gone.
func ActivationDisplay(conn *dbus.Conn) error {
	env := map[string]string{}
	var assign []string
	for _, k := range activationEnv {
		if v := os.Getenv(k); v != "" {
			env[k] = v
			assign = append(assign, k+"="+v)
		}
	}
	if len(env) == 0 {
		return nil
	}
	if err := conn.BusObject().Call("org.freedesktop.DBus.UpdateActivationEnvironment", 0, env).Err; err != nil {
		return err
	}
	return conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").
		Call("org.freedesktop.systemd1.Manager.SetEnvironment", 0, assign).Err
}
