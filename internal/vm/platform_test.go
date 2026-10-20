package vm

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/vm/testutil"
)

func TestDetectPlatformWithInjectedProbes(t *testing.T) {
	root := testutil.Dir(t)
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "os-release"), []byte("ID=ubuntu\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"dev/kvm", "var/run/docker.sock", "dev/net/tun"} {
		path := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	programs := map[string]string{
		"qemu-system-x86_64": "/usr/bin/qemu-system-x86_64",
		"systemd-run":        "/usr/bin/systemd-run",
		"remote-viewer":      "/usr/bin/remote-viewer",
	}
	platform := DetectPlatform(root, PlatformProbes{
		Exists:         func(string) bool { return true },
		Accessible:     func(string) bool { return true },
		CommandOutput:  func(string, ...string) (string, error) { return "v2.29.1", nil },
		Executable:     func(n string) bool { return n == "xfreerdp3" || n == "xdg-open" },
		ExecutablePath: func(n string) string { return programs[n] },
		Architecture:   func() string { return "amd64" },
		Filesystem:     func(string) string { return "ext4" },
	})
	if !platform.Supported || platform.Distribution != "ubuntu" || !platform.KVM.Accessible || !platform.DockerSocket.Accessible || !platform.ComposeV2.Available || !platform.FreeRDP.Available || !platform.BrowserOpener.Available || !platform.Disk.Available || !platform.TUN.Available {
		t.Fatalf("incomplete platform detection: %+v", platform)
	}
	if !platform.QEMU.Available || !platform.Firmware.Available || !platform.SystemdRun.Available || !platform.SPICEViewer.Available {
		t.Fatalf("incomplete QEMU host detection: %+v", platform)
	}
	// The checks that ask after a program say where it was found.
	if platform.QEMU.Detail != programs["qemu-system-x86_64"] || platform.SystemdRun.Detail != programs["systemd-run"] || platform.SPICEViewer.Detail != programs["remote-viewer"] {
		t.Errorf("the programs' paths should be shown: %+v %+v %+v", platform.QEMU, platform.SystemdRun, platform.SPICEViewer)
	}
	if len(platform.Doctor) != 13 {
		t.Fatalf("doctor items = %d", len(platform.Doctor))
	}
}

// The window shows one line per check and words each by name, and a package
// hint switches on it: the names are a contract, so the whole list is asserted
// here, in order. A check that quietly disappears would fail this test.
func TestDoctorChecksAreNamedAndOrdered(t *testing.T) {
	p := DetectPlatform(t.TempDir(), PlatformProbes{
		CommandOutput: func(string, ...string) (string, error) { return "", errors.New("no docker here") },
	})
	want := []string{
		"Operating system",
		"KVM",
		"QEMU",
		"Firmware (UEFI)",
		"Systemd",
		"SPICE viewer",
		"osinfo-db",
		"Docker",
		"Docker Compose v2",
		"FreeRDP",
		"Browser opener",
		"Disk",
		"TUN",
	}
	got := make([]string, 0, len(p.Doctor))
	for _, item := range p.Doctor {
		got = append(got, item.Name)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("doctor checks = %v, want %v", got, want)
	}
}

// Without the programs and the firmware each of the QEMU checks fails, and
// says it is missing: that is what the window words and what the package
// hint under it is chosen from.
func TestQEMUChecksReportMissing(t *testing.T) {
	p := DetectPlatform(t.TempDir(), PlatformProbes{
		Exists:         func(string) bool { return false },
		Accessible:     func(string) bool { return false },
		CommandOutput:  func(string, ...string) (string, error) { return "", errors.New("no docker here") },
		Executable:     func(string) bool { return false },
		ExecutablePath: func(string) string { return "" },
		Architecture:   func() string { return "amd64" },
		Filesystem:     func(string) string { return "ext4" },
	})
	byName := map[string]DoctorItem{}
	for _, item := range p.Doctor {
		byName[item.Name] = item
	}
	for _, name := range []string{"QEMU", "Firmware (UEFI)", "Systemd", "SPICE viewer"} {
		item, ok := byName[name]
		if !ok {
			t.Fatalf("no %s check: %+v", name, p.Doctor)
		}
		if item.OK || item.Reason != ReasonMissing {
			t.Errorf("%s check = %+v, want a missing reason", name, item)
		}
		if item.Remedy != nil {
			t.Errorf("%s offers a fix of its own: %+v", name, item.Remedy)
		}
	}
	if p.QEMU.Available || p.Firmware.Available || p.SystemdRun.Available || p.SPICEViewer.Available {
		t.Errorf("a host with nothing installed should provide nothing: %+v", p)
	}
}

