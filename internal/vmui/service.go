package vmui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/vm"
)

// vmService owns the Docker backend, the persisted configuration and the
// operation lock.
//
// Two things are true of a guest, and they are not the same: a project may
// be configured (the Compose file and password exist) while Windows is still
// installing, and only later does the guest become usable. The distinction
// decides which page the window shows and which actions make sense, so both
// are tracked.
type vmService struct {
	mu           sync.Mutex
	backend      *vm.DockerBackend
	config       vm.Config
	host         vm.Host
	platform     vm.Platform
	hasProject   bool // the Compose project and password exist
	installed    bool // Windows has come up at least once
	busy         bool
	err          error
	state        vm.State
	stateErr     error
	progressText string
	secretConn   *dbus.Conn // the Secret Service session, held for the app's life
}

// vmSecretStore keeps the guest password in the Secret Service under inua's
// own attributes; the Compose environment file is written from it at start.
type vmSecretStore struct{ client *keyring.Client }

func vmSecretAttrs(id string) map[string]string {
	return map[string]string{"application": "inua", "kind": "windows-vm", "id": id}
}
func (s vmSecretStore) Store(ctx context.Context, id string, value []byte) error {
	return s.client.Store(ctx, "Inua Windows VM", vmSecretAttrs(id), value)
}
func (s vmSecretStore) Load(ctx context.Context, id string) ([]byte, error) {
	v, ok, err := s.client.Lookup(ctx, vmSecretAttrs(id))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("the Windows VM password is missing from the Secret Service")
	}
	return v, nil
}
func (s vmSecretStore) Delete(_ context.Context, id string) error {
	return s.client.Delete(vmSecretAttrs(id))
}

// newService wires the Docker backend, the host's resources and the Secret
// Service, then reads what is on disk. It touches no GTK: the command line
// uses it for --start and --stop, the window for everything.
func newService() *vmService {
	s := &vmService{host: vm.DetectHost(""), platform: vm.DetectPlatform("", vm.PlatformProbes{})}
	s.config = vm.DefaultConfig()
	s.backend = vm.NewDockerBackend(nil)
	// The Secret Service session must outlive this call: closing the bus
	// connection here would break every later password read.
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		if client, err := keyring.Connect(conn, keyring.AlgDH); err == nil {
			s.secretConn = conn
			s.backend.Secrets = vmSecretStore{client: client}
		} else {
			_ = conn.Close()
		}
	}
	s.load()
	return s
}

// close drops the Secret Service session. The window keeps it for the app's
// life; a run from the command line has nobody else to do it.
func (s *vmService) close() {
	if s.secretConn != nil {
		_ = s.secretConn.Close()
		s.secretConn = nil
	}
}

func logErr(what string, err error) {
	if err != nil {
		log.Printf("%s: %v", what, err)
	}
}

func (s *vmService) load() {
	data, err := os.ReadFile(vm.ConfigPath())
	if err != nil {
		return
	}
	saved, err := vm.ParsePersisted(data)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.config = saved.Config
	// The project exists when its Compose file does; the flag in the file
	// only says whether Windows had come up, which a removal resets.
	s.hasProject = s.backend.SetInstalled(saved.Config) == nil
	// A saved "installed" flag is only true if the guest disk is still
	// there: a compose project that never got a Windows image is still
	// installing, not a stopped guest.
	s.installed = saved.Installed && vm.GuestDiskPresent(saved.Config.DataDir)
	corrected := saved.Installed && !s.installed
	s.mu.Unlock()
	if corrected {
		if err := s.save(false); err != nil {
			logErr("vm state", err)
		}
	}
}

