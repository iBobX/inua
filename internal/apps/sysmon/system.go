package sysmon

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ParseCores reads /proc/stat's line for each CPU (cpu0, cpu1, …).
func ParseCores(stat string) []CPUTimes {
	var cores []CPUTimes
	for _, line := range strings.Split(stat, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || !strings.HasPrefix(f[0], "cpu") || f[0] == "cpu" {
			continue
		}
		var t CPUTimes
		for i, v := range f[1:] {
			x, _ := strconv.ParseUint(v, 10, 64)
			t.Total += x
			if i != 3 && i != 4 {
				t.Busy += x
			}
		}
		cores = append(cores, t)
	}
	return cores
}

// CPUInfo is the processor: its model, cores and threads, and top speed.
type CPUInfo struct {
	Model          string
	Cores, Threads int
	MaxMHz         float64
}

// ParseCPUInfo reads /proc/cpuinfo.
func ParseCPUInfo(s string) CPUInfo {
	var c CPUInfo
	cores := map[string]bool{}
	var phys string
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "processor":
			c.Threads++
		case "model name":
			if c.Model == "" {
				c.Model = strings.Join(strings.Fields(v), " ")
			}
		case "physical id":
			phys = v
		case "core id":
			cores[phys+"/"+v] = true
		}
	}
	c.Cores = len(cores)
	if c.Cores == 0 {
		c.Cores = c.Threads
	}
	return c
}

// ReadCPUInfo reads the processor's description and top speed.
func ReadCPUInfo() CPUInfo {
	c := ParseCPUInfo(ReadFile("proc/cpuinfo"))
	if khz, err := strconv.ParseFloat(strings.TrimSpace(ReadFile("sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq")), 64); err == nil {
		c.MaxMHz = khz / 1000
	}
	return c
}

// ReadFrequency is the CPUs' speed now, on average, in MHz; 0 if unknown.
func ReadFrequency() float64 {
	files, _ := filepath.Glob(path("sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq"))
	var sum float64
	n := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if khz, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); err == nil {
			sum += khz
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n) / 1000
}

// DiskIO is what the disks have read and written since boot, in bytes.
type DiskIO struct{ Read, Written uint64 }

// ParseDiskStats reads /proc/diskstats, counting whole disks only (not
// their partitions, loop devices, or the layers built on disks, which
// would count the same bytes twice). isDisk tells a whole disk by name.
func ParseDiskStats(s string, isDisk func(name string) bool) DiskIO {
	var io DiskIO
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		name := f[2]
		for _, skip := range []string{"loop", "ram", "zram", "dm-", "md", "sr"} {
			if strings.HasPrefix(name, skip) {
				name = ""
			}
		}
		if name == "" || !isDisk(name) {
			continue
		}
		r, _ := strconv.ParseUint(f[5], 10, 64)
		w, _ := strconv.ParseUint(f[9], 10, 64)
		io.Read += r * 512
		io.Written += w * 512
	}
	return io
}

// ReadDiskIO reads the disks' counters.
func ReadDiskIO() DiskIO {
	return ParseDiskStats(ReadFile("proc/diskstats"), func(name string) bool {
		_, err := os.Stat(path("sys/block/" + name))
		return err == nil
	})
}

// Filesystem is a mounted disk's file system.
type Filesystem struct {
	Mount, Device, Type string
	Total, Free         uint64
}

// Used is the space taken.
func (f Filesystem) Used() uint64 { return f.Total - min(f.Free, f.Total) }

// diskTypes are the file systems on disks, as against virtual ones.
var diskTypes = []string{"ext2", "ext3", "ext4", "btrfs", "xfs", "vfat", "exfat", "ntfs", "ntfs3", "fuseblk", "f2fs", "zfs", "bcachefs", "jfs", "reiserfs", "hfsplus", "apfs"}

// ParseMounts reads /proc/mounts: the disks' file systems, each device
// once (a btrfs disk is mounted for each subvolume), at its shortest
// mount point.
func ParseMounts(s string) []Filesystem {
	var out []Filesystem
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !slices.Contains(diskTypes, f[2]) {
			continue
		}
		fs := Filesystem{Device: unescape(f[0]), Mount: unescape(f[1]), Type: f[2]}
		if i := slices.IndexFunc(out, func(o Filesystem) bool { return o.Device == fs.Device }); i >= 0 {
			if len(fs.Mount) < len(out[i].Mount) {
				out[i] = fs
			}
			continue
		}
		out = append(out, fs)
	}
	return out
}