// The QEMU program is the one for this host's own architecture, which is the
// only one its guest can run on. An architecture this code has no name for has
// no program to look for, so nothing is looked for.
func TestQEMUCheckFollowsTheHostArchitecture(t *testing.T) {
	for arch, program := range map[string]string{
		"amd64":   "qemu-system-x86_64",
		"arm64":   "qemu-system-aarch64",
		"riscv64": "qemu-system-riscv64",
		"386":     "qemu-system-i386",
	} {
		var asked []string
		p := DetectPlatform(t.TempDir(), PlatformProbes{
			CommandOutput: func(string, ...string) (string, error) { return "", errors.New("no docker here") },
			Architecture:  func() string { return arch },
			ExecutablePath: func(name string) string {
				asked = append(asked, name)
				return "/usr/bin/" + name
			},
		})
		if !p.QEMU.Available || p.QEMU.Detail != "/usr/bin/"+program {
			t.Errorf("%s: QEMU = %+v, want %s", arch, p.QEMU, "/usr/bin/"+program)
		}
		if !slices.Contains(asked, program) {
			t.Errorf("%s: looked for %v, want %s among them", arch, asked, program)
		}
	}

	p := DetectPlatform(t.TempDir(), PlatformProbes{
		CommandOutput: func(string, ...string) (string, error) { return "", errors.New("no docker here") },
		Architecture:  func() string { return "loong64" },
		ExecutablePath: func(name string) string {
			if strings.HasPrefix(name, "qemu-system-") {
				t.Errorf("looked for %q for an architecture with no name", name)
			}
			return ""
		},
	})
	if p.QEMU.Available || p.QEMU.Detail == "" {
		t.Errorf("an unnamed architecture should say so: %+v", p.QEMU)
	}
}

// The firmware is a search, not a path: Debian and Ubuntu, Fedora, Arch and
// NixOS each put their OVMF image somewhere else, so a host with any one of
// them passes. The test is a fake root holding exactly one of them.
func TestFirmwareCheckFindsAnInstalledOVMF(t *testing.T) {
	for _, tc := range []struct{ name, candidate string }{
		{"debian and ubuntu", "/usr/share/OVMF/OVMF_CODE_4M.fd"},
		{"fedora", "/usr/share/edk2/x64/OVMF_CODE.4m.fd"},
		{"arch", "/usr/share/edk2-ovmf/x64/OVMF_CODE.fd"},
		{"nixos", "/run/libvirt/nix-ovmf/OVMF_CODE.fd"},
	} {
		root := testutil.Dir(t)
		path := filepath.Join(root, strings.TrimPrefix(tc.candidate, "/"))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		p := DetectPlatform(root, PlatformProbes{
			Accessible:     func(string) bool { return false },
			CommandOutput:  func(string, ...string) (string, error) { return "", errors.New("no docker here") },
			Executable:     func(string) bool { return false },
			ExecutablePath: func(string) string { return "" },
			Architecture:   func() string { return "amd64" },
			Filesystem:     func(string) string { return "ext4" },
		})
		if !p.Firmware.Available || p.Firmware.Detail != path {
			t.Errorf("%s: firmware = %+v, want %s", tc.name, p.Firmware, path)
		}
		var found bool
		for _, item := range p.Doctor {
			if item.Name == "Firmware (UEFI)" {
				found = item.OK
			}
		}
		if !found {
			t.Errorf("%s: the doctor should pass the firmware: %+v", tc.name, p.Doctor)
		}
	}
}

