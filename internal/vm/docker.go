package vm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	dockerComposeFile = "compose.yaml"
	// dockerImage is pinned to a version and to that version's manifest
	// digest. The container gets /dev/kvm, /dev/net/tun and NET_ADMIN, so an
	// image that changed under its tag would run with those rights without
	// anyone choosing the new version; updating it is deliberate.
	dockerImage = "dockurr/windows:6.06@sha256:16fd7c39a3e48bb946d29c59dc6015942e5b900a1fa5d7e5552053bfc48ee2ea"
)

// Docker Compose passes PASSWORD to daemon container metadata. Docker access
// is root-equivalent; users must trust the daemon.

// CommandRunner is injectable so backend behavior can be exercised without Docker.
type CommandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}
type GuestSecretStore interface {
	Store(context.Context, string, []byte) error
	Load(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return stdout.String(), fmt.Errorf("%w: %s", err, msg)
		}
	}
	return stdout.String(), err
}

// DockerBackend manages one Dockur Windows container using Docker Compose v2.
type DockerBackend struct {
	Runner  CommandRunner
	Secrets GuestSecretStore
	Config  Config
	// Answers reports whether Remote Desktop answers at an address; it is
	// rdpAnswers unless a test puts another in.
	Answers     func(ctx context.Context, addr string) bool
	composePath string
	secretID    string
}

func NewDockerBackend(runner CommandRunner) *DockerBackend {
	if runner == nil {
		runner = execCommandRunner{}
	}
	return &DockerBackend{Runner: runner}
}

// SecretID is the Secret Service id of the guest password, empty until a
// project has been written or restored.
func (b *DockerBackend) SecretID() string { return b.secretID }

// Install writes the Compose project for cfg, replacing one that is already
// there: the project is generated from the configuration, so writing it
// again is how a changed memory, CPU count or disk size takes effect - and
// how the window's Apply does anything at all.
//
// The guest password is made only the first time and kept afterwards, since
// the installed Windows already has it. The disk is never touched.
func (b *DockerBackend) Install(ctx context.Context, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := safeOwnedDir(cfg.DataDir); err != nil {
		return vmError("install", ErrorInvalidConfig, err)
	}
	if err := markDataDir(cfg.DataDir); err != nil {
		return vmError("install", ErrorBackend, err)
	}
	composeDir := filepath.Join(configBase(), "inua", "vm", cfg.Name)
	if err := safeOwnedDir(composeDir); err != nil {
		return vmError("install", ErrorInvalidConfig, err)
	}
	composePath := filepath.Join(composeDir, dockerComposeFile)
	if err := rejectPathSymlinks(composePath); err != nil {
		return vmError("install", ErrorInvalidConfig, err)
	}
	if b.Secrets == nil {
		return vmError("install", ErrorUnavailable, errors.New("secure guest password storage is unavailable"))
	}
	secretID := "inua/windows/" + cfg.Name

	// The guest's password: keep the one it was installed with, and make one
	// only when there is none yet.
	password, err := b.Secrets.Load(ctx, secretID)
	if err != nil {
		password, err = newGuestPassword()
		if err != nil {
			return vmError("install", ErrorBackend, err)
		}
		if err := b.Secrets.Store(ctx, secretID, password); err != nil {
			clear(password)
			return vmError("install", ErrorUnavailable, err)
		}
	}
	defer clear(password)

	if cfg.ISOPath != "" && !ISOPresent(cfg.ISOPath) {
		return vmError("install", ErrorInvalidConfig, fmt.Errorf("the installer ISO %s cannot be read", cfg.ISOPath))
	}

	envPath := filepath.Join(composeDir, ".guest.env")
	if err := writeAtomic(envPath, []byte("PASSWORD="+string(password)+"\n"), 0600); err != nil {
		return vmError("install", ErrorBackend, err)
	}
	if err := writeCompose(composePath, cfg); err != nil {
		return vmError("install", ErrorBackend, err)
	}
	b.Config, b.composePath, b.secretID = cfg, composePath, secretID
	return nil
}