// snapshot is non-blocking: it reports the state last observed by refresh,
// because its callers run on the GTK main thread. Reading Docker there (a
// subprocess, up to seconds) would stall the whole shell.
func (s *vmService) snapshot() VMSnapshot {
	s.mu.Lock()
	cfg, hasProject, installed := s.config, s.hasProject, s.installed
	busy, oldErr, state, stateErr := s.busy, s.err, s.state, s.stateErr
	progressText := s.progressText
	platform, host := s.platform, s.host
	s.mu.Unlock()
	if !hasProject {
		state = vm.StateStopped
	}
	// The host checks, one line each, so the window can show every check and
	// mark the ones that fail.
	checks := make([]vm.DoctorItem, 0, len(platform.Doctor)+1)
	checks = append(checks, platform.Doctor...)
	if s.backend.Secrets == nil {
		checks = append(checks, vm.DoctorItem{
			Name:   "Secret Service",
			OK:     false,
			Reason: vm.ReasonMissing,
		})
	}
	available := platform.Supported && platform.KVM.Available && platform.KVM.Accessible &&
		platform.DockerSocket.Available && platform.DockerSocket.Accessible &&
		platform.ComposeV2.Available && platform.FreeRDP.Available &&
		platform.TUN.Available && platform.TUN.Accessible && s.backend.Secrets != nil
	err := stateErr
	if err == nil {
		err = oldErr
	}
	progress := vm.ParsePercent(progressText)
	return VMSnapshot{Config: cfg, State: state, Checks: checks, Busy: busy,
		Available: available, HasProject: hasProject, Installed: installed,
		HasDisk: vm.GuestDataPresent(cfg.DataDir), Progress: progress, ProgressText: progressText,
		Recommended: vm.RecommendProfile(host), Host: host,
		Family: platform.Family, Err: err}
}

// Refresh reads Docker off the GTK thread and returns a snapshot the window
// can paint. It is also where "Windows is installed" is decided: the guest
// is taken to be installed once its Remote Desktop has answered, so a later
// stop does not send the window back to "still installing".
func (s *vmService) Refresh() VMSnapshot {
	// Re-read the host while the window is open: the user may have installed
	// Docker or joined a group since the shell started, and the prereq page
	// has to notice that without a restart.
	host := vm.DetectHost("")
	platform := vm.DetectPlatform("", vm.PlatformProbes{})
	s.mu.Lock()
	s.host, s.platform = host, platform
	s.mu.Unlock()
	s.sync()
	return s.snapshot()
}

func (s *vmService) refresh() { go s.sync() }

func (s *vmService) sync() {
	s.mu.Lock()
	hasProject := s.hasProject
	dataDir := s.config.DataDir
	s.mu.Unlock()
	var state vm.State
	var err error
	var progress string
	if hasProject {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		state, err = s.backend.State(ctx)
		if err == nil {
			progress = s.backend.RecentOutput(ctx)
		}
		cancel()
	} else {
		state = vm.StateStopped
	}
	disk := vm.GuestDiskPresent(dataDir)
	s.mu.Lock()
	if hasProject {
		s.state, s.stateErr, s.progressText = state, err, progress
	} else {
		s.state, s.stateErr, s.progressText = vm.StateStopped, nil, ""
	}
	if s.installed && !disk {
		s.installed = false
	}
	firstReady := err == nil && state == vm.StateRunning && disk && !s.installed
	if firstReady {
		s.installed = true
	}
	s.mu.Unlock()
	if firstReady {
		if err := s.save(true); err != nil {
			logErr("vm state", err)
		}
	}
}

// Snapshot is the window's read of the guest; it does not block.
func (s *vmService) Snapshot() VMSnapshot { return s.snapshot() }

// Fix applies a check's remedy. A privileged one goes through pkexec, so the
// shell's own polkit dialog takes the password; this runs on a goroutine,
// because that dialog is on the main thread and this call waits for it.
//
// It re-reads the platform afterwards, so the window shows the new state
// rather than the one it was looking at.
func (s *vmService) Fix(r vm.Remedy) (string, bool, error) {
	done, relogin, err := vm.ApplyRemedy(r)
	s.mu.Lock()
	s.platform = vm.DetectPlatform("", vm.PlatformProbes{})
	s.mu.Unlock()
	s.sync()
	return done, relogin, err
}

// Configure writes the Compose project for c and remembers the
// configuration. It does not start anything: installing Windows is its own,
// visible step.
//
// Writing the project again is what applying a changed memory, CPU count or
// disk size does, so this runs for a guest that already has one. It does not
// make the guest "installed" - only Windows coming up does.
func (s *vmService) Configure(c vm.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := s.begin(); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(opErr) }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	if err := s.backend.Install(ctx, c); err != nil {
		opErr = err
		return err
	}
	s.mu.Lock()
	s.config, s.hasProject = c, true
	s.mu.Unlock()
	if err := s.save(s.installedNow()); err != nil {
		opErr = err
		return err
	}
	return nil
}