// unescape undoes /proc/mounts' octal escapes ("\040" for a space).
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ReadFilesystems lists the disks' file systems with their space.
func ReadFilesystems() []Filesystem {
	fss := ParseMounts(ReadFile("proc/self/mounts"))
	for i := range fss {
		if d, err := ReadDisk(fss[i].Mount); err == nil {
			fss[i].Total, fss[i].Free = d.Total, d.Free
		}
	}
	return fss
}

// Sensor is one reading of a hardware monitor: a temperature (°C) or a
// fan's speed (RPM).
type Sensor struct {
	Chip, Label string
	Value       float64
	Fan         bool
}

// ReadSensors reads the hardware monitors' temperatures and fans.
func ReadSensors() []Sensor {
	dirs, _ := filepath.Glob(path("sys/class/hwmon/hwmon*"))
	slices.Sort(dirs)
	var out []Sensor
	for _, d := range dirs {
		chip := strings.TrimSpace(readAbs(filepath.Join(d, "name")))
		for _, kind := range []string{"temp", "fan"} {
			inputs, _ := filepath.Glob(filepath.Join(d, kind+"*_input"))
			slices.SortFunc(inputs, func(a, b string) int { return strings.Compare(sensorNum(a), sensorNum(b)) })
			for _, in := range inputs {
				v, err := strconv.ParseFloat(strings.TrimSpace(readAbs(in)), 64)
				if err != nil {
					continue
				}
				base := strings.TrimSuffix(filepath.Base(in), "_input")
				label := strings.TrimSpace(readAbs(filepath.Join(d, base+"_label")))
				if label == "" {
					label = base
				}
				s := Sensor{Chip: chip, Label: label, Value: v, Fan: kind == "fan"}
				if !s.Fan {
					s.Value = v / 1000
					if s.Value <= -40 || s.Value >= 200 { // not connected
						continue
					}
				}
				out = append(out, s)
			}
		}
	}
	return out
}

func sensorNum(p string) string {
	b := filepath.Base(p)
	n := strings.TrimLeft(b, "abcdefghijklmnopqrstuvwxyz")
	n = strings.TrimSuffix(n, "_input")
	return strings.Repeat("0", max(0, 4-len(n))) + n
}

func readAbs(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// GPU is a graphics card: its name, driver, how busy it is (-1 when the
// driver does not say) and its video memory.
type GPU struct {
	Name, Driver        string
	Busy                float64
	VRAMUsed, VRAMTotal uint64
}

// ReadGPUs reads the graphics cards the DRM drivers show.
func ReadGPUs() []GPU {
	cards, _ := filepath.Glob(path("sys/class/drm/card[0-9]*"))
	slices.Sort(cards)
	var out []GPU
	for _, c := range cards {
		if strings.Contains(filepath.Base(c), "-") { // a connector, card0-DP-1
			continue
		}
		dev := filepath.Join(c, "device")
		g := GPU{Busy: -1}
		var pciID string
		for _, line := range strings.Split(readAbs(filepath.Join(dev, "uevent")), "\n") {
			if v, ok := strings.CutPrefix(line, "DRIVER="); ok {
				g.Driver = v
			}
			if v, ok := strings.CutPrefix(line, "PCI_ID="); ok {
				pciID = v
			}
		}
		if g.Driver == "" {
			continue
		}
		if b, err := strconv.ParseFloat(strings.TrimSpace(readAbs(filepath.Join(dev, "gpu_busy_percent"))), 64); err == nil {
			g.Busy = b
		}
		g.VRAMUsed, _ = strconv.ParseUint(strings.TrimSpace(readAbs(filepath.Join(dev, "mem_info_vram_used"))), 10, 64)
		g.VRAMTotal, _ = strconv.ParseUint(strings.TrimSpace(readAbs(filepath.Join(dev, "mem_info_vram_total"))), 10, 64)
		if v, d, ok := strings.Cut(pciID, ":"); ok {
			g.Name = PCIName(v, d)
		}
		out = append(out, g)
	}
	return out
}

// pciIDs are where the PCI ID database may be.
var pciIDs = []string{"usr/share/misc/pci.ids", "usr/share/hwdata/pci.ids"}

// PCIName names a PCI device by its vendor and device IDs from the PCI ID
// database, "" when it is not there: "AMD Navi 21 [Radeon RX 6800]".
func PCIName(vendor, device string) string {
	vendor, device = strings.ToLower(vendor), strings.ToLower(device)
	for _, p := range pciIDs {
		f, err := os.Open(path(p))
		if err != nil {
			continue
		}
		defer f.Close()
		return findPCI(bufio.NewScanner(f), vendor, device)
	}
	return ""
}

func findPCI(sc *bufio.Scanner, vendor, device string) string {
	var vname string
	in := false
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] != '\t' {
			if in {
				return vname // the vendor's devices are over
			}
			if id, name, ok := strings.Cut(line, "  "); ok && id == vendor {
				in, vname = true, shortVendor(name)
			}
			continue
		}
		if in && !strings.HasPrefix(line, "\t\t") {
			if id, name, ok := strings.Cut(line[1:], "  "); ok && id == device {
				return vname + " " + name
			}
		}
	}
	if in {
		return vname
	}
	return ""
}

