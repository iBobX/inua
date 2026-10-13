// Package proc names running processes, from /proc, to tell the user who
// asked for something: a secret, a passphrase.
package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// root is where the processes are read from; tests point it elsewhere.
var root = "/proc"

// interpreters run a script named by their first argument: the script is
// the program to name ("pass", not "bash").
var interpreters = map[string]bool{
	"sh": true, "bash": true, "dash": true, "zsh": true, "fish": true,
	"python": true, "python3": true, "perl": true, "ruby": true, "node": true,
}

// session are the processes a chain stops before: what every program
// runs under, which says nothing about who asked.
var session = map[string]bool{
	"init": true, "systemd": true, "Hyprland": true, "inua-shell": true,
	"login": true, "sshd": true, "greetd": true, "dbus-daemon": true,
}

// Name is a process's program: its command's base name, the script for an
// interpreter, else the kernel's name for it; "" for no such process.
func Name(pid int) string {
	if pid <= 0 {
		return ""
	}
	dir := filepath.Join(root, strconv.Itoa(pid))
	if b, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil && len(b) > 0 {
		args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		name := strings.TrimPrefix(filepath.Base(args[0]), "-") // a login shell: "-zsh"
		if interpreters[name] {
			for _, a := range args[1:] {
				if a == "-c" || len(a) > 2 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a, 'c') {
					break // code given on the command line (sh -c, sh -ec …): no script
				}
				if !strings.HasPrefix(a, "-") {
					return filepath.Base(a)
				}
			}
		}
		if name != "" && name != "." {
			return name
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Parent is a process's parent's PID; 0 when it is not known.
func Parent(pid int) int {
	b, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0
	}
	// "PID (comm) STATE PPID …": comm may hold spaces and parentheses.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 2 {
		return 0
	}
	ppid, _ := strconv.Atoi(f[1])
	return ppid
}

// Chain names pid and the processes it runs under, most direct first:
// "gpg ← pass ← zsh ← ghostty". At most n names; a run of one name is
// named once; it stops before the session (systemd, the compositor …).
func Chain(pid, n int) string {
	var names []string
	for ; pid > 1 && len(names) < n; pid = Parent(pid) {
		name := Name(pid)
		if name == "" || session[name] {
			break
		}
		if len(names) == 0 || names[len(names)-1] != name {
			names = append(names, name)
		}
	}
	return strings.Join(names, " ← ")
}

// Describe names a process for a message: "zed (pid 4123)", or the PID
// alone when it has gone.
func Describe(pid int) string {
	if name := Name(pid); name != "" {
		return fmt.Sprintf("%s (pid %d)", name, pid)
	}
	return fmt.Sprintf("pid %d", pid)
}