// installedNow reports whether Windows has been seen up, for persisting.
func (s *vmService) installedNow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installed
}

// Install writes the project from c and starts the container, which
// downloads the image and installs Windows. It returns once the start has
// been asked for; the window follows the rest on the provisioning page.
func (s *vmService) Install(c vm.Config) error {
	if err := s.Configure(c); err != nil {
		return err
	}
	return s.Start()
}

// Start brings the container up. It does not wait for Windows: the window
// follows the guest's state, and Windows' own setup on a first install takes
// tens of minutes after the container is up.
func (s *vmService) Start() error {
	if err := s.begin(); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(opErr) }()
	if s.backend.Secrets == nil {
		opErr = errors.New("the Secret Service is unavailable; the Windows VM password cannot be kept safely")
		return opErr
	}
	s.mu.Lock()
	iso, installed := s.config.ISOPath, s.installed
	s.mu.Unlock()
	if iso != "" && !installed && !vm.ISOPresent(iso) {
		// Without it Dockur would download Windows 11 instead, which is
		// not what was chosen.
		opErr = errors.New(i18n.Tf("Windows installs from %s, which is not there any more.", iso))
		return opErr
	}
	// A first start pulls the image, which can take a while on a slow
	// connection.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	opErr = s.backend.Start(ctx)
	return opErr
}

func (s *vmService) Connect(display *rdpDisplay) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := s.backend.Connect(ctx)
	if err != nil {
		return err
	}
	if s.backend.Secrets == nil || s.backend.SecretID() == "" {
		return errors.New("the Windows VM password is missing from the Secret Service")
	}
	password, err := s.backend.Secrets.Load(ctx, s.backend.SecretID())
	if err != nil {
		return err
	}
	defer clear(password)
	return startRDPClient(c.Address, c.Port, vm.GuestUser, string(password), display)
}

// Console opens the container's web console in the browser: during the first
// Windows installation it is the only usable view of the guest.
func (s *vmService) Console() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url, err := s.backend.Console(ctx)
	if err != nil {
		return err
	}
	return openBrowser(url)
}

// openBrowser hands a URL to the desktop's opener.
func openBrowser(url string) error {
	bin, err := exec.LookPath("xdg-open")
	if err != nil {
		return errors.New("no browser opener (xdg-open) is installed")
	}
	cmd := exec.Command(bin, url)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func (s *vmService) Stop() error {
	if err := s.begin(); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(opErr) }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	opErr = s.backend.Stop(ctx)
	return opErr
}

func (s *vmService) Remove() error {
	if err := s.begin(); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(opErr) }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := s.backend.Remove(ctx); err != nil {
		opErr = err
		return err
	}
	if err := os.Remove(vm.ConfigPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		opErr = err
		return err
	}
	s.mu.Lock()
	// The project and the guest are both gone; a fresh start is a fresh
	// setup, not a guest that is merely stopped.
	s.hasProject, s.installed = false, false
	s.config = vm.DefaultConfig()
	s.mu.Unlock()
	return nil
}

// RemoveData deletes the guest disk. It runs only after Remove (or when no
// configuration remains), so the container that had the disk mounted is gone.
func (s *vmService) RemoveData() error {
	if err := s.begin(); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(opErr) }()
	s.mu.Lock()
	// The backend may have no project (leftover disk after Remove, or a
	// fresh shell looking at DataDir). Point it at the configured directory
	// before deleting, so it does not refuse an empty path.
	s.backend.Config = s.config
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if opErr = s.backend.RemoveData(ctx); opErr != nil {
		return opErr
	}
	s.mu.Lock()
	s.installed = false
	hasProject := s.hasProject
	s.mu.Unlock()
	if hasProject {
		if err := s.save(false); err != nil {
			opErr = err
			return err
		}
	}
	return nil
}

func (s *vmService) begin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return errors.New("another Windows VM operation is in progress")
	}
	s.busy, s.err = true, nil
	return nil
}
func (s *vmService) end(err error) {
	s.mu.Lock()
	s.busy, s.err = false, err
	s.mu.Unlock()
	s.refresh() // so the next status shows what the operation did
}

