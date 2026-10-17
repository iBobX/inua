package vm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// fakeRunner stands in for Docker so the lifecycle runs without a daemon.
type fakeRunner struct {
	calls []string
	fail  map[string]error
	out   map[string]string
	// did, when set, does what the command would (a container deleting
	// files, say).
	did func(call string)
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if f.did != nil {
		f.did(call)
	}
	if err, ok := f.fail[call]; ok {
		return "", err
	}
	if f.out != nil {
		if s, ok := f.out[call]; ok {
			return s, nil
		}
		for prefix, s := range f.out {
			if strings.HasPrefix(call, prefix) {
				return s, nil
			}
		}
	}
	return "", nil
}

type fakeSecrets struct{ items map[string][]byte }

func (f *fakeSecrets) Store(_ context.Context, id string, v []byte) error {
	f.items[id] = append([]byte(nil), v...)
	return nil
}
func (f *fakeSecrets) Load(_ context.Context, id string) ([]byte, error) {
	v, ok := f.items[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}
func (f *fakeSecrets) Delete(_ context.Context, id string) error {
	delete(f.items, id)
	return nil
}

// newTestBackend puts the backend's configuration under a temporary root so
// nothing touches the user's real XDG directories.
func newTestBackend(t *testing.T) (*DockerBackend, Config, *fakeRunner, *fakeSecrets) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	cfg := DefaultConfig()
	cfg.DataDir = filepath.Join(root, "data", "inua", "vm", "windows")
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{fail: map[string]error{}}
	secrets := &fakeSecrets{items: map[string][]byte{}}
	b := NewDockerBackend(runner)
	b.Secrets = secrets
	b.Answers = func(context.Context, string) bool { return false }
	return b, cfg, runner, secrets
}

func TestInstallWritesComposeAndEnvWithoutLeakingSecret(t *testing.T) {
	b, cfg, _, secrets := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "inua", "vm", cfg.Name)
	compose, err := os.ReadFile(filepath.Join(dir, dockerComposeFile))
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	envPath := filepath.Join(dir, ".guest.env")
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("env file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("env file mode = %o, want 0600", perm)
	}
	password, err := secrets.Load(ctx, "inua/windows/"+cfg.Name)
	if err != nil {
		t.Fatalf("stored password: %v", err)
	}
	if len(password) < 32 {
		t.Errorf("generated password is too short: %d", len(password))
	}
	// The password belongs in the 0600 env file alone, never in the project.
	if strings.Contains(string(compose), string(password)) {
		t.Error("the guest password leaked into the Compose project")
	}
	// Ports must stay on the loopback interface.
	if !strings.Contains(string(compose), "127.0.0.1:") {
		t.Error("Compose does not publish ports on the loopback interface")
	}
	// The image is pinned: the container runs with /dev/kvm, /dev/net/tun and
	// NET_ADMIN, so a tag that moved would change what runs with those rights.
	if !strings.Contains(string(compose), dockerImage) || !strings.Contains(dockerImage, "@sha256:") {
		t.Errorf("the image is not pinned by version and digest: %q", dockerImage)
	}
	if strings.Contains(string(compose), "\n      - \"8006:8006\"") {
		t.Error("the web console is published on every interface")
	}
	if !strings.Contains(string(compose), `USERNAME: "`+GuestUser+`"`) {
		t.Errorf("compose does not set the guest user %q:\n%s", GuestUser, compose)
	}
}

