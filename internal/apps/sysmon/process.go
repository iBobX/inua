package sysmon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var users struct {
	once sync.Once
	m    map[int]string
}

// Users are the system's user names by ID, from /etc/passwd.
func Users() map[int]string {
	users.once.Do(func() { users.m = ParsePasswd(ReadFile("etc/passwd")) })
	return users.m
}

// ParsePasswd reads /etc/passwd's names and IDs.
func ParsePasswd(s string) map[int]string {
	m := map[int]string{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Split(line, ":")
		if len(f) < 3 {
			continue
		}
		if id, err := strconv.Atoi(f[2]); err == nil {
			m[id] = f[0]
		}
	}
	return m
}

// StateName says a process's state in words.
func StateName(s byte) string {
	switch s {
	case 'R':
		return "running"
	case 'S':
		return "sleeping"
	case 'D':
		return "waiting on a disk"
	case 'T':
		return "stopped"
	case 't':
		return "stopped by a debugger"
	case 'Z':
		return "zombie"
	case 'I':
		return "idle"
	case 'X':
		return "dead"
	}
	return string(s)
}

// Detail is what the System Monitor shows of one process.
type Detail struct {
	PID, PPID, UID          int
	Name, Parent, User      string
	State                   byte
	Cmdline, Exe, Cwd       string // "" where the process is not ours to read
	Threads, Nice           int
	Started                 time.Time
	CPUTime                 time.Duration
	RSS, Anon, Shared, Swap uint64 // bytes
	Peak                    uint64 // the most it has been resident
	ReadBytes, WriteBytes   int64  // from and to storage; -1 where unknown
	FDs                     int    // files open; -1 where unknown
	Unit                    string // its systemd unit or scope
}

// ReadDetail reads a process's details.
func ReadDetail(pid int) (Detail, error) {
	dir := path(filepath.Join("proc", strconv.Itoa(pid)))
	b, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return Detail{}, err
	}
	st, ok := ParseStat(string(b))
	if !ok {
		return Detail{}, fmt.Errorf("process %d: unreadable", pid)
	}
	d := Detail{PID: pid, PPID: st.PPID, Name: st.Name, State: st.State, Threads: st.Threads, Nice: st.Nice,
		CPUTime: time.Duration(st.Ticks) * time.Second / clockTicks, ReadBytes: -1, WriteBytes: -1, FDs: -1, UID: -1}
	if boot := ReadSystemInfo().Boot; !boot.IsZero() {
		d.Started = boot.Add(time.Duration(st.Start) * time.Second / clockTicks)
	}
	if fi, err := os.Stat(dir); err == nil {
		if s, ok := fi.Sys().(*syscall.Stat_t); ok {
			d.UID = int(s.Uid)
			d.User = Users()[d.UID]
		}
	}
	if pb, err := os.ReadFile(path(filepath.Join("proc", strconv.Itoa(st.PPID), "stat"))); err == nil {
		if ps, ok := ParseStat(string(pb)); ok {
			d.Parent = ps.Name
		}
	}
	if c, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
		d.Cmdline = strings.TrimSpace(strings.ReplaceAll(string(c), "\x00", " "))
	}
	d.Exe, _ = os.Readlink(filepath.Join(dir, "exe"))
	d.Cwd, _ = os.Readlink(filepath.Join(dir, "cwd"))
	d.RSS, d.Anon, d.Shared, d.Swap, d.Peak = parseStatus(readAbs(filepath.Join(dir, "status")))
	if io := readAbs(filepath.Join(dir, "io")); io != "" {
		d.ReadBytes, d.WriteBytes = parseIO(io)
	}
	if fds, err := os.ReadDir(filepath.Join(dir, "fd")); err == nil {
		d.FDs = len(fds)
	}
	d.Unit = ParseCgroup(readAbs(filepath.Join(dir, "cgroup")))
	return d, nil
}

// parseStatus reads /proc/PID/status's memory lines, in bytes.
func parseStatus(s string) (rss, anon, shared, swap, peak uint64) {
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		kb, _ := strconv.ParseUint(f[0], 10, 64)
		switch k {
		case "VmRSS":
			rss = kb * 1024
		case "RssAnon":
			anon = kb * 1024
		case "RssFile", "RssShmem":
			shared += kb * 1024
		case "VmSwap":
			swap = kb * 1024
		case "VmHWM":
			peak = kb * 1024
		}
	}
	return rss, anon, shared, swap, peak
}

// parseIO reads /proc/PID/io: the bytes read from and written to storage.
func parseIO(s string) (read, written int64) {
	read, written = -1, -1
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "read_bytes":
			read = n
		case "write_bytes":
			written = n
		}
	}
	return read, written
}

// ParseCgroup names a process's systemd unit or scope from
// /proc/PID/cgroup: the last part of its path.
func ParseCgroup(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if p, ok := strings.CutPrefix(line, "0::"); ok {
			p = strings.TrimSpace(p)
			if p == "" || p == "/" {
				return ""
			}
			return filepath.Base(p)
		}
	}
	return ""
}

// Elapsed says how long ago a time was, shortly: "3 h 12 min", or
// "under a minute".
func Elapsed(t time.Time) string {
	switch d := time.Since(t); {
	case t.IsZero():
		return ""
	case d < time.Minute:
		return "under a minute"
	default:
		return Duration(d)
	}
}