func TestPlanPackagesRequiresConfirmation(t *testing.T) {
	for distro, manager := range map[string]string{"nixos": "nix", "ubuntu": "apt", "debian": "apt"} {
		plan := PlanPackages(distro)
		if plan.PackageManager != manager || len(plan.Packages) == 0 || !plan.RequiresUserConfirmation || plan.Confirmation == "" {
			t.Errorf("invalid plan for %s: %+v", distro, plan)
		}
	}
	// Any Linux whose family is known gets a plan; one with no known family
	// has no package names to offer and gets none.
	if p := PlanPackages("arch"); p.PackageManager != "pacman" || len(p.Packages) == 0 {
		t.Errorf("arch is a known family and should get a plan: %+v", p)
	}
	if p := PlanPackages("plan9"); p.PackageManager != "" || len(p.Packages) != 0 {
		t.Errorf("a distribution with no known family should get no plan: %+v", p)
	}
}

// The plan has to name the QEMU backend's needs too, not only the Docker
// backend's: the QEMU program for this host's architecture, the firmware and
// the SPICE viewer. A NixOS host has no package manager to run those through,
// so its plan says they go into the system configuration.
func TestPlanPackagesNamesTheQEMUChecksPackages(t *testing.T) {
	// Debian builds one QEMU per guest architecture; the host runs its own.
	wantQEMU := map[string]string{
		"amd64":   "qemu-system-x86",
		"arm64":   "qemu-system-arm",
		"riscv64": "qemu-system-riscv64",
		"386":     "qemu-system-misc",
	}[runtime.GOARCH]
	if wantQEMU == "" {
		t.Skipf("no Debian QEMU package for %s", runtime.GOARCH)
	}
	for _, distro := range []string{"debian", "ubuntu"} {
		plan := PlanPackages(distro)
		for _, want := range []string{wantQEMU, "ovmf", "virt-viewer", "osinfo-db"} {
			if !slices.Contains(plan.Packages, want) {
				t.Errorf("%s plan does not name %s: %v", distro, want, plan.Packages)
			}
		}
	}

	nix := PlanPackages("nixos")
	for _, want := range []string{"OVMF", "virt-viewer", "osinfo-db"} {
		if !slices.Contains(nix.Packages, want) {
			t.Errorf("nixos plan does not name %s: %v", want, nix.Packages)
		}
	}
	if !strings.Contains(nix.Confirmation, "environment.systemPackages") {
		t.Errorf("the nixos plan should say where the names belong: %q", nix.Confirmation)
	}
}

func TestDetectPlatformChecksComposeV2Output(t *testing.T) {
	for _, tc := range []struct {
		output string
		err    error
		want   bool
	}{{"v2.29.1", nil, true}, {"v1.29.2", nil, false}, {"", errors.New("missing plugin"), false}} {
		platform := DetectPlatform(t.TempDir(), PlatformProbes{CommandOutput: func(name string, args ...string) (string, error) {
			if name != "docker" || strings.Join(args, " ") != "compose version --short" {
				t.Errorf("unexpected command: %s %v", name, args)
			}
			return tc.output, tc.err
		}})
		if platform.ComposeV2.Available != tc.want {
			t.Errorf("Compose output %q, error %v: %+v", tc.output, tc.err, platform.ComposeV2)
		}
	}
}

