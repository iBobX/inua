// Package sysmon is the System Monitor app's logic: the machine's meters
// (CPU, memory, disk, network, battery, temperature) and its processes,
// read from /proc and /sys.
package sysmon

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lyricat/inua/internal/i18n"
)

// Root is where /proc and /sys are; tests point it elsewhere.
var Root = "/"

func path(p string) string { return filepath.Join(Root, p) }

// CPUTimes are the counters on /proc/stat's first line, in clock ticks.
type CPUTimes struct{ Busy, Total uint64 }

// ParseCPU reads /proc/stat: the overall counters and the number of CPUs.
func ParseCPU(stat string) (CPUTimes, int) {
	var t CPUTimes
	n := 0
	for _, line := range strings.Split(stat, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		if f[0] != "cpu" {
			n++
			continue
		}
		for i, v := range f[1:] {
			x, _ := strconv.ParseUint(v, 10, 64)
			t.Total += x
			if i != 3 && i != 4 { // idle and iowait
				t.Busy += x
			}
		}
	}
	return t, n
}

// Usage is the share of time busy between two readings, 0 to 100.
func Usage(prev, cur CPUTimes) float64 {
	if cur.Total <= prev.Total {
		return 0
	}
	return 100 * float64(cur.Busy-prev.Busy) / float64(cur.Total-prev.Total)
}

// Memory is RAM and swap, in bytes.
type Memory struct {
	Total, Available, Free   uint64
	Buffers, Cached, Reclaim uint64 // the page cache, and kernel memory it can take back
	Shmem                    uint64 // shared memory, counted in Cached but not freeable
	SwapTotal, SwapFree      uint64
}

// Used is the RAM in use.
func (m Memory) Used() uint64 { return m.Total - min(m.Available, m.Total) }

// Cache is the RAM the system lends to files and can take back.
func (m Memory) Cache() uint64 {
	c := m.Buffers + m.Cached + m.Reclaim
	return c - min(m.Shmem, c)
}

// ParseMemory reads /proc/meminfo.
func ParseMemory(meminfo string) Memory {
	var m Memory
	for _, line := range strings.Split(meminfo, "\n") {
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
		case "MemTotal":
			m.Total = kb * 1024
		case "MemAvailable":
			m.Available = kb * 1024
		case "SwapTotal":
			m.SwapTotal = kb * 1024
		case "SwapFree":
			m.SwapFree = kb * 1024
		case "MemFree":
			m.Free = kb * 1024
		case "Buffers":
			m.Buffers = kb * 1024
		case "Cached":
			m.Cached = kb * 1024
		case "SReclaimable":
			m.Reclaim = kb * 1024
		case "Shmem":
			m.Shmem = kb * 1024
		}
	}
	return m
}

// NetBytes are the bytes received and sent by every interface but lo.
type NetBytes struct{ Rx, Tx uint64 }

// ParseNet reads /proc/net/dev.
func ParseNet(dev string) NetBytes {
	var n NetBytes
	for _, line := range strings.Split(dev, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "lo" {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		n.Rx += rx
		n.Tx += tx
	}
	return n
}

// Disk is a filesystem's size and use, in bytes.
type Disk struct{ Total, Free uint64 }

// Used is the space in use.
func (d Disk) Used() uint64 { return d.Total - min(d.Free, d.Total) }

// ReadDisk reads the filesystem holding dir.
func ReadDisk(dir string) (Disk, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return Disk{}, err
	}
	bs := uint64(st.Bsize)
	return Disk{Total: st.Blocks * bs, Free: st.Bavail * bs}, nil
}

// Battery is the first battery's charge.
type Battery struct {
	Present  bool
	Percent  int
	Status   string  // "Charging", "Discharging", "Full", …
	Watts    float64 // drawn or taken, 0 if unknown
	Capacity float64 // health: full charge now against new, %, 0 if unknown
}

