package vm

import (
	"bufio"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Platform describes host facilities needed to run and connect to a Windows
// VM: what the Docker backend needs - a socket, Compose, FreeRDP, TUN - and
// what the one that runs QEMU needs instead: the QEMU program for this host's
// own architecture, UEFI firmware, systemd-run for a guest that outlives the
// window, and a SPICE viewer to see it with.
type Platform struct {
	OS           string
	Distribution string
	// Family is the distribution, or the one it derives from (os-release's
	// ID_LIKE), whose packages PackageFor names; "" when it knows none.
	Family        string
	Supported     bool
	KVM           DeviceCapability
	QEMU          Capability
	Firmware      Capability
	SystemdRun    Capability
	SPICEViewer   Capability
	OSInfoDB      Capability
	DockerSocket  DeviceCapability
	ComposeV2     Capability
	FreeRDP       Capability
	BrowserOpener Capability
	Disk          Capability
	Filesystem    string
	TUN           DeviceCapability
	Doctor        []DoctorItem
}

type Capability struct {
	Available bool
	Detail    string
}
type DeviceCapability struct {
	Available  bool
	Accessible bool
	Path       string
	Detail     string
}

// DoctorReason says why a check failed, so the caller can say it in the
// user's language. A check that passed has none. The set is small and stable.
type DoctorReason string

const (
	ReasonMissing DoctorReason = "missing" // the device, tool or service is not there
	ReasonDenied  DoctorReason = "denied"  // it is there, but this user cannot use it
	ReasonStopped DoctorReason = "stopped" // the service is there but not running
	ReasonTooOld  DoctorReason = "too_old" // a version older than the one required
	ReasonUnclear DoctorReason = "unclear" // could not be told apart
)

// Remedy is what fixes a failing check, when Inua can do it at all. A check
// the user must fix by hand (installing a package) carries none, and the
// window shows the platform's instructions instead.
//
// A privileged remedy runs through pkexec, so the session's polkit agent
// decides whether to ask for a password; it never runs with the caller's
// rights.
type Remedy struct {
	// What is the fix, in the imperative: "Add your user to the docker group".
	What string
	// Group is the account group the fix adds the user to, when it does.
	Group string
	// Privileged means the command needs root, and runs through pkexec.
	Privileged bool
	// Argv is the command, run without a shell. {User} and {Group} are
	// replaced with this session's user and Group.
	Argv []string
	// Relogin says the fix only takes effect after the user logs out and in
	// again - a process's group list cannot be changed from inside it.
	Relogin bool
}

// DoctorItem is one host check: what it needs, whether it holds, when it does
// not why, and how to fix it when Inua can. The text is a stable identifier,
// so the window can word all of it in the user's language.
type DoctorItem struct {
	Name   string
	OK     bool
	Reason DoctorReason
	Remedy *Remedy
}

type PlatformProbes struct {
	Executable    func(string) bool
	Accessible    func(string) bool
	Filesystem    func(string) string
	Exists        func(string) bool
	CommandOutput func(string, ...string) (string, error)
	// ExecutablePath is Executable's fuller answer: the path a program is at,
	// or "" when it is not on PATH. The checks that name what they found -
	// the QEMU program, systemd-run, the SPICE viewer - show that path.
	ExecutablePath func(string) string
	// Architecture is the host's own architecture, in place of runtime.GOARCH,
	// so a test on one machine can ask which QEMU program another is looked
	// for under.
	Architecture func() string
}

// DetectPlatform inspects host support without modifying system state. Root maps absolute
// paths into a fixture root; probe functions make host-dependent checks deterministic in tests.
func DetectPlatform(root string, probes PlatformProbes) Platform {
	if root == "" {
		root = "/"
	}
	if probes.Exists == nil {
		probes.Exists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	}
	if probes.Accessible == nil {
		probes.Accessible = func(p string) bool {
			fi, err := os.Stat(p)
			if err != nil {
				return false
			}
			if fi.Mode()&os.ModeSocket != 0 {
				// A Unix socket cannot be opened like a file: connecting is
				// what checks the permission bits.
				c, err := net.DialTimeout("unix", p, 250*time.Millisecond)
				if err != nil {
					return false
				}
				_ = c.Close()
				return true
			}
			f, err := os.OpenFile(p, os.O_RDWR, 0)
			if err != nil {
				return false
			}
			_ = f.Close()
			return true
		}
	}
	if probes.ExecutablePath == nil {
		probes.ExecutablePath = func(name string) string {
			path, err := exec.LookPath(name)
			if err != nil {
				return ""
			}
			return path
		}
	}
	if probes.Executable == nil {
		probes.Executable = func(name string) bool { return probes.ExecutablePath(name) != "" }
	}
	if probes.Architecture == nil {
		probes.Architecture = func() string { return runtime.GOARCH }
	}
	if probes.CommandOutput == nil {
		probes.CommandOutput = func(name string, args ...string) (string, error) {
			out, err := exec.Command(name, args...).CombinedOutput()
			return string(out), err
		}
	}
	mapPath := func(p string) string {
		return filepath.Join(root, strings.TrimPrefix(filepath.Clean(p), string(filepath.Separator)))
	}
	osName, distro, like := hostOS(root)
	p := Platform{OS: osName, Distribution: distro, Family: packageFamily(append([]string{distro}, like...)), Supported: osName == "linux"}
	kvm := mapPath("/dev/kvm")
	p.KVM = DeviceCapability{Path: kvm, Available: probes.Exists(kvm), Accessible: probes.Accessible(kvm)}
	check := func(name string, ok bool, reason DoctorReason) DoctorItem {
		return DoctorItem{Name: name, OK: ok, Reason: reason}
	}

	// The remedies Inua can carry out itself: adding the user to a device's
	// group. A process's group list cannot be changed from inside it, so the
	// fix only takes hold at the next login.
	groupRemedy := func(group string) *Remedy {
		return &Remedy{
			What:       "Add your user to the " + group + " group",
			Group:      group,
			Privileged: true,
			Argv:       []string{"gpasswd", "-a", "{User}", group},
			Relogin:    true,
		}
	}

	kvmCheck := check("KVM", p.KVM.Available && p.KVM.Accessible, ReasonMissing)
	if p.KVM.Available && !p.KVM.Accessible {
		// The device is there; this user is not in the group it belongs to.
		kvmCheck.Reason = ReasonDenied
		kvmCheck.Remedy = groupRemedy("kvm")
	}

	// What the QEMU backend needs. A guest of another architecture is emulated
	// rather than run, and the backend names its own program, so the check
	// asks after this host's architecture alone.
	p.QEMU = Capability{Detail: "no QEMU program for this architecture"}
	if program := QEMUProgram(archFor(probes.Architecture())); program != "" {
		p.QEMU = pathCapability(probes, program, program+" unavailable")
	}
	// The UEFI firmware is a search, not a path: which OVMF image a host has is
	// a distribution question. The list is the backend's own, looked for under
	// root so a test can be a host that has one.
	p.Firmware = Capability{Detail: "no UEFI firmware (OVMF) found"}
	for _, candidate := range platformOVMFCodeFiles() {
		if path := mapPath(candidate); probes.Exists(path) {
			p.Firmware = Capability{Available: true, Detail: path}
			break
		}
	}
	// A machine runs under systemd-run, so that the guest outlives the window
	// that started it and dies with the session.
	p.SystemdRun = pathCapability(probes, "systemd-run", "systemd-run unavailable")
	// remote-viewer, from virt-viewer, is how a SPICE machine is seen.
	p.SPICEViewer = pathCapability(probes, "remote-viewer", "remote-viewer unavailable")
	// osinfo-db is the install images' catalog: the download page asks it what
	// the distributions publish rather than naming URLs itself. Without it the
	// page still works, but on inua's own three images instead of the
	// distributions' current list, and the check says so rather than only
	// naming what is missing.
	p.OSInfoDB = Capability{Detail: "osinfo-db is not installed: install images are offered from inua's own three-image list instead of the distributions' catalog"}
	if dir := mapPath("/usr/share/osinfo/os"); probes.Exists(dir) {
		p.OSInfoDB = Capability{Available: true, Detail: dir}
	}

	socket := dockerSocket(os.Getenv("DOCKER_HOST"), os.Getenv("XDG_RUNTIME_DIR"), mapPath("/var/run/docker.sock"), probes.Exists)
	p.DockerSocket = DeviceCapability{Path: socket, Available: probes.Exists(socket), Accessible: probes.Accessible(socket)}
	socketCheck := check("Docker", p.DockerSocket.Available && p.DockerSocket.Accessible, ReasonMissing)
	if p.DockerSocket.Available && !p.DockerSocket.Accessible {
		// The socket is there; either this user cannot talk to it, or the
		// daemon is not running. Dial once more to tell the two apart, so
		// the window does not say Docker is down when it is only this user.
		if err := dockerDial(socket); err != nil && isConnRefused(err) {
			socketCheck.Reason = ReasonStopped
			p.DockerSocket.Detail = "Docker is not running"
		} else {
			// No remedy: membership in the docker group is as good as root
			// on this host, so it is never Inua's one-click fix. The window
			// explains the risk and points at rootless Docker or Podman;
			// the kvm group, which grants one device, keeps its remedy.
			socketCheck.Reason = ReasonDenied
		}
	}

	composeVersion, composeErr := composeVersion(probes)
	compose := composeErr == nil && composeVersion != ""
	p.ComposeV2 = Capability{Available: compose, Detail: "Docker Compose v2 plugin unavailable"}
	if compose {
		p.ComposeV2.Detail = "Docker Compose " + composeVersion + " available"
	}
	p.FreeRDP = executableCapability(probes, RDPClients, "FreeRDP unavailable")
	p.BrowserOpener = executableCapability(probes, []string{"xdg-open"}, "browser opener xdg-open unavailable")
	tunPath := mapPath("/dev/net/tun")
	p.TUN = DeviceCapability{Path: tunPath, Available: probes.Exists(tunPath), Accessible: probes.Accessible(tunPath)}
	tunCheck := check("TUN", p.TUN.Available && p.TUN.Accessible, ReasonMissing)
	if p.TUN.Available && !p.TUN.Accessible {
		tunCheck.Reason = ReasonDenied
	}
	p.Filesystem = "unknown"
	if probes.Filesystem != nil {
		p.Filesystem = probes.Filesystem(root)
	} else {
		p.Filesystem = detectFilesystem(root)
	}
	p.Disk = Capability{Available: p.Filesystem != "" && p.Filesystem != "unknown", Detail: "filesystem: " + p.Filesystem}

	osCheck := check("Operating system", p.Supported, ReasonUnclear)
	if osName == "linux" && distro != "" {
		osCheck.Reason = ReasonTooOld // an unsupported distribution, said plainly
	}

	// A check that passes says nothing about why; a failing one carries the
	// reason, and a remedy when Inua can apply one.
	p.Doctor = []DoctorItem{
		osCheck,
		kvmCheck,
		check("QEMU", p.QEMU.Available, ReasonMissing),
		check("Firmware (UEFI)", p.Firmware.Available, ReasonMissing),
		check("Systemd", p.SystemdRun.Available, ReasonMissing),
		check("SPICE viewer", p.SPICEViewer.Available, ReasonMissing),
		check("osinfo-db", p.OSInfoDB.Available, ReasonMissing),
		socketCheck,
		check("Docker Compose v2", p.ComposeV2.Available, ReasonMissing),
		check("FreeRDP", p.FreeRDP.Available, ReasonMissing),
		check("Browser opener", p.BrowserOpener.Available, ReasonMissing),
		check("Disk", p.Disk.Available, ReasonUnclear),
		tunCheck,
	}
	return p
}

// composeVersion reads the Compose v2 plugin's version. Docker prints it bare
// ("2.39.1") or with a leading "v", depending on the release.
func composeVersion(p PlatformProbes) (string, error) {
	out, err := p.CommandOutput("docker", "compose", "version", "--short")
	if err != nil {
		return "", err
	}
	v := strings.TrimPrefix(strings.TrimSpace(out), "v")
	if v == "" || strings.HasPrefix(v, "1.") {
		return "", errors.New("Docker Compose v2 is not available")
	}
	return v, nil
}

// detectFilesystem names the filesystem holding the user's home, where the VM
// disk lives; "unknown" when /proc/mounts cannot be read.
func detectFilesystem(root string) string {
	if root != "/" {
		return "unknown"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "unknown"
	}
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "unknown"
	}
	best, fs := "", ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		mount := strings.ReplaceAll(fields[1], `\040`, " ")
		if mount == "/" || home == mount || strings.HasPrefix(home, mount+"/") {
			if len(mount) > len(best) {
				best, fs = mount, fields[2]
			}
		}
	}
	if fs == "" {
		return "unknown"
	}
	return fs
}

