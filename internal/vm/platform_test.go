package vm

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectPlatformWithInjectedProbes(t *testing.T) {
	root := t.TempDir()
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
	platform := DetectPlatform(root, PlatformProbes{Exists: func(string) bool { return true }, Accessible: func(string) bool { return true }, CommandOutput: func(string, ...string) (string, error) { return "v2.29.1", nil }, Executable: func(n string) bool { return n == "xfreerdp3" || n == "xdg-open" }, Filesystem: func(string) string { return "ext4" }})
	if !platform.Supported || platform.Distribution != "ubuntu" || platform.Family != "ubuntu" || !platform.KVM.Accessible || !platform.DockerSocket.Accessible || !platform.ComposeV2.Available || !platform.FreeRDP.Available || !platform.BrowserOpener.Available || !platform.Disk.Available || !platform.TUN.Available {
		t.Fatalf("incomplete platform detection: %+v", platform)
	}
	if len(platform.Doctor) != 8 {
		t.Fatalf("doctor items = %d", len(platform.Doctor))
	}
}

// The guest needs Docker and KVM, not a particular distribution: any Linux
// passes, and a derivative (Mint, Manjaro, …) gets its parent's packages.
func TestAnyLinuxDistributionIsSupported(t *testing.T) {
	for _, tc := range []struct {
		osRelease, family string
	}{
		{"ID=arch\n", "arch"},
		{"ID=fedora\n", "fedora"},
		{"ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", "ubuntu"},
		{"ID=manjaro\nID_LIKE=arch\n", "arch"},
		{"ID=opensuse-tumbleweed\nID_LIKE=\"opensuse suse\"\n", ""},
	} {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "etc"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "etc", "os-release"), []byte(tc.osRelease), 0600); err != nil {
			t.Fatal(err)
		}
		p := DetectPlatform(root, PlatformProbes{CommandOutput: func(string, ...string) (string, error) { return "", errors.New("no docker") }})
		if !p.Supported || p.Family != tc.family {
			t.Errorf("%q: supported %v, family %q; want supported, family %q", tc.osRelease, p.Supported, p.Family, tc.family)
		}
		for _, item := range p.Doctor {
			if item.Name == "Operating system" && !item.OK {
				t.Errorf("%q: the operating system check failed: %+v", tc.osRelease, item)
			}
		}
	}
}

// Each check names its own package: the Compose check is not answered with
// the Docker engine's package, and KVM, which no package provides, names none.
func TestPackageForNamesThePackageOfEachCheck(t *testing.T) {
	for _, tc := range []struct {
		family, check, manager, pkg string
	}{
		{"ubuntu", "Docker", "apt", "docker.io"},
		{"ubuntu", "Docker Compose v2", "apt", "docker-compose-v2"},
		{"arch", "FreeRDP", "pacman", "freerdp"},
		{"fedora", "Docker", "dnf", "moby-engine"},
		{"ubuntu", "KVM", "", ""},
		{"", "Docker", "", ""},
	} {
		manager, pkg := PackageFor(tc.family, tc.check)
		if manager != tc.manager || pkg != tc.pkg {
			t.Errorf("PackageFor(%q, %q) = %q, %q; want %q, %q", tc.family, tc.check, manager, pkg, tc.manager, tc.pkg)
		}
	}
}

func TestDetectPlatformChecksComposeV2Output(t *testing.T) {
	for _, tc := range []struct {
		output string
		err    error
		want   bool
	}{{"v2.29.1", nil, true}, {"v1.29.2", nil, false}, {"", errors.New("missing plugin"), false}} {
		platform := DetectPlatform(t.TempDir(), PlatformProbes{CommandOutput: func(name string, args ...string) (string, error) {
			if name == "docker" && len(args) > 0 && args[0] == "version" {
				return "", errors.New("no daemon")
			}
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
	root := t.TempDir()
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

	t.Setenv("DOCKER_HOST", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	p := DetectPlatform(root, PlatformProbes{
		Exists:     func(string) bool { return true },
		Accessible: func(string) bool { return false },
		Executable: func(string) bool { return true },
		Filesystem: func(string) string { return "ext4" },
		CommandOutput: func(_ string, args ...string) (string, error) {
			if args[0] == "version" {
				return "", errors.New("permission denied while trying to connect to the Docker daemon socket")
			}
			return "v2.29.1", nil
		},
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
func TestAccessibleConnectsToASocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", path)
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
	if !accessible(path) {
		t.Fatal("a listening Unix socket should be accessible")
	}
}

// The FreeRDP check takes any client Connect can start, so a host with only
// the SDL client is not sent to install another.
func TestFreeRDPCheckTakesEveryClientConnectStarts(t *testing.T) {
	for _, client := range RDPClients {
		p := DetectPlatform(t.TempDir(), PlatformProbes{
			Executable:    func(name string) bool { return name == client },
			CommandOutput: func(string, ...string) (string, error) { return "", errors.New("no docker") },
		})
		if !p.FreeRDP.Available {
			t.Errorf("FreeRDP not found with only %s installed", client)
		}
	}
}

// The guest's Docker is whatever the docker command reaches: a rootless
// daemon, which has no socket in /var/run, counts as much as the system's.
func TestDockerIsWhatTheDockerCommandReaches(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	p := DetectPlatform(t.TempDir(), PlatformProbes{
		Exists:     func(string) bool { return false },
		Accessible: func(string) bool { return false },
		Executable: func(string) bool { return true },
		CommandOutput: func(_ string, args ...string) (string, error) {
			if args[0] == "version" {
				return "28.1.1\n", nil
			}
			return "v2.29.1", nil
		},
	})
	if !p.DockerSocket.Available || !p.DockerSocket.Accessible {
		t.Fatalf("a daemon the docker command reaches is not taken: %+v", p.DockerSocket)
	}
	for _, item := range p.Doctor {
		if item.Name == "Docker" && !item.OK {
			t.Errorf("the Docker check failed: %+v", item)
		}
	}
}

// When the command reaches no daemon, the socket it would use says why:
// DOCKER_HOST's, a rootless daemon's, else the system's.
func TestDockerSocketIsTheOneTheCommandUses(t *testing.T) {
	run := t.TempDir()
	rootless := filepath.Join(run, "docker.sock")
	exists := func(p string) bool { return p == rootless }
	for _, tc := range []struct {
		host, runtimeDir, want string
	}{
		{"unix:///run/user/1000/podman/podman.sock", run, "/run/user/1000/podman/podman.sock"},
		{"tcp://127.0.0.1:2375", run, ""},
		{"", run, rootless},
		{"", t.TempDir(), "/var/run/docker.sock"},
		{"", "", "/var/run/docker.sock"},
	} {
		if got := dockerSocket(tc.host, tc.runtimeDir, "/var/run/docker.sock", exists); got != tc.want {
			t.Errorf("dockerSocket(%q, %q) = %q, want %q", tc.host, tc.runtimeDir, got, tc.want)
		}
	}
}

func TestDockerWithoutItsSocketIsNotRunning(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	p := DetectPlatform(t.TempDir(), PlatformProbes{
		Exists:     func(string) bool { return false },
		Accessible: func(string) bool { return false },
		Executable: func(string) bool { return true },
		CommandOutput: func(_ string, args ...string) (string, error) {
			return "", errors.New("Cannot connect to the Docker daemon")
		},
	})
	for _, item := range p.Doctor {
		if item.Name == "Docker" && item.Reason != ReasonStopped {
			t.Errorf("Docker reason = %q, want %q", item.Reason, ReasonStopped)
		}
	}
}