// ReadBattery reads /sys/class/power_supply/BAT*.
func ReadBattery() Battery {
	dirs, _ := filepath.Glob(path("sys/class/power_supply/BAT*"))
	if len(dirs) == 0 {
		return Battery{}
	}
	d := dirs[0]
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(d, name))
		return strings.TrimSpace(string(b))
	}
	num := func(name string) float64 {
		v, _ := strconv.ParseFloat(read(name), 64)
		return v
	}
	b := Battery{Present: true, Status: read("status")}
	b.Percent, _ = strconv.Atoi(read("capacity"))
	if p := num("power_now"); p > 0 {
		b.Watts = p / 1e6
	} else if c, v := num("current_now"), num("voltage_now"); c > 0 && v > 0 {
		b.Watts = c * v / 1e12
	}
	full, design := num("energy_full"), num("energy_full_design")
	if full == 0 {
		full, design = num("charge_full"), num("charge_full_design")
	}
	if design > 0 {
		b.Capacity = 100 * full / design
	}
	return b
}

// ReadTemperature is the hottest thermal zone's temperature in °C, 0 if
// none reports.
func ReadTemperature() float64 {
	zones, _ := filepath.Glob(path("sys/class/thermal/thermal_zone*/temp"))
	var hot float64
	for _, z := range zones {
		b, err := os.ReadFile(z)
		if err != nil {
			continue
		}
		v, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
		hot = max(hot, v/1000)
	}
	return hot
}

// Uptime is how long the machine has been up.
func Uptime() time.Duration {
	b, err := os.ReadFile(path("proc/uptime"))
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	s, _ := strconv.ParseFloat(f[0], 64)
	return time.Duration(s * float64(time.Second))
}

// Load is the 1, 5 and 15 minute load averages.
func Load() [3]float64 {
	var l [3]float64
	b, err := os.ReadFile(path("proc/loadavg"))
	if err != nil {
		return l
	}
	for i, f := range strings.Fields(string(b)) {
		if i < 3 {
			l[i], _ = strconv.ParseFloat(f, 64)
		}
	}
	return l
}

// Process is one running program.
type Process struct {
	PID, PPID int
	Name      string
	UID       int
	User      string
	State     byte // R running, S sleeping, D waiting on a disk, T stopped, Z a zombie, …
	Threads   int
	Ticks     uint64 // user and system time, in clock ticks
	RSS       uint64 // resident memory, bytes
	CPU       float64
}

// Stat is what a /proc/PID/stat line says.
type Stat struct {
	Name          string
	State         byte
	PPID          int
	Ticks         uint64 // user and system time
	Nice, Threads int
	Start         uint64 // when it started, in clock ticks since boot
	RSSPages      uint64
	UTime, STime  uint64
}

// ParseStat reads a /proc/PID/stat line.
func ParseStat(line string) (Stat, bool) {
	open, close := strings.IndexByte(line, '('), strings.LastIndexByte(line, ')')
	if open < 0 || close < open {
		return Stat{}, false
	}
	st := Stat{Name: line[open+1 : close]}
	f := strings.Fields(line[close+1:])
	// Fields after the name: state f[0], ppid f[1], utime f[11], stime
	// f[12], nice f[16], threads f[17], start time f[19], rss f[21].
	if len(f) < 22 || len(f[0]) == 0 {
		return Stat{}, false
	}
	st.State = f[0][0]
	st.PPID, _ = strconv.Atoi(f[1])
	st.UTime, _ = strconv.ParseUint(f[11], 10, 64)
	st.STime, _ = strconv.ParseUint(f[12], 10, 64)
	st.Ticks = st.UTime + st.STime
	st.Nice, _ = strconv.Atoi(f[16])
	st.Threads, _ = strconv.Atoi(f[17])
	st.Start, _ = strconv.ParseUint(f[19], 10, 64)
	st.RSSPages, _ = strconv.ParseUint(f[21], 10, 64)
	return st, true
}

// pageSize is the size of a memory page.
var pageSize = uint64(os.Getpagesize())