// writeCompose writes the Compose project for cfg at path.
//
// An installer ISO of the user's is bound read-only to /boot.iso, where
// Dockur installs from it instead of downloading Windows, and which it never
// writes. The bind is left out while the file is not there: once Windows is
// installed it boots from its disk, and a bind of a missing file would stop
// the container from starting at all.
func writeCompose(path string, cfg Config) error {
	volumes := "      - " + yamlQuote(cfg.DataDir+":/storage") + "\n"
	if cfg.ISOPath != "" && ISOPresent(cfg.ISOPath) {
		volumes += "      - type: bind\n        source: " + yamlQuote(cfg.ISOPath) + "\n        target: /boot.iso\n        read_only: true\n"
	}
	content := fmt.Sprintf("services:\n  windows:\n    image: %s\n    container_name: inua-%s\n    environment:\n      VERSION: \"11\"\n      USERNAME: \"%s\"\n      RAM_SIZE: \"%dG\"\n      CPU_CORES: \"%d\"\n      DISK_SIZE: \"%dG\"\n    env_file:\n      - %s\n    devices:\n      - /dev/kvm\n      - /dev/net/tun\n    cap_add:\n      - NET_ADMIN\n    ports:\n      - \"127.0.0.1:%d:3389/tcp\"\n      - \"127.0.0.1:%d:3389/udp\"\n      - \"127.0.0.1:8006:8006\"\n    volumes:\n%s    restart: unless-stopped\n    stop_grace_period: 2m\n", dockerImage, cfg.Name, GuestUser, cfg.MemoryMiB/1024, cfg.CPUs, cfg.DiskGiB, yamlQuote(filepath.Join(filepath.Dir(path), ".guest.env")), cfg.RDPPort, cfg.RDPPort, volumes)
	return writeAtomic(path, []byte(content), 0600)
}

// ISOPresent reports whether path is an installer ISO that can be read.
func ISOPresent(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

// Start brings the container up. It does not wait for Windows: on a first
// install that is tens of minutes of downloading and setup, and the caller
// needs to show progress and offer Stop meanwhile. Readiness is State's to
// report - it is what the running container's RDP port answers to.
func (b *DockerBackend) Start(ctx context.Context) error {
	if err := b.ensureInstalled(); err != nil {
		return vmError("start", ErrorConflict, err)
	}
	if b.Secrets == nil || b.secretID == "" {
		return vmError("start", ErrorUnavailable, errors.New("secure guest password storage is unavailable"))
	}
	password, err := b.Secrets.Load(ctx, b.secretID)
	if err != nil {
		return vmError("start", ErrorUnavailable, err)
	}
	defer clear(password)
	envPath := filepath.Join(filepath.Dir(b.composePath), ".guest.env")
	if err := writeAtomic(envPath, []byte("PASSWORD="+string(password)+"\n"), 0600); err != nil {
		return vmError("start", ErrorBackend, err)
	}
	// Written afresh, so the installer ISO is bound only while it is there.
	if err := writeCompose(b.composePath, b.Config); err != nil {
		return vmError("start", ErrorBackend, err)
	}
	if _, err := b.compose(ctx, "up", "-d"); err != nil {
		return vmError("start", ErrorBackend, err)
	}
	return nil
}

// State reports what the guest is doing.
//
// Running means Windows itself is up, which is not the same as the container
// being up: a running container whose guest is still installing or booting
// is StateStarting. The distinction is what tells the window whether to
// offer Connect or to keep showing the installation.
//
// Windows is up once its Remote Desktop answers. Dockur's log says "started
// successfully" as soon as the firmware boots from the disk or from the
// installation DVD, so on a first install it says so at the very start of
// Windows' setup; and a restarted container still carries the line of the
// boot before.
func (b *DockerBackend) State(ctx context.Context) (State, error) {
	if err := b.ensureInstalled(); err != nil {
		return StateUnknown, vmError("state", ErrorConflict, err)
	}
	out, err := b.compose(ctx, "ps", "--status", "running", "--services")
	if err != nil {
		return StateUnknown, vmError("state", ErrorBackend, err)
	}
	if !strings.Contains(out, "windows") {
		return StateStopped, nil
	}
	answers := b.Answers
	if answers == nil {
		answers = rdpAnswers
	}
	if answers(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(b.Config.RDPPort))) {
		return StateRunning, nil
	}
	return StateStarting, nil
}

// RecentOutput is the last useful line of the container's logs, for the
// window to show while Windows is installing.
func (b *DockerBackend) RecentOutput(ctx context.Context) string {
	logs, err := b.containerLogs(ctx, "12")
	if err != nil {
		return ""
	}
	return lastLogLine(logs)
}

func (b *DockerBackend) containerLogs(ctx context.Context, tail string) (string, error) {
	started, err := b.compose(ctx, "ps", "-q", "windows")
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(started)
	if id == "" {
		return "", nil
	}
	return b.Runner.Run(ctx, "docker", "logs", "--tail", tail, id)
}

