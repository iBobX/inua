package system

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadSysInfo(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"/proc/sys/kernel/hostname":         "laptop\n",
		"/proc/sys/kernel/osrelease":        "6.8.0-138-generic\n",
		"/proc/sys/kernel/arch":             "x86_64\n",
		"/proc/uptime":                      "93784.21 1200.00\n",
		"/etc/os-release":                   "NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 26.04.1 LTS\"\nID=ubuntu\n",
		"/sys/class/dmi/id/sys_vendor":      "Framework\n",
		"/sys/class/dmi/id/product_name":    "Laptop 13 (AMD Ryzen 7040Series)\n",
		"/sys/class/dmi/id/product_version": "A7\n",
		"/proc/cpuinfo": "processor\t: 0\nmodel name\t: AMD Ryzen 7 7840U w/ Radeon 780M Graphics\n\n" +
			"processor\t: 1\nmodel name\t: AMD Ryzen 7 7840U w/ Radeon 780M Graphics\n",
		"/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq": "5132000\n",
		"/sys/class/drm/card1/device/vendor":                    "0x1002\n",
		"/sys/class/drm/card1/device/device":                    "0x15bf\n",
		"/sys/class/drm/card1-DP-1/status":                      "disconnected\n",
		"/usr/share/hwdata/pci.ids": "# comment\n1002  Advanced Micro Devices, Inc. [AMD/ATI]\n\t15bf  Phoenix1\n\t\t1234 5678  A subsystem\n" +
			"10de  NVIDIA Corporation\n\t2684  AD102 [GeForce RTX 4090]\n",
		"/proc/meminfo":                        "MemTotal:       16384000 kB\nMemFree:  100 kB\nMemAvailable:    8192000 kB\nSwapTotal:       2048000 kB\nSwapFree:        2048000 kB\n",
		"/proc/mounts":                         "sysfs /sys sysfs rw 0 0\n/dev/nvme0n1p2 / ext4 rw,relatime 0 0\n",
		"/var/lib/dpkg/status":                 "Package: a\nStatus: install ok installed\n\nPackage: b\nStatus: deinstall ok config-files\n\nPackage: c\nStatus: install ok installed\n",
		"/snap/firefox/1/x":                    "",
		"/snap/bin/firefox":                    "",
		"/snap/README":                         "",
		"/var/lib/flatpak/app/org.gimp.GIMP/x": "",
	})

	s := ReadSysInfo(root)
	if s.Disk.Total == 0 {
		t.Error("no disk size")
	}
	s.Disk = Usage{}
	want := SysInfo{
		Host: "laptop", OS: "Ubuntu 26.04.1 LTS", OSID: "ubuntu", Arch: "x86_64", Kernel: "6.8.0-138-generic",
		Model:    "Framework Laptop 13 (AMD Ryzen 7040Series)",
		Uptime:   26*time.Hour + 3*time.Minute + 4*time.Second,
		Packages: []Packages{{"dpkg", 2}, {"flatpak", 1}, {"snap", 1}},
		CPU:      "AMD Ryzen 7 7840U", Cores: 2, MHz: 5132,
		GPUs:   []string{"AMD Phoenix1"},
		Memory: Usage{Used: 8192000 * 1024, Total: 16384000 * 1024},
		Swap:   Usage{Used: 0, Total: 2048000 * 1024},
		DiskFS: "ext4",
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("info\n got %+v\nwant %+v", s, want)
	}
}

func TestMachineModel(t *testing.T) {
	for _, c := range []struct {
		files map[string]string
		want  string
	}{
		{map[string]string{"sys_vendor": "LENOVO", "product_name": "21K5CTO1WW", "product_version": "ThinkPad T14s Gen 4"}, "LENOVO 21K5CTO1WW (ThinkPad T14s Gen 4)"},
		{map[string]string{"sys_vendor": "System manufacturer", "product_name": "System Product Name", "board_vendor": "ASUSTeK COMPUTER INC.", "board_name": "ROG STRIX B650E-F"}, "ASUSTeK COMPUTER INC. ROG STRIX B650E-F"},
		{map[string]string{"sys_vendor": "Dell Inc.", "product_name": "XPS 13 9310", "product_version": "Not Specified"}, "Dell Inc. XPS 13 9310"},
		{map[string]string{}, ""},
	} {
		dir := t.TempDir()
		for name, body := range c.files {
			writeFiles(t, dir, map[string]string{name: body + "\n"})
		}
		if got := machineModel(dir); got != c.want {
			t.Errorf("machineModel(%v) = %q, want %q", c.files, got, c.want)
		}
	}
}

func TestReadCPU(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"cpuinfo": "processor : 0\nmodel name : Intel(R) Core(TM) i7-1185G7 @ 3.00GHz\n"})
	if model, cores := readCPU(filepath.Join(dir, "cpuinfo")); model != "Intel Core i7-1185G7" || cores != 1 {
		t.Errorf("readCPU = %q %d", model, cores)
	}
}
