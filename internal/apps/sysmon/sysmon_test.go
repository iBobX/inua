package sysmon

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCPU(t *testing.T) {
	a, n := ParseCPU("cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 50 0 50 350 50 0 0 0 0 0\ncpu1 50 0 50 350 50 0 0 0 0 0\nintr 1\n")
	b, _ := ParseCPU("cpu  200 0 200 1300 300 0 0 0 0 0\n")
	if n != 2 || a.Total != 1000 || a.Busy != 200 {
		t.Fatalf("parse %+v n=%d", a, n)
	}
	if got := Usage(a, b); got != 200.0/1000*100 {
		t.Errorf("Usage = %v", got)
	}
	if Usage(b, a) != 0 {
		t.Error("counters going back is no use")
	}
}

func TestMemoryNet(t *testing.T) {
	m := ParseMemory("MemTotal:       16000000 kB\nMemFree: 1 kB\nMemAvailable:    6000000 kB\nSwapTotal: 2000 kB\nSwapFree: 1000 kB\n")
	if m.Used() != 10000000*1024 || m.SwapTotal != 2000*1024 {
		t.Errorf("%+v used %d", m, m.Used())
	}
	n := ParseNet("Inter-|   Receive\n face |bytes packets\n    lo: 999 1 0 0 0 0 0 0 999 1 0 0 0 0 0 0\n  wlp1s0: 1000 10 0 0 0 0 0 0 200 5 0 0 0 0 0 0\n  eth0: 24 1 0 0 0 0 0 0 6 1 0 0 0 0 0 0\n")
	if n.Rx != 1024 || n.Tx != 206 {
		t.Errorf("%+v", n)
	}
}

func TestStat(t *testing.T) {
	line := "1234 (Web Content (x)) S 1 2 3 4 5 6 7 8 9 10 300 200 0 0 20 0 30 0 99 123456 2500 18446744073709551615"
	st, ok := ParseStat(line)
	if !ok || st.Name != "Web Content (x)" || st.Ticks != 500 || st.RSSPages != 2500 || st.State != 'S' ||
		st.PPID != 1 || st.Threads != 30 || st.Start != 99 {
		t.Errorf("%+v %v", st, ok)
	}
	ps := []Process{{PID: 1, Ticks: 150, RSS: 10}, {PID: 2, Ticks: 50, RSS: 30}, {PID: 3, Ticks: 9}}
	SetCPU(ps, map[int]uint64{1: 100, 2: 50}, 2*time.Second)
	if ps[0].CPU != 25 || ps[1].CPU != 0 || ps[2].CPU != 0 {
		t.Errorf("CPU %v %v %v", ps[0].CPU, ps[1].CPU, ps[2].CPU)
	}
	Sort(ps, "memory")
	if ps[0].PID != 2 {
		t.Errorf("by memory first %d", ps[0].PID)
	}
	Sort(ps, "cpu")
	if ps[0].PID != 1 {
		t.Errorf("by CPU first %d", ps[0].PID)
	}
	named := []Process{{PID: 3, Name: "zsh"}, {PID: 2, Name: "Bash"}, {PID: 1, Name: "bash"}}
	Sort(named, "name")
	if named[0].PID != 1 || named[1].PID != 2 || named[2].Name != "zsh" {
		t.Errorf("by name %+v", named)
	}
}

func TestSysFiles(t *testing.T) {
	Root = t.TempDir()
	defer func() { Root = "/" }()
	write := func(p, s string) {
		full := filepath.Join(Root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(s), 0o644)
	}
	write("sys/class/power_supply/BAT0/capacity", "79\n")
	write("sys/class/power_supply/BAT0/status", "Discharging\n")
	write("sys/class/power_supply/BAT0/power_now", "8500000\n")
	write("sys/class/power_supply/BAT0/energy_full", "45000000\n")
	write("sys/class/power_supply/BAT0/energy_full_design", "50000000\n")
	write("sys/class/thermal/thermal_zone0/temp", "41000\n")
	write("sys/class/thermal/thermal_zone1/temp", "57500\n")
	write("proc/uptime", "3725.5 100\n")
	write("proc/loadavg", "1.20 0.90 0.70 2/900 12345\n")
	b := ReadBattery()
	if !b.Present || b.Percent != 79 || b.Watts != 8.5 || b.Capacity != 90 || b.Status != "Discharging" {
		t.Errorf("%+v", b)
	}
	if got := ReadTemperature(); got != 57.5 {
		t.Errorf("temperature %v", got)
	}
	if got := Duration(Uptime()); got != "1 h 2 min" {
		t.Errorf("uptime %q", got)
	}
	if l := Load(); l[0] != 1.2 || l[2] != 0.7 {
		t.Errorf("load %v", l)
	}
}