func lastLogLine(s string) string {
	s = stripANSI(s)
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ParsePercent reads a 0–100 figure out of a log line, when it has one.
func ParsePercent(line string) float64 {
	best := -1.0
	for i := 0; i < len(line); i++ {
		if line[i] < '0' || line[i] > '9' {
			continue
		}
		n := 0
		j := i
		for j < len(line) && line[j] >= '0' && line[j] <= '9' {
			n = n*10 + int(line[j]-'0')
			j++
		}
		if j < len(line) && line[j] == '%' && n <= 100 {
			best = float64(n) / 100
		}
		i = j
	}
	if best < 0 {
		return 0
	}
	return best
}

func (b *DockerBackend) Stop(ctx context.Context) error {
	if err := b.ensureInstalled(); err != nil {
		return vmError("stop", ErrorConflict, err)
	}
	if _, err := b.compose(ctx, "stop", "-t", "120"); err != nil {
		return vmError("stop", ErrorBackend, err)
	}
	return nil
}

func (b *DockerBackend) Connect(ctx context.Context) (Connection, error) {
	state, err := b.State(ctx)
	if err != nil {
		return Connection{}, err
	}
	if state != StateRunning {
		return Connection{}, vmError("connect", ErrorUnavailable, errors.New("Windows guest is not RDP-ready"))
	}
	return Connection{Address: "127.0.0.1", Port: b.Config.RDPPort}, nil
}

// ConsoleURL is the container's web console, on the loopback interface only.
func (b *DockerBackend) ConsoleURL() (string, error) {
	if err := b.ensureInstalled(); err != nil {
		return "", vmError("console", ErrorConflict, err)
	}
	return "http://127.0.0.1:8006/", nil
}

// Console returns the container's web console address, published on the
// loopback interface by the Compose project.
func (b *DockerBackend) Console(context.Context) (string, error) {
	return b.ConsoleURL()
}

// Remove tears down the managed container and project. The guest disk and
// its password stay: deleting the user's Windows install is a separate,
// explicitly confirmed act (RemoveData), and that Windows still uses this
// password.
func (b *DockerBackend) Remove(ctx context.Context) error {
	if err := b.ensureInstalled(); err != nil {
		return vmError("remove", ErrorConflict, err)
	}
	if _, err := b.compose(ctx, "down"); err != nil {
		return vmError("remove", ErrorBackend, err)
	}
	if err := os.Remove(b.composePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return vmError("remove", ErrorBackend, err)
	}
	if err := os.Remove(filepath.Join(filepath.Dir(b.composePath), ".guest.env")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return vmError("remove", ErrorBackend, err)
	}
	// The guest password stays: Remove keeps the disk, and that Windows
	// still uses this password. A later install reuses it.
	return nil
}

// Down stops the container and removes it, leaving the project files and
// the disk. RemoveData needs the disk unmounted first.
func (b *DockerBackend) Down(ctx context.Context) error {
	if err := b.ensureInstalled(); err != nil {
		return nil
	}
	if _, err := b.compose(ctx, "down"); err != nil {
		return vmError("down", ErrorBackend, err)
	}
	return nil
}

// dataDirMarker is the file Install leaves in a VM data directory, so that
// RemoveData can tell a folder inua made from one a configuration names.
const dataDirMarker = ".inua-vm"

// markDataDir leaves dataDirMarker in dir, once.
func markDataDir(dir string) error {
	path := filepath.Join(dir, dataDirMarker)
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	return writeAtomic(path, []byte("This folder holds a Windows guest of inua-vm; deleting its disk deletes the folder.\n"), 0600)
}

// RemoveData deletes the guest disk directory. It only ever removes the
// configured VM data directory itself, after checking that it is the kind of
// path Inua owns: absolute, not a filesystem root, not a symlink, and either
// the default place or a folder Install marked - a configuration naming a
// folder of the user's must not turn Delete disk into deleting that folder.
func (b *DockerBackend) RemoveData(ctx context.Context) error {
	if err := b.Down(ctx); err != nil {
		return err
	}
	dir := b.Config.DataDir
	if err := validateOwnedDataDir(dir); err != nil {
		return vmError("remove-data", ErrorInvalidConfig, err)
	}
	if err := rejectPathSymlinks(dir); err != nil {
		return vmError("remove-data", ErrorInvalidConfig, err)
	}
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return vmError("remove-data", ErrorBackend, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, dataDirMarker)); err != nil && dir != DefaultDataDir() {
		return vmError("remove-data", ErrorInvalidConfig, fmt.Errorf("%s is not a folder inua made for the guest", dir))
	}
	err := os.RemoveAll(dir)
	if err == nil || !errors.Is(err, os.ErrPermission) {
		return err
	}
	// Dockur runs as root in its container, so what it leaves in folders of
	// its own (an unfinished download) is root's, which this user cannot
	// delete. A container of the same image, which is here once one has run,
	// has the rights: it empties the folder, and the folder itself is ours.
	if b.Runner == nil {
		b.Runner = execCommandRunner{}
	}
	if _, err := b.Runner.Run(ctx, "docker", "run", "--rm", "--pull", "never", "--network", "none",
		"-v", dir+":/storage", "--entrypoint", "find", dockerImage, "/storage", "-mindepth", "1", "-delete"); err != nil {
		return vmError("remove-data", ErrorBackend, err)
	}
	return os.RemoveAll(dir)
}