func dockerDial(path string) error {
	c, err := net.DialTimeout("unix", path, 250*time.Millisecond)
	if err != nil {
		return err
	}
	_ = c.Close()
	return nil
}

func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "connect: no such file")
}

// RDPClients are FreeRDP's clients, in the order Connect tries them. The
// check and Connect share this list, so what the doctor says is missing is
// exactly what Connect would look for.
var RDPClients = []string{"sdl-freerdp3", "sdl-freerdp", "wlfreerdp3", "wlfreerdp", "xfreerdp3", "xfreerdp"}

func executableCapability(p PlatformProbes, names []string, missing string) Capability {
	for _, name := range names {
		if p.Executable(name) {
			return Capability{true, name + " available"}
		}
	}
	return Capability{false, missing}
}

// pathCapability is a capability that holds when the program is on PATH, and
// names the path it was found at: that is what the doctor line shows, and the
// only thing worth reading when a machine will not start.
func pathCapability(p PlatformProbes, name, missing string) Capability {
	if path := p.ExecutablePath(name); path != "" {
		return Capability{true, path}
	}
	return Capability{false, missing}
}

// QEMUProgram is the QEMU binary that runs a guest of one architecture, or ""
// for one this code has no name for: no binary, and nothing of that kind can be
// started here. The host's own architecture is the one the checks ask about; a
// machine of another architecture needs its own, and the window asks this for
// the machine's.
func QEMUProgram(arch Arch) string {
	switch arch {
	case ArchX8664:
		return "qemu-system-x86_64"
	case ArchAarch64:
		return "qemu-system-aarch64"
	case ArchRiscv64:
		return "qemu-system-riscv64"
	case ArchI386:
		return "qemu-system-i386"
	}
	return ""
}