func (s *vmService) save(installed bool) error {
	if err := os.MkdirAll(filepath.Dir(vm.ConfigPath()), 0700); err != nil {
		return err
	}
	s.mu.Lock()
	data, err := json.MarshalIndent(vm.Persisted{Config: s.config, Installed: installed}, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(vm.ConfigPath()), ".vm-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(append(data, '\n'))
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, vm.ConfigPath())
}

// rdpCommand runs FreeRDP with args. FreeRDP 3 reads them from its standard
// input (/args-from:stdin, one a line), so the guest's password stays off the
// command line, which every user of this machine can read in the process
// list. FreeRDP 2 has no such option and takes them as they are.
func rdpCommand(bin string, args []string, fromStdin bool) *exec.Cmd {
	if !fromStdin {
		return exec.Command(bin, args...)
	}
	cmd := exec.Command(bin, "/args-from:stdin")
	cmd.Stdin = strings.NewReader(strings.Join(args, "\n") + "\n")
	return cmd
}

// freerdp3 reports whether the client is FreeRDP 3 or later: the SDL client
// and the clients named with a 3 are; another says so in its version.
func freerdp3(name, bin string) bool {
	if strings.HasPrefix(name, "sdl-") || strings.HasSuffix(name, "3") {
		return true
	}
	out, _ := exec.Command(bin, "/version").CombinedOutput()
	return freerdpMajor(string(out)) >= 3
}

// freerdpMajor is the major version in FreeRDP's "This is FreeRDP version
// 3.10.3 (…)", 0 when there is none.
func freerdpMajor(version string) int {
	_, rest, ok := strings.Cut(version, "FreeRDP version ")
	if !ok {
		return 0
	}
	major, _, _ := strings.Cut(rest, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}

// rdpDisplay is the monitor FreeRDP's window appears on: its logical size and
// Hyprland's scale, which the guest's own display scaling follows.
type rdpDisplay struct {
	Width, Height int
	Scale         float64
}

// rdpScale is FreeRDP's desktop scale for a monitor. XWayland here forces
// scale 1, so the monitor scale must become the remote DPI: otherwise Windows
// renders at 100% into a 2x physical screen and its UI looks too small.
func rdpScale(scale float64) int {
	if scale <= 0 {
		return 100
	}
	return min(500, max(100, int(scale*100+0.5)))
}

// rdpArgs builds FreeRDP's arguments for a monitor. It opens a normal window,
// not fullscreen, at 80% of the monitor's logical size so the desktop remains
// usable; the remote resolution matches that window and follows resizes.
func rdpArgs(address string, port int, user, password string, d *rdpDisplay) []string {
	args := []string{
		"/v:" + net.JoinHostPort(address, fmt.Sprint(port)),
		"/u:" + user,
		"/p:" + password,
		"/dynamic-resolution", "/cert:tofu", "/clipboard",
	}
	if d == nil || d.Width <= 0 || d.Height <= 0 {
		return args
	}
	width, height := rdpWindowSize(d.Width, d.Height)
	args = append(args,
		fmt.Sprintf("/size:%dx%d", width, height),
		fmt.Sprintf("/scale-desktop:%d", rdpScale(d.Scale)),
		fmt.Sprintf("/scale-device:%d", min(180, max(100, rdpScale(d.Scale)))),
	)
	return args
}

// rdpWindowSize keeps a margin around the remote desktop on its monitor.
func rdpWindowSize(width, height int) (int, int) {
	w, h := int(float64(width)*0.8), int(float64(height)*0.8)
	return max(800, w), max(600, h)
}

// startRDPClient opens FreeRDP on a loopback endpoint, with the first of
// vm.RDPClients that is installed.
func startRDPClient(address string, port int, user, password string, d *rdpDisplay) error {
	for _, name := range vm.RDPClients {
		bin, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		cmd := rdpCommand(bin, rdpArgs(address, port, user, password, d), freerdp3(name, bin))
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		go cmd.Wait()
		return nil
	}
	return errors.New("FreeRDP is not installed")
}
