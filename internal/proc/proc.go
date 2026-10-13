// Package proc names running processes, from /proc, to tell the user who
// asked for something: a secret, a passphrase.
package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
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
	if args, _ := arguments(pid); len(args) > 0 {
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

// arguments is a process's command line; none when it has gone. A
// program that rewrote it as one line (Chrome, setproctitle) is split at
// spaces, and title is true: where an argument held a space is lost.
func arguments(pid int) (args []string, title bool) {
	b, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil, false
	}
	line := strings.TrimRight(string(b), "\x00")
	if line == "" {
		return nil, false
	}
	if !strings.ContainsRune(line, 0) && strings.ContainsRune(line, ' ') {
		if _, err := os.Stat(line); err != nil { // not a program whose path has a space
			return strings.Fields(line), true
		}
	}
	return strings.Split(line, "\x00"), false
}

// Args is a process's command line, as arguments (see arguments); none
// when it has gone.
func Args(pid int) []string {
	if pid <= 0 {
		return nil
	}
	args, _ := arguments(pid)
	return args
}

// Exe is the full path of a process's program; "" when it has gone or
// belongs to someone else.
func Exe(pid int) string {
	if pid <= 0 {
		return ""
	}
	exe, _ := os.Readlink(filepath.Join(root, strconv.Itoa(pid), "exe"))
	return exe
}

// maxCommand is the longest command line Command gives, in bytes.
const maxCommand = 1024

// Command is a process's command line, its arguments quoted where they
// hold spaces or quotes, at most maxCommand bytes; "" when it has gone.
func Command(pid int) string {
	if pid <= 0 {
		return ""
	}
	args, title := arguments(pid)
	if len(args) == 0 {
		return ""
	}
	cmd := strings.Join(args, " ")
	if !title {
		for i, a := range args {
			if a == "" || strings.ContainsAny(a, " \t\n\"'\\") {
				args[i] = strconv.Quote(a)
			}
		}
		cmd = strings.Join(args, " ")
	}
	if len(cmd) > maxCommand {
		cmd = strings.ToValidUTF8(cmd[:maxCommand], "") + "…"
	}
	return cmd
}

// stat is a process's /proc/PID/stat after its name, field n (0 is the
// state, 1 the parent, 2 the process group, 19 the start time); 0 when it
// is not known.
func stat(pid, n int) int64 {
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
	if len(f) <= n {
		return 0
	}
	v, _ := strconv.ParseInt(f[n], 10, 64)
	return v
}

// Parent is a process's parent's PID; 0 when it is not known.
func Parent(pid int) int { return int(stat(pid, 1)) }

// Group is a process's process group: the job a shell ran it in. A
// program that went to the background (wl-copy) keeps it after its
// parent has gone.
func Group(pid int) int { return int(stat(pid, 2)) }

// clockTicks is the kernel's USER_HZ, which /proc counts times in: 100 on
// every Linux architecture inua runs on.
const clockTicks = 100

// Age is how long a process has run; 0 when it is not known.
func Age(pid int) time.Duration {
	start := stat(pid, 19)
	b, err := os.ReadFile(filepath.Join(root, "uptime"))
	if start == 0 || err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	up, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0
	}
	return max(0, time.Duration(up*float64(time.Second))-time.Duration(start)*time.Second/clockTicks)
}

// All lists the running processes' PIDs.
func All() []int {
	entries, _ := os.ReadDir(root)
	var pids []int
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() {
			pids = append(pids, pid)
		}
	}
	slices.Sort(pids) // the oldest first, mostly: PIDs are given in order until they wrap
	return pids
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