func TestFormats(t *testing.T) {
	for n, want := range map[uint64]string{512: "512 B", 1500: "1.5 KB", 6_100_000_000: "6.1 GB", 250_000_000: "250 MB"} {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
	if got := Duration(76 * time.Hour); got != "3 d 4 h" {
		t.Error(got)
	}
}

func TestFilter(t *testing.T) {
	ps := []Process{{PID: 120, Name: "Firefox", User: "lyra"}, {PID: 1203, Name: "bash", User: "lyra"}, {PID: 77, Name: "firewalld", User: "root"}}
	names := func(ps []Process) (out []string) {
		for _, p := range ps {
			out = append(out, p.Name)
		}
		return out
	}
	for q, want := range map[string][]string{
		"":     {"Firefox", "bash", "firewalld"},
		"  ":   {"Firefox", "bash", "firewalld"},
		"FIRE": {"Firefox", "firewalld"},
		"120":  {"Firefox", "bash"},
		"77":   {"firewalld"},
		"zsh":  nil,
		"Root": {"firewalld"},
		"lyr":  nil, // a user is matched whole
	} {
		if got := names(Filter(ps, q)); !slices.Equal(got, want) {
			t.Errorf("Filter(%q) = %v, want %v", q, got, want)
		}
	}
}

func TestSystemParsers(t *testing.T) {
	stat := "cpu  10 0 10 80 0 0 0 0\ncpu0 5 0 5 40 0 0 0 0\ncpu1 5 0 5 40 0 0 0 0\nintr 1"
	if c := ParseCores(stat); len(c) != 2 || c[0].Busy != 10 || c[0].Total != 50 {
		t.Errorf("cores %+v", c)
	}
	info := ParseCPUInfo("processor : 0\nmodel name : AMD  Ryzen 7 7840U\nphysical id : 0\ncore id : 0\n\nprocessor : 1\nphysical id : 0\ncore id : 0\n\nprocessor : 2\nphysical id : 0\ncore id : 1\n")
	if info.Model != "AMD Ryzen 7 7840U" || info.Threads != 3 || info.Cores != 2 {
		t.Errorf("cpuinfo %+v", info)
	}
	disks := "   8       0 sda 100 0 2000 0 50 0 4000 0 0 0 0\n   8       1 sda1 90 0 1800 0 40 0 3000 0 0 0 0\n   7       0 loop0 5 0 100 0 0 0 0 0 0 0 0\n 259       0 nvme0n1 10 0 20 0 10 0 40 0 0 0 0\n 253       0 dm-0 9 0 999 0 9 0 999 0 0 0 0"
	io := ParseDiskStats(disks, func(n string) bool { return n == "sda" || n == "nvme0n1" || n == "dm-0" || n == "loop0" })
	if io.Read != (2000+20)*512 || io.Written != (4000+40)*512 {
		t.Errorf("diskstats %+v", io)
	}
	mounts := "/dev/nvme0n1p2 / btrfs rw 0 0\nproc /proc proc rw 0 0\n/dev/nvme0n1p2 /home btrfs rw 0 0\n/dev/nvme0n1p1 /boot/efi vfat rw 0 0\n/dev/sdb1 /media/My\\040Disk exfat rw 0 0\ntmpfs /tmp tmpfs rw 0 0"
	fss := ParseMounts(mounts)
	if len(fss) != 3 || fss[0].Mount != "/" || fss[1].Type != "vfat" || fss[2].Mount != "/media/My Disk" {
		t.Errorf("mounts %+v", fss)
	}
	dev := "Inter-|   Receive\n face |bytes packets\n    lo: 100 1 0 0 0 0 0 0 100 1 0 0 0 0 0 0\n  wlan0: 5000 9 0 0 0 0 0 0 700 4 0 0 0 0 0 0"
	if n := ParseNetDevices(dev); n["wlan0"].Rx != 5000 || n["wlan0"].Tx != 700 || n["lo"].Rx != 100 {
		t.Errorf("net devices %+v", n)
	}
	if u := ParsePasswd("root:x:0:0:root:/root:/bin/bash\nlyra:x:1000:1000::/home/lyra:/bin/zsh\n"); u[0] != "root" || u[1000] != "lyra" {
		t.Errorf("passwd %v", u)
	}
	if c := ParseCgroup("0::/user.slice/user-1000.slice/app.slice/app-kitty-123.scope\n"); c != "app-kitty-123.scope" {
		t.Errorf("cgroup %q", c)
	}
	rss, anon, shared, swap, peak := parseStatus("VmHWM:\t  2048 kB\nVmRSS:\t  1024 kB\nRssAnon:\t 512 kB\nRssFile:\t 256 kB\nRssShmem:\t 256 kB\nVmSwap:\t 8 kB\n")
	if rss != 1024*1024 || anon != 512*1024 || shared != 512*1024 || swap != 8*1024 || peak != 2048*1024 {
		t.Errorf("status %d %d %d %d %d", rss, anon, shared, swap, peak)
	}
	if r, w := parseIO("rchar: 9\nread_bytes: 4096\nwrite_bytes: 8192\n"); r != 4096 || w != 8192 {
		t.Errorf("io %d %d", r, w)
	}
	m := ParseMemory("MemTotal: 1000 kB\nMemFree: 100 kB\nMemAvailable: 600 kB\nBuffers: 50 kB\nCached: 400 kB\nSReclaimable: 30 kB\nShmem: 80 kB\n")
	if m.Cache() != (50+400+30-80)*1024 || m.Free != 100*1024 || m.Used() != 400*1024 {
		t.Errorf("memory %+v cache %d", m, m.Cache())
	}
	ids := "# comment\n1002  Advanced Micro Devices, Inc. [AMD/ATI]\n\t73bf  Navi 21 [Radeon RX 6800/6800 XT / 6900 XT]\n\t\t1002 0e3a  Some board\n8086  Intel Corporation\n\t46a6  Alder Lake-P GT2 [Iris Xe Graphics]\n"
	if n := findPCI(bufio.NewScanner(strings.NewReader(ids)), "1002", "73bf"); n != "AMD Navi 21 [Radeon RX 6800/6800 XT / 6900 XT]" {
		t.Errorf("pci %q", n)
	}
	if n := findPCI(bufio.NewScanner(strings.NewReader(ids)), "8086", "46a6"); n != "Intel Alder Lake-P GT2 [Iris Xe Graphics]" {
		t.Errorf("pci intel %q", n)
	}
	if n := findPCI(bufio.NewScanner(strings.NewReader(ids)), "1002", "ffff"); n != "AMD" {
		t.Errorf("pci unknown device %q", n)
	}
}

func TestSensorsAndGPUs(t *testing.T) {
	Root = t.TempDir()
	defer func() { Root = "/" }()
	write := func(p, s string) {
		os.MkdirAll(filepath.Join(Root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(Root, p), []byte(s), 0o644)
	}
	write("sys/class/hwmon/hwmon0/name", "k10temp\n")
	write("sys/class/hwmon/hwmon0/temp1_input", "54250\n")
	write("sys/class/hwmon/hwmon0/temp1_label", "Tctl\n")
	write("sys/class/hwmon/hwmon0/temp2_input", "-273000\n") // not connected
	write("sys/class/hwmon/hwmon1/name", "thinkpad\n")
	write("sys/class/hwmon/hwmon1/fan1_input", "2400\n")
	s := ReadSensors()
	if len(s) != 2 || s[0].Chip != "k10temp" || s[0].Label != "Tctl" || s[0].Value != 54.25 || !s[1].Fan || s[1].Value != 2400 || s[1].Label != "fan1" {
		t.Errorf("sensors %+v", s)
	}
	write("sys/class/drm/card1/device/uevent", "DRIVER=amdgpu\nPCI_ID=1002:FFFF\n")
	write("sys/class/drm/card1/device/gpu_busy_percent", "37\n")
	write("sys/class/drm/card1/device/mem_info_vram_used", "1048576\n")
	write("sys/class/drm/card1/device/mem_info_vram_total", "4194304\n")
	write("sys/class/drm/card1-eDP-1/status", "connected\n")
	g := ReadGPUs()
	if len(g) != 1 || g[0].Driver != "amdgpu" || g[0].Busy != 37 || g[0].VRAMTotal != 4194304 {
		t.Errorf("gpus %+v", g)
	}
}