// A socket can be opened only by those the daemon lets in. When this user is
// not among them the window says so and stops there: membership in the docker
// group is as good as root on the host, so it is explained, never offered as
// Inua's own one-click fix. The kvm group, which grants one device, keeps its
// remedy.
func TestDockerSocketDeniedOffersNoOneClickFix(t *testing.T) {
	root := testutil.Dir(t)
	if err := os.MkdirAll(filepath.Join(root, "var", "run"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "dev"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dev", "kvm"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	// A socket that answers, so the dial cannot be mistaken for a daemon that
	// is merely down; the probes then report this user as denied.
	ln, err := net.Listen("unix", filepath.Join(root, "var", "run", "docker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	sockPath := filepath.Join(root, "var", "run", "docker.sock")
	t.Setenv("DOCKER_HOST", "unix://"+sockPath)
	p := DetectPlatform(root, PlatformProbes{
		Exists:        func(string) bool { return true },
		Accessible:    func(string) bool { return false },
		Executable:    func(string) bool { return true },
		Filesystem:    func(string) string { return "ext4" },
		CommandOutput: func(string, ...string) (string, error) { return "v2.29.1", nil },
	})
	byName := map[string]DoctorItem{}
	for _, item := range p.Doctor {
		byName[item.Name] = item
	}
	docker, ok := byName["Docker"]
	if !ok {
		t.Fatalf("no Docker check: %+v", p.Doctor)
	}
	if docker.Reason != ReasonDenied {
		t.Errorf("Docker reason = %q, want %q", docker.Reason, ReasonDenied)
	}
	if docker.Remedy != nil {
		t.Errorf("a denied Docker socket must not offer a fix of its own: %+v", docker.Remedy)
	}
	kvm, ok := byName["KVM"]
	if !ok {
		t.Fatalf("no KVM check: %+v", p.Doctor)
	}
	if kvm.Remedy == nil || kvm.Remedy.Group != "kvm" || !kvm.Remedy.Privileged || !kvm.Remedy.Relogin {
		t.Errorf("the kvm group remedy should stay: %+v", kvm.Remedy)
	}
}

// A Docker socket cannot be opened like a file; accessibility must come from
// connecting to it, or every user would be told they lack access.
func TestDetectPlatformConnectsToDockerSocket(t *testing.T) {
	root := testutil.Dir(t)
	if err := os.MkdirAll(filepath.Join(root, "var", "run"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "dev"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dev", "kvm"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(root, "var", "run", "docker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	p := DetectPlatform(root, PlatformProbes{})
	if !p.DockerSocket.Accessible {
		t.Fatalf("a listening Unix socket should be accessible: %+v", p.DockerSocket)
	}
}

// osinfo-db is the install images' catalog. A host without it still offers a
// page, but a short one of inua's own, so the check says what its absence
// costs rather than only naming what is missing.
func TestOSInfoDBCheckSaysWhatItsAbsenceCosts(t *testing.T) {
	byName := func(p Platform) map[string]DoctorItem {
		out := map[string]DoctorItem{}
		for _, item := range p.Doctor {
			out[item.Name] = item
		}
		return out
	}
	missing := DetectPlatform(testutil.Dir(t), PlatformProbes{
		Exists:         func(string) bool { return false },
		Accessible:     func(string) bool { return false },
		CommandOutput:  func(string, ...string) (string, error) { return "", errors.New("no docker here") },
		Executable:     func(string) bool { return false },
		ExecutablePath: func(string) string { return "" },
		Architecture:   func() string { return "amd64" },
		Filesystem:     func(string) string { return "ext4" },
	})
	if missing.OSInfoDB.Available {
		t.Errorf("a host with nothing installed reports osinfo-db: %+v", missing.OSInfoDB)
	}
	if !strings.Contains(missing.OSInfoDB.Detail, "list") {
		t.Errorf("the check does not say what its absence costs: %q", missing.OSInfoDB.Detail)
	}
	item, ok := byName(missing)["osinfo-db"]
	if !ok {
		t.Fatalf("no osinfo-db check: %+v", missing.Doctor)
	}
	if item.OK || item.Reason != ReasonMissing || item.Remedy != nil {
		t.Errorf("osinfo-db check = %+v, want a missing reason and no fix of its own", item)
	}

	// A host that has the data package passes the check, and names where the
	// database was found, as the checks that name a path do.
	root := testutil.Dir(t)
	dir := filepath.Join(root, "usr", "share", "osinfo", "os")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	installed := DetectPlatform(root, PlatformProbes{
		Accessible:     func(string) bool { return false },
		CommandOutput:  func(string, ...string) (string, error) { return "", errors.New("no docker here") },
		Executable:     func(string) bool { return false },
		ExecutablePath: func(string) string { return "" },
		Architecture:   func() string { return "amd64" },
		Filesystem:     func(string) string { return "ext4" },
	})
	if !installed.OSInfoDB.Available || installed.OSInfoDB.Detail != dir {
		t.Errorf("osinfo-db = %+v, want it found at %s", installed.OSInfoDB, dir)
	}
	if item, ok := byName(installed)["osinfo-db"]; !ok || !item.OK {
		t.Errorf("the doctor does not pass osinfo-db: %+v", installed.Doctor)
	}
}

// The package plan names osinfo-db, which is the download page's catalog and
// not a QEMU dependency; a NixOS host is told to configure it.
func TestPlanPackagesNamesTheOsinfoCatalog(t *testing.T) {
	for _, distro := range []string{"debian", "ubuntu"} {
		if plan := PlanPackages(distro); !slices.Contains(plan.Packages, "osinfo-db") {
			t.Errorf("the %s plan does not name osinfo-db: %v", distro, plan.Packages)
		}
	}
	if plan := PlanPackages("nixos"); !slices.Contains(plan.Packages, "osinfo-db") {
		t.Errorf("the nixos plan does not name osinfo-db: %v", plan.Packages)
	}
}

// Any Linux runs the guests: the distribution only decides how the missing
// tools are named, not whether a machine can be installed at all. A
// distribution is placed in the family whose packages are known, itself or
// the one it derives from (os-release's ID_LIKE).
func TestAnyLinuxDistributionIsSupported(t *testing.T) {
	for _, tc := range []struct {
		osRelease, distro, family string
	}{
		{"ID=nixos\n", "nixos", "nixos"},
		{"ID=ubuntu\nID_LIKE=\"debian\"\n", "ubuntu", "ubuntu"},
		{"ID=debian\n", "debian", "debian"},
		{"ID=arch\n", "arch", "arch"},
		{"ID=fedora\n", "fedora", "fedora"},
		{"ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", "linuxmint", "ubuntu"},
		{"ID=alpine\n", "alpine", ""}, // supported, but no package names known
	} {
		root := testutil.Dir(t)
		if err := os.MkdirAll(filepath.Join(root, "etc"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "etc", "os-release"), []byte(tc.osRelease), 0600); err != nil {
			t.Fatal(err)
		}
		p := DetectPlatform(root, PlatformProbes{Exists: func(string) bool { return false }})
		if !p.Supported {
			t.Errorf("%s should be supported: %+v", tc.distro, p)
		}
		if p.Distribution != tc.distro {
			t.Errorf("distribution = %q, want %q", p.Distribution, tc.distro)
		}
		if p.Family != tc.family {
			t.Errorf("family = %q, want %q", p.Family, tc.family)
		}
	}
}

// The socket is the one the docker command would use, not only the system's:
// DOCKER_HOST first, then a rootless daemon's in the runtime directory, then
// the system's. Rootless Docker and Podman pass this way; a host that names
// another kind (tcp://, ssh://) is left to the command itself.
func TestDockerSocketFollowsTheDockerCommand(t *testing.T) {
	exists := func(p string) bool { return p == "/run/user/1000/docker.sock" || p == "/var/run/docker.sock" }
	if got := dockerSocket("unix:///tmp/custom.sock", "/run/user/1000", "/var/run/docker.sock", exists); got != "/tmp/custom.sock" {
		t.Errorf("DOCKER_HOST unix:// = %q, want /tmp/custom.sock", got)
	}
	if got := dockerSocket("tcp://127.0.0.1:2375", "/run/user/1000", "/var/run/docker.sock", exists); got != "" {
		t.Errorf("DOCKER_HOST tcp:// = %q, want empty: only the command can check it", got)
	}
	if got := dockerSocket("", "/run/user/1000", "/var/run/docker.sock", exists); got != "/run/user/1000/docker.sock" {
		t.Errorf("rootless = %q, want the runtime directory's socket", got)
	}
	if got := dockerSocket("", "/run/user/9999", "/var/run/docker.sock", exists); got != "/var/run/docker.sock" {
		t.Errorf("fallback = %q, want the system socket", got)
	}
}