// Applying a configuration is what the window's Apply does, and it has to
// work on a project that already exists - that is how a changed memory, CPU
// count or disk size takes effect. The guest's password, though, is the one
// Windows was installed with and must survive.
func TestInstallRewritesTheProjectAndKeepsThePassword(t *testing.T) {
	b, cfg, _, secrets := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("first install: %v", err)
	}
	first, err := secrets.Load(ctx, "inua/windows/"+cfg.Name)
	if err != nil {
		t.Fatalf("stored password: %v", err)
	}

	bigger := cfg
	bigger.MemoryMiB = cfg.MemoryMiB * 2
	bigger.CPUs = cfg.CPUs + 2
	if err := b.Install(ctx, bigger); err != nil {
		t.Fatalf("applying a new configuration: %v", err)
	}

	compose, err := os.ReadFile(b.composePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), fmt.Sprintf("RAM_SIZE: \"%dG\"", bigger.MemoryMiB/1024)) {
		t.Errorf("the new memory is not in the project:\n%s", compose)
	}
	if !strings.Contains(string(compose), fmt.Sprintf("CPU_CORES: \"%d\"", bigger.CPUs)) {
		t.Errorf("the new CPU count is not in the project:\n%s", compose)
	}

	again, err := secrets.Load(ctx, "inua/windows/"+cfg.Name)
	if err != nil {
		t.Fatalf("password after the rewrite: %v", err)
	}
	if !bytes.Equal(first, again) {
		t.Error("rewriting the project changed the guest's password")
	}
}

func TestInstallRefusesWithoutSecretStore(t *testing.T) {
	b, cfg, _, _ := newTestBackend(t)
	b.Secrets = nil
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err == nil {
		t.Fatal("install succeeded without a secure password store")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "inua", "vm", cfg.Name, dockerComposeFile)); !os.IsNotExist(err) {
		t.Error("a refused install left a Compose project behind")
	}
}

func TestRemoveDeletesProjectAndKeepsThePassword(t *testing.T) {
	b, cfg, _, secrets := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	first, err := secrets.Load(ctx, "inua/windows/"+cfg.Name)
	if err != nil {
		t.Fatalf("stored password: %v", err)
	}
	if err := b.Remove(ctx); err != nil {
		t.Fatalf("remove: %v", err)
	}
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "inua", "vm", cfg.Name)
	for _, name := range []string{dockerComposeFile, ".guest.env"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s survived removal", name)
		}
	}
	again, err := secrets.Load(ctx, "inua/windows/"+cfg.Name)
	if err != nil {
		t.Fatalf("password after remove: %v", err)
	}
	if !bytes.Equal(first, again) {
		t.Error("remove dropped the password of the disk it kept")
	}
}

func TestConsoleURLIsLoopbackOnly(t *testing.T) {
	b, cfg, _, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	url, err := b.ConsoleURL()
	if err != nil {
		t.Fatalf("console url: %v", err)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Errorf("console url is not loopback: %q", url)
	}
}

func TestSetInstalledRestoresManagedLocations(t *testing.T) {
	b, cfg, _, _ := newTestBackend(t)
	b.SetInstalled(cfg)
	if b.composePath != filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "inua", "vm", cfg.Name, dockerComposeFile) {
		t.Errorf("compose path not restored: %q", b.composePath)
	}
	if b.secretID != "inua/windows/"+cfg.Name {
		t.Errorf("secret id not restored: %q", b.secretID)
	}
	if err := b.ensureInstalled(); err == nil {
		t.Error("ensureInstalled accepted a project that was never written")
	}
}

// The Compose project must be valid YAML: a quoted volume source followed by
// a bare ": /storage" made Docker refuse the project outright.
func TestInstallWritesParsableCompose(t *testing.T) {
	b, cfg, _, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	data, err := os.ReadFile(b.composePath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("compose is not valid YAML: %v\n%s", err, data)
	}
	services, _ := doc["services"].(map[string]any)
	if services == nil {
		t.Fatalf("compose has no services: %s", data)
	}
	if _, ok := services["windows"]; !ok {
		t.Fatalf("compose has no windows service: %s", data)
	}
}