// validateOwnedDataDir rejects anything that is not a plainly-owned VM data
// directory, so a misconfigured path cannot turn RemoveData into a wide rm.
func validateOwnedDataDir(dir string) error {
	clean := filepath.Clean(dir)
	switch {
	case dir == "" || !filepath.IsAbs(clean):
		return errors.New("VM data directory must be absolute")
	case clean == string(filepath.Separator):
		return errors.New("refusing to remove the filesystem root")
	case clean == filepath.Clean(os.Getenv("HOME")) && os.Getenv("HOME") != "":
		return errors.New("refusing to remove the home directory")
	case dir != clean:
		return errors.New("VM data directory must be a clean path")
	}
	return nil
}

func (b *DockerBackend) compose(ctx context.Context, args ...string) (string, error) {
	if b.Runner == nil {
		b.Runner = execCommandRunner{}
	}
	// Pin the project name: Docker Compose otherwise derives it from the
	// directory the file sits in, so any other Compose project whose folder
	// is named "windows" and whose service is "windows" would be treated as
	// this one - and `up -d` would tear its container down.
	argv := []string{"compose", "-p", composeProject(b.Config.Name), "-f", b.composePath}
	argv = append(argv, args...)
	return b.Runner.Run(ctx, "docker", argv...)
}

// composeProject is the Compose project name for a VM, namespaced so it can
// never collide with a user's own Compose files.
func composeProject(name string) string {
	return "inua-vm-" + name
}

// SetInstalled points the backend at cfg's Compose project, and reports
// whether that project is actually there: a configuration on file whose
// project was removed (or never written) is not an installed guest.
func (b *DockerBackend) SetInstalled(cfg Config) error {
	b.Config = cfg
	if cfg.Name == "" {
		b.composePath = ""
		b.secretID = ""
		return errors.New("no VM configured")
	}
	dir := filepath.Join(configBase(), "inua", "vm", cfg.Name)
	b.composePath = filepath.Join(dir, dockerComposeFile)
	b.secretID = "inua/windows/" + cfg.Name
	return b.ensureInstalled()
}
func (b *DockerBackend) ensureInstalled() error {
	if b.composePath == "" || b.Config.Name == "" {
		return errors.New("VM is not installed by this backend instance")
	}
	if err := rejectPathSymlinks(b.composePath); err != nil {
		return err
	}
	fi, err := os.Stat(b.composePath)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("compose path is not a regular file")
	}
	return nil
}

func safeOwnedDir(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Clean(path) == string(filepath.Separator) {
		return errors.New("unsafe directory path")
	}
	if err := rejectPathSymlinks(path); err != nil {
		return err
	}
	return os.MkdirAll(path, 0700)
}

func rejectPathSymlinks(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	vol := filepath.VolumeName(abs)
	cur := vol + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, cur), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path component rejected: %s", cur)
		}
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".compose-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func yamlQuote(s string) string {
	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n").Replace(s) + "\""
}

func configBase() string {
	if p := os.Getenv("XDG_CONFIG_HOME"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func vmError(op string, kind ErrorKind, err error) error {
	return &Error{Op: op, Kind: kind, Err: err}
}

func newGuestPassword() ([]byte, error) {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, err
	}
	password := make([]byte, hex.EncodedLen(len(entropy)))
	hex.Encode(password, entropy[:])
	return password, nil
}