// hostQEMUProgram is QEMUProgram for the host's own architecture, which is what
// the host check names.
func hostQEMUProgram() string {
	switch runtime.GOARCH {
	case "amd64":
		return QEMUProgram(ArchX8664)
	case "arm64":
		return QEMUProgram(ArchAarch64)
	case "riscv64":
		return QEMUProgram(ArchRiscv64)
	case "386":
		return QEMUProgram(ArchI386)
	}
	return ""
}

// qemuSystemPackage is Debian's package holding that program. Debian builds
// one binary package per guest architecture; the guests it does not build for
// itself - 386 among them - are in qemu-system-misc.
func qemuSystemPackage(arch string) string {
	switch arch {
	case "amd64":
		return "qemu-system-x86"
	case "arm64":
		return "qemu-system-arm"
	case "riscv64":
		return "qemu-system-riscv64"
	case "386":
		return "qemu-system-misc"
	}
	return ""
}

// platformOVMFCodeFiles are the UEFI firmware images the distributions
// install, in the order they are looked for. It is the same search the QEMU
// backend makes: Debian and Ubuntu's /usr/share/OVMF, Fedora and openSUSE's
// edk2, Arch's edk2-ovmf, a NixOS libvirt's /run/libvirt/nix-ovmf, and the
// aarch64 and riscv64 builds. Which one a host has is a distribution question,
// so it is a search and not a path.
// UEFIFirmwarePath is the OVMF code image this host has, or "" when it has
// none. The QEMU backend boots a UEFI machine with it, and the check says
// whether there is one at all.
func UEFIFirmwarePath() string {
	for _, path := range platformOVMFCodeFiles() {
		if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

// archFor is Go's name for an architecture as the Arch this package uses.
func archFor(goarch string) Arch {
	switch goarch {
	case "amd64":
		return ArchX8664
	case "arm64":
		return ArchAarch64
	case "riscv64":
		return ArchRiscv64
	case "386":
		return ArchI386
	}
	return ""
}

func platformOVMFCodeFiles() []string {
	return []string{
		"/usr/share/edk2/x64/OVMF_CODE.4m.fd",
		"/usr/share/edk2/x64/OVMF_CODE.fd",
		"/usr/share/OVMF/OVMF_CODE_4M.fd",
		"/usr/share/OVMF/OVMF_CODE.fd",
		"/usr/share/edk2-ovmf/x64/OVMF_CODE.fd",
		"/usr/share/edk2-ovmf/OVMF_CODE.fd",
		"/usr/share/qemu/OVMF_CODE.fd",
		"/run/libvirt/nix-ovmf/OVMF_CODE.fd",
		"/usr/share/edk2/aarch64/QEMU_EFI.fd",
		"/usr/share/qemu-efi-aarch64/QEMU_EFI.fd",
		"/usr/share/AAVMF/AAVMF_CODE.fd",
		"/usr/share/edk2/riscv64/RISCV_VIRT_CODE.fd",
	}
}

func hostOS(root string) (string, string, []string) {
	if runtime.GOOS != "linux" {
		return runtime.GOOS, "", nil
	}
	f, err := os.Open(filepath.Join(root, "etc", "os-release"))
	if err != nil {
		return "linux", "", nil
	}
	defer f.Close()
	values := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		k, v, ok := strings.Cut(line, "=")
		if ok {
			values[k] = strings.Trim(strings.TrimSpace(v), "\"'")
		}
	}
	id := strings.ToLower(values["ID"])
	if id == "" {
		return "linux", "", nil
	}
	return "linux", id, strings.Fields(strings.ToLower(values["ID_LIKE"]))
}
func supportedDistribution(d string) bool { return d == "nixos" || d == "ubuntu" || d == "debian" }
func distroDetail(osName, distro string) string {
	if osName != "linux" {
		return "Windows VM host support requires Linux"
	}
	// Any Linux runs the guests; only the package names it installs them
	// with are unknown when no family is known.
	return distro + " supported"
}

// packages are, per distribution family, its package manager and the package
// each check needs. The QEMU names are the machines' backend: the QEMU program
// for this host's architecture, the UEFI firmware, the SPICE viewer, and the
// install images' catalog.
var packages = map[string]struct {
	manager string
	byCheck map[string]string
}{
	"arch": {"pacman", map[string]string{
		"Docker": "docker", "Docker Compose v2": "docker-compose", "FreeRDP": "freerdp", "Browser opener": "xdg-utils",
		"QEMU": "qemu-desktop", "Firmware (UEFI)": "edk2-ovmf", "SPICE viewer": "virt-viewer", "osinfo-db": "osinfo-db",
	}},
	"debian": {"apt", map[string]string{
		"Docker": "docker.io", "Docker Compose v2": "docker-compose", "FreeRDP": "freerdp3-sdl", "Browser opener": "xdg-utils",
		"QEMU": "qemu-system-x86", "Firmware (UEFI)": "ovmf", "SPICE viewer": "virt-viewer", "osinfo-db": "osinfo-db",
	}},
	"fedora": {"dnf", map[string]string{
		"Docker": "moby-engine", "Docker Compose v2": "docker-compose", "FreeRDP": "freerdp", "Browser opener": "xdg-utils",
		"QEMU": "qemu-system-x86", "Firmware (UEFI)": "edk2-ovmf", "SPICE viewer": "virt-viewer", "osinfo-db": "osinfo-db",
	}},
	"nixos": {"nix", map[string]string{
		"Docker": "docker", "Docker Compose v2": "docker", "FreeRDP": "freerdp", "Browser opener": "xdg-utils",
		"QEMU": "qemu_kvm", "Firmware (UEFI)": "OVMF", "SPICE viewer": "virt-viewer", "osinfo-db": "osinfo-db",
	}},
	"ubuntu": {"apt", map[string]string{
		"Docker": "docker.io", "Docker Compose v2": "docker-compose-v2", "FreeRDP": "freerdp3-sdl", "Browser opener": "xdg-utils",
		"QEMU": "qemu-system-x86", "Firmware (UEFI)": "ovmf", "SPICE viewer": "virt-viewer", "osinfo-db": "osinfo-db",
	}},
}

// packageFamily is the first of ids whose packages are known: the
// distribution itself, else the one it derives from.
func packageFamily(ids []string) string {
	for _, id := range ids {
		if _, ok := packages[id]; ok {
			return id
		}
	}
	return ""
}

// PackageFor is how family installs what check needs: its package manager and
// the package, or "" for both when there is none to name. It only names them;
// Inua never runs a package manager.
func PackageFor(family, check string) (manager, pkg string) {
	p, ok := packages[family]
	if !ok || p.byCheck[check] == "" {
		return "", ""
	}
	return p.manager, p.byCheck[check]
}

// dockerSocket is the socket the docker command uses when it is a local one:
// DOCKER_HOST's, else a rootless daemon's in the runtime directory, else the
// system's. It is "" when DOCKER_HOST names another kind (tcp://, ssh://),
// which only the command itself can check.
func dockerSocket(host, runtimeDir, system string, exists func(string) bool) string {
	if host != "" {
		path, ok := strings.CutPrefix(host, "unix://")
		if !ok {
			return ""
		}
		return path
	}
	if runtimeDir != "" {
		if rootless := filepath.Join(runtimeDir, "docker.sock"); exists(rootless) {
			return rootless
		}
	}
	return system
}

type PackagePlan struct {
	Distribution             string
	PackageManager           string
	Packages                 []string
	RequiresUserConfirmation bool
	Confirmation             string
}

// PlanPackages returns recommendations only; it never runs a package manager or changes groups.
func PlanPackages(distribution string) PackagePlan {
	d := strings.ToLower(distribution)
	family := packageFamily([]string{d})
	p := PackagePlan{Distribution: d, RequiresUserConfirmation: true, Confirmation: "Review and confirm before installing packages or changing system configuration."}
	if manager, _ := PackageFor(family, "Docker"); manager != "" {
		p.PackageManager = manager
	}
	// Every check this branch's backends can ask for, named the way this
	// family installs them. The QEMU program is the one for this host's
	// architecture, which is the only guest it can accelerate.
	for _, check := range []string{"Docker", "Docker Compose v2", "FreeRDP", "Browser opener", "QEMU", "Firmware (UEFI)", "SPICE viewer", "osinfo-db"} {
		_, pkg := PackageFor(family, check)
		if pkg == "" {
			continue
		}
		if check == "QEMU" {
			if q := qemuSystemPackage(runtime.GOARCH); q != "" && (family == "debian" || family == "ubuntu") {
				pkg = q
			}
		}
		p.Packages = append(p.Packages, pkg)
	}
	if family == "nixos" {
		p.Confirmation = "Add these to the system configuration - environment.systemPackages - or turn on virtualisation.libvirtd, which is what the firmware and the SPICE viewer come from. Then rebuild."
	}
	return p
}