// ReadProcesses lists the running processes.
func ReadProcesses() []Process {
	entries, err := os.ReadDir(path("proc"))
	if err != nil {
		return nil
	}
	users := Users()
	var ps []Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := path(filepath.Join("proc", e.Name()))
		b, err := os.ReadFile(filepath.Join(dir, "stat"))
		if err != nil {
			continue
		}
		st, ok := ParseStat(string(b))
		if !ok || st.RSSPages == 0 { // kernel threads have no memory of their own
			continue
		}
		p := Process{PID: pid, PPID: st.PPID, Name: st.Name, State: st.State, Threads: st.Threads,
			Ticks: st.Ticks, RSS: st.RSSPages * pageSize, UID: -1}
		if fi, err := os.Stat(dir); err == nil {
			if s, ok := fi.Sys().(*syscall.Stat_t); ok {
				p.UID = int(s.Uid)
				p.User = users[p.UID]
			}
		}
		ps = append(ps, p)
	}
	return ps
}

// clockTicks is the kernel's USER_HZ: 100 on every Linux Go runs on.
const clockTicks = 100

// SetCPU fills in each process's CPU use since the earlier reading, in
// percent of one CPU, over elapsed time.
func SetCPU(ps []Process, earlier map[int]uint64, elapsed time.Duration) {
	if elapsed <= 0 {
		return
	}
	for i := range ps {
		if t, ok := earlier[ps[i].PID]; ok && ps[i].Ticks >= t {
			ps[i].CPU = 100 * float64(ps[i].Ticks-t) / clockTicks / elapsed.Seconds()
		}
	}
}

// Sort orders processes: by "cpu" or "memory", the most first, or by
// "name".
func Sort(ps []Process, by string) {
	slices.SortStableFunc(ps, func(a, b Process) int {
		switch by {
		case "memory":
			return cmpDesc(float64(a.RSS), float64(b.RSS))
		case "name":
			if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
				return c
			}
			return a.PID - b.PID
		}
		if c := cmpDesc(a.CPU, b.CPU); c != 0 {
			return c
		}
		return cmpDesc(float64(a.RSS), float64(b.RSS))
	})
}

func cmpDesc(a, b float64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	}
	return 0
}

// Bytes shows a size: "512 B", "1.5 KB", "6.1 GB".
func Bytes(n uint64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	f := "%.1f %cB"
	if v >= 100 {
		f = "%.0f %cB"
	}
	return fmt.Sprintf(f, v, "KMGTP"[exp])
}

// Rate shows a transfer rate: "1.2 MB/s".
func Rate(bytesPerSecond float64) string { return Bytes(uint64(max(0, bytesPerSecond))) + "/s" }

// Duration shows an uptime: "3 d 4 h", "4 h 12 min", "12 min".
func Duration(d time.Duration) string {
	m := int(d.Minutes())
	switch days, hours := m/1440, m/60%24; {
	case days > 0:
		return i18n.Tf("%d d %d h", days, hours)
	case m >= 60:
		return i18n.Tf("%d h %d min", m/60, m%60)
	}
	return i18n.Tf("%d min", m)
}

// ReadFile reads a /proc or /sys file as text, "" if it cannot.
func ReadFile(p string) string {
	f, err := os.Open(path(p))
	if err != nil {
		return ""
	}
	defer f.Close()
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		b.WriteString(sc.Text())
		b.WriteByte('\n')
	}
	return b.String()
}

// Filter keeps the processes whose name has q in it, any case, whose PID
// starts with q, or whose user is q; all of them when q is blank.
func Filter(ps []Process, q string) []Process {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return ps
	}
	var out []Process
	for _, p := range ps {
		if strings.Contains(strings.ToLower(p.Name), q) || strings.HasPrefix(strconv.Itoa(p.PID), q) ||
			strings.ToLower(p.User) == q {
			out = append(out, p)
		}
	}
	return out
}