// composeVolumes are the windows service's volumes as Compose reads them.
func composeVolumes(t *testing.T, path string) []any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]struct {
			Volumes []any `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("compose is not valid YAML: %v\n%s", err, data)
	}
	return doc.Services["windows"].Volumes
}

// isoBind is the /boot.iso bind among volumes, nil when there is none.
func isoBind(volumes []any) map[string]any {
	for _, v := range volumes {
		if m, ok := v.(map[string]any); ok && m["target"] == "/boot.iso" {
			return m
		}
	}
	return nil
}

// An ISO of the user's is what Dockur installs from, bound read-only so it
// is never written; with none, nothing is bound there and Dockur downloads.
func TestInstallBindsTheInstallerISOReadOnly(t *testing.T) {
	b, cfg, _, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	if bind := isoBind(composeVolumes(t, b.composePath)); bind != nil {
		t.Fatalf("an ISO is bound with none chosen: %v", bind)
	}

	// A colon in the path would split a short volume entry.
	cfg.ISOPath = filepath.Join(t.TempDir(), "Win10: 22H2.iso")
	if err := os.WriteFile(cfg.ISOPath, []byte("CD001"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install with an ISO: %v", err)
	}
	bind := isoBind(composeVolumes(t, b.composePath))
	if bind == nil || bind["type"] != "bind" || bind["source"] != cfg.ISOPath || bind["read_only"] != true {
		t.Fatalf("the ISO is not bound read-only to /boot.iso: %v", bind)
	}
}

func TestInstallRefusesAnISOThatIsNotThere(t *testing.T) {
	b, cfg, _, _ := newTestBackend(t)
	cfg.ISOPath = filepath.Join(t.TempDir(), "missing.iso")
	if err := b.Install(context.Background(), cfg); err == nil {
		t.Fatal("Install accepted an ISO that is not there")
	}
}

// Once Windows is installed the ISO may go: a start then leaves its bind
// out, since a bind of a missing file would stop the container starting.
func TestStartLeavesOutAnISOThatIsGone(t *testing.T) {
	b, cfg, _, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg.ISOPath = filepath.Join(t.TempDir(), "win.iso")
	if err := os.WriteFile(cfg.ISOPath, []byte("CD001"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := os.Remove(cfg.ISOPath); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if bind := isoBind(composeVolumes(t, b.composePath)); bind != nil {
		t.Fatalf("a missing ISO is still bound: %v", bind)
	}
}

func TestRemoveKeepsDataUntilExplicitlyDeleted(t *testing.T) {
	b, cfg, _, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	// The guest image stands in for a real disk.
	disk := filepath.Join(cfg.DataDir, "data.img")
	if err := os.WriteFile(disk, []byte("windows"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.Remove(ctx); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(disk); err != nil {
		t.Fatalf("remove deleted the guest disk: %v", err)
	}
	if err := b.RemoveData(ctx); err != nil {
		t.Fatalf("remove-data: %v", err)
	}
	if _, err := os.Stat(cfg.DataDir); !os.IsNotExist(err) {
		t.Errorf("the VM data directory survived RemoveData")
	}
}

func TestRemoveDataRefusesDangerousPaths(t *testing.T) {
	for name, dir := range map[string]string{
		"root":     "/",
		"home":     os.Getenv("HOME"),
		"empty":    "",
		"relative": "vm/windows",
	} {
		t.Run(name, func(t *testing.T) {
			b, cfg, _, _ := newTestBackend(t)
			b.Config = cfg
			cfg.DataDir = dir
			b.Config = cfg
			if err := b.RemoveData(context.Background()); err == nil {
				t.Fatalf("RemoveData accepted %q", dir)
			}
		})
	}
}

// Compose derives its project name from the compose file's folder. Two
// projects whose folder is "windows" and whose service is "windows" would be
// the same project to Docker, so `up -d` would tear down the other one's
// container. The backend must pin a namespaced project name instead.
func TestComposePinsNamespacedProject(t *testing.T) {
	b, cfg, runner, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	// State reads through compose, without waiting for RDP readiness.
	if _, err := b.State(ctx); err != nil {
		t.Fatalf("state: %v", err)
	}
	var sawProject bool
	for _, call := range runner.calls {
		if !strings.HasPrefix(call, "docker compose ") {
			continue
		}
		if !strings.Contains(call, "-p inua-vm-windows") {
			t.Errorf("compose call does not pin the project name: %s", call)
			continue
		}
		sawProject = true
	}
	if !sawProject {
		t.Fatalf("no compose call was made: %v", runner.calls)
	}
}

func TestComposeProjectNamesAreNamespaced(t *testing.T) {
	if got := composeProject("windows"); got != "inua-vm-windows" {
		t.Errorf("composeProject = %q", got)
	}
	if composeProject("windows") == "windows" {
		t.Error("the project name is the bare VM name, which collides with a user's own")
	}
}

func TestStateReportsStartingUntilGuestReady(t *testing.T) {
	b, cfg, runner, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	runner.out = map[string]string{
		"docker compose": "windows\n",
		"docker logs":    "Downloading Windows 11... 12%\n",
	}
	state, err := b.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state != StateStarting {
		t.Fatalf("state = %q, want starting", state)
	}
}

// Dockur says "started successfully" once the firmware boots, the
// installation DVD included: on a first install that is the start of
// Windows' setup, so the line alone must not make the guest running.
func TestStateIsStartingWhileOnlyTheLogSaysStarted(t *testing.T) {
	b, cfg, runner, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	runner.out = map[string]string{
		"docker compose": "windows\n",
		"docker logs":    "Windows started successfully, visit http://127.0.0.1:8006/\n",
	}
	state, err := b.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state != StateStarting {
		t.Fatalf("state = %q, want starting", state)
	}
}

func TestStateIsRunningOnceRemoteDesktopAnswers(t *testing.T) {
	b, cfg, runner, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg.RDPPort = 13389
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	runner.out = map[string]string{"docker compose": "windows\n"}
	var asked string
	b.Answers = func(_ context.Context, addr string) bool { asked = addr; return true }
	state, err := b.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state != StateRunning {
		t.Fatalf("state = %q, want running", state)
	}
	if asked != "127.0.0.1:13389" {
		t.Errorf("asked %q, want the guest's port on the loopback interface", asked)
	}
}

// rdpServer listens on the loopback interface and answers each connection
// with reply, the way a server behind a port forward would.
func rdpServer(t *testing.T, reply []byte) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				request := make([]byte, len(rdpRequest))
				if _, err := io.ReadFull(c, request); err != nil {
					return
				}
				c.Write(reply)
			}()
		}
	}()
	return l.Addr().String()
}

func TestRDPAnswersOnlyToAConnectionConfirm(t *testing.T) {
	confirm := []byte{0x03, 0x00, 0x00, 0x13, 0x0e, 0xd0, 0x00, 0x00, 0x12, 0x34, 0x00,
		0x02, 0x00, 0x08, 0x00, 0x02, 0x00, 0x00, 0x00}
	for _, tc := range []struct {
		name  string
		reply []byte
		want  bool
	}{
		{"a Remote Desktop server", confirm, true},
		// Docker's port forward accepts the connection and closes it when
		// nothing listens behind it yet.
		{"a forward with nothing behind it", nil, false},
		{"another service", []byte("HTTP/1.1 400 Bad Request\r\n\r\n"), false},
	} {
		addr := rdpServer(t, tc.reply)
		if got := rdpAnswers(context.Background(), addr); got != tc.want {
			t.Errorf("%s: rdpAnswers = %v, want %v", tc.name, got, tc.want)
		}
	}
	// Nothing listening at all.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	if rdpAnswers(context.Background(), addr) {
		t.Error("rdpAnswers with nothing listening")
	}
}

func TestRecentOutputIsLastLogLine(t *testing.T) {
	b, cfg, runner, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	runner.out = map[string]string{
		"docker compose": "abc123\n",
		"docker logs":    "\x1b[32mDownloading\x1b[0m\nExtracting 45%\n",
	}
	got := b.RecentOutput(ctx)
	if got != "Extracting 45%" {
		t.Fatalf("RecentOutput = %q", got)
	}
	if p := ParsePercent(got); p != 0.45 {
		t.Fatalf("ParsePercent = %v", p)
	}
}

func TestRemoveDataUnmountsThenDeletesTheDisk(t *testing.T) {
	b, cfg, runner, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	disk := filepath.Join(cfg.DataDir, "data.img")
	if err := os.WriteFile(disk, []byte("windows"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.RemoveData(ctx); err != nil {
		t.Fatalf("remove-data: %v", err)
	}
	if _, err := os.Stat(cfg.DataDir); !os.IsNotExist(err) {
		t.Errorf("the VM data directory survived RemoveData")
	}
	var down bool
	for _, call := range runner.calls {
		if strings.Contains(call, " compose ") && strings.HasSuffix(call, " down") {
			down = true
		}
	}
	if !down {
		t.Error("RemoveData did not bring the container down before deleting the disk")
	}
}

// Delete disk deletes a folder, so it must be one inua made: a
// configuration naming a folder of the user's is refused, and the folder
// stays. One Install marked is deleted wherever it is.
func TestRemoveDataDeletesOnlyAFolderInuaMade(t *testing.T) {
	b, cfg, _, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg.DataDir = filepath.Join(t.TempDir(), "Documents")
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	letter := filepath.Join(cfg.DataDir, "letter.odt")
	if err := os.WriteFile(letter, []byte("dear"), 0600); err != nil {
		t.Fatal(err)
	}
	b.Config = cfg
	if err := b.RemoveData(ctx); err == nil {
		t.Fatal("RemoveData deleted a folder inua did not make")
	}
	if _, err := os.Stat(letter); err != nil {
		t.Fatalf("the user's file is gone: %v", err)
	}

	cfg.DataDir = filepath.Join(t.TempDir(), "vm")
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := b.RemoveData(ctx); err != nil {
		t.Fatalf("remove-data: %v", err)
	}
	if _, err := os.Stat(cfg.DataDir); !os.IsNotExist(err) {
		t.Error("the folder Install made survived RemoveData")
	}
}

// Dockur runs as root, so an unfinished download leaves folders this user
// cannot empty; a container of the image deletes them instead.
func TestRemoveDataDeletesWhatTheContainerLeftAsRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may delete anything")
	}
	b, cfg, runner, _ := newTestBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Install(ctx, cfg); err != nil {
		t.Fatalf("install: %v", err)
	}
	tmp := filepath.Join(cfg.DataDir, "tmp")
	if err := os.MkdirAll(tmp, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "win11x64.iso"), []byte("CD001"), 0600); err != nil {
		t.Fatal(err)
	}
	// Not this user's to empty, as a folder of root's would be.
	if err := os.Chmod(tmp, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(tmp, 0700) })
	var container string
	runner.did = func(call string) {
		if strings.HasPrefix(call, "docker run ") {
			container = call
			os.Chmod(tmp, 0700)
			os.RemoveAll(tmp)
		}
	}
	if err := b.RemoveData(ctx); err != nil {
		t.Fatalf("remove-data: %v", err)
	}
	if _, err := os.Stat(cfg.DataDir); !os.IsNotExist(err) {
		t.Error("the VM data directory survived RemoveData")
	}
	for _, want := range []string{"--rm", "--network none", cfg.DataDir + ":/storage", dockerImage, "/storage -mindepth 1 -delete"} {
		if !strings.Contains(container, want) {
			t.Errorf("the container %q lacks %q", container, want)
		}
	}
}

func TestRemoveDataDeletesLeftoverDiskWithoutProject(t *testing.T) {
	b, cfg, _, _ := newTestBackend(t)
	disk := filepath.Join(cfg.DataDir, "data.img")
	if err := os.WriteFile(disk, []byte("windows"), 0600); err != nil {
		t.Fatal(err)
	}
	b.Config = cfg
	if err := b.RemoveData(context.Background()); err != nil {
		t.Fatalf("remove leftover disk: %v", err)
	}
	if _, err := os.Stat(cfg.DataDir); !os.IsNotExist(err) {
		t.Errorf("leftover disk survived")
	}
}