// shortVendor keeps a vendor's name short: "Advanced Micro Devices, Inc.
// [AMD/ATI]" is "AMD".
func shortVendor(name string) string {
	switch {
	case strings.Contains(name, "[AMD"):
		return "AMD"
	case strings.HasPrefix(name, "Intel"):
		return "Intel"
	case strings.HasPrefix(name, "NVIDIA"):
		return "NVIDIA"
	}
	if i := strings.Index(name, ","); i > 0 {
		return name[:i]
	}
	return name
}

// SystemInfo is the computer and its system.
type SystemInfo struct {
	Host, OS, Kernel, Model string
	Boot                    time.Time
}

// ReadSystemInfo reads the computer's name, model, system and boot time.
func ReadSystemInfo() SystemInfo {
	var s SystemInfo
	s.Host, _ = os.Hostname()
	for _, line := range strings.Split(ReadFile("etc/os-release"), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			s.OS = strings.Trim(v, `"`)
		}
	}
	// ReadFile ends each line with a newline.
	read := func(p string) string { return strings.TrimSpace(ReadFile(p)) }
	s.Kernel = read("proc/sys/kernel/osrelease")
	vendor := read("sys/class/dmi/id/sys_vendor")
	product := read("sys/class/dmi/id/product_name")
	if version := read("sys/class/dmi/id/product_version"); strings.EqualFold(vendor, "LENOVO") && version != "" {
		product = version // Lenovo names the model there
	}
	s.Model = strings.TrimSpace(vendor + " " + product)
	for _, line := range strings.Split(ReadFile("proc/stat"), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if t, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				s.Boot = time.Unix(t, 0)
			}
		}
	}
	return s
}

// ParseNetDevices reads /proc/net/dev for each interface.
func ParseNetDevices(dev string) map[string]NetBytes {
	out := map[string]NetBytes{}
	for _, line := range strings.Split(dev, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		out[strings.TrimSpace(name)] = NetBytes{rx, tx}
	}
	return out
}

// Interface is a network interface: its addresses, kind and traffic.
type Interface struct {
	Name, MAC string
	Addrs     []string
	Up        bool
	Wireless  bool
	Traffic   NetBytes
}

// ReadInterfaces lists the network interfaces but loopback, up ones first.
func ReadInterfaces() []Interface {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	traffic := ParseNetDevices(ReadFile("proc/net/dev"))
	var out []Interface
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback != 0 {
			continue
		}
		it := Interface{Name: i.Name, MAC: i.HardwareAddr.String(), Up: i.Flags&net.FlagUp != 0, Traffic: traffic[i.Name]}
		if _, err := os.Stat(path("sys/class/net/" + i.Name + "/wireless")); err == nil {
			it.Wireless = true
		}
		if addrs, err := i.Addrs(); err == nil {
			for _, a := range addrs {
				it.Addrs = append(it.Addrs, a.String())
			}
		}
		out = append(out, it)
	}
	slices.SortStableFunc(out, func(a, b Interface) int {
		if a.Up != b.Up && len(a.Addrs) > 0 != (len(b.Addrs) > 0) {
			if len(a.Addrs) > 0 {
				return -1
			}
			return 1
		}
		return 0
	})
	return out
}
