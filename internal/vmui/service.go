package vmui

import (
	"context"
	"errors"
	"fmt"
	"github.com/godbus/dbus/v5"
	"github.com/lyricat/inua/internal/keyring"
	"github.com/lyricat/inua/internal/vm"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// vmService owns the machines, the backends and the host checks. It touches no
// GTK: the command line uses it for --list, --start and --stop, the window for
// everything else.
//
// Reading what a guest is doing is slow (a Docker call, a QMP round trip on a
// Unix socket), so it never happens on the GTK thread: Refresh reads a machine
// off-thread and keeps the result here, and the window paints from that.
type vmService struct {
	mu       sync.Mutex
	store    vm.Store
	host     vm.Host
	platform vm.Platform
	backends map[vm.BackendKind]vm.Backend
	seen     map[string]observed // what each machine was last seen doing
	busy     map[string]bool     // a machine with an operation in flight

	// The install images a QEMU machine can be given: the catalog osinfo-db
	// publishes, the cache the images are downloaded into, and the transfer
	// each machine has going. isoRoots and isoDir are what a test replaces; a
	// test may set isoCatalog and isoCache outright.
	isoRoots   vm.OSInfoRoots
	isoDir     string
	isoCatalog *vm.ISOCatalog
	isoCache   *vm.ISODownloader
	// isoFilters is what the download list is showing: the window's controls
	// set it and isoView applies it, so both halves of the list agree.
	isoFilters vm.ISOFilters
	downloads  map[string]*isoDownload // a machine's transfer, by name

	// The Secret Service session, once it is there, and whether this service
	// has been closed. The connection is made in the background (see
	// connectSecrets), so both are read under mu.
	secretConn *dbus.Conn
	secrets    vm.GuestSecretStore
	closed     bool
}

// observed is what a machine was last seen doing. Everything the window shows
// about a machine comes from here plus its machine.toml.
type observed struct {
	state    vm.State
	err      error
	progress string
	prepared bool // the backend has made what it needs
	guest    bool // the guest's own files are on disk
	display  vm.Display

	accelerated        bool
	accelerationReason string
	serial             string
	debugger           string
}

// MachineView is one machine as the window shows it. Building one never touches
// a backend: it is the machine's own definition plus the last observation.
type MachineView struct {
	Name     string
	Machine  vm.Machine
	Problems []vm.Problem

	State        vm.State
	Prepared     bool // the backend has made what it needs (the project, the disk)
	Installed    bool // the guest has been installed: its own files are there
	Leftovers    bool // guest files are there without a project: a removal left them
	Busy         bool
	Progress     float64
	ProgressText string
	Err          error
	Page         vm.Page

	// Ready: this build has a backend for the machine's kind. Available: the
	// host gives that backend what it needs (the same checks as the prereq
	// page). Checks are the host checks, in the window's words.
	Ready        bool
	Available    bool
	Checks       []vm.DoctorItem
	Display      vm.Display
	Recommended  vm.Profile
	Distribution string
	HostOS       string

	// Accelerated: this machine's guest will run on the host's processor,
	// rather than being emulated. AccelerationReason says why it will not, as
	// one of the backend's stable identifiers, so the window can word it.
	Accelerated        bool
	AccelerationReason string
	// SerialLog is what the guest has printed to its serial console, for the
	// machines that have one.
	SerialLog string
	// DebuggerCmd is the line that attaches gdb to a machine started under the
	// debugger: what the window shows and copies. Empty when there is nothing
	// to attach to.
	DebuggerCmd string
	// ISO is the install images of a machine that installs from one: the
	// catalog to choose from and the download in flight. It is nil for the
	// machines whose backend fetches its own media, and for one that is
	// already set up.
	ISO *ISOView
}

// vmSecretStore keeps a guest's password in the Secret Service where the rest
// of the user's secrets are: labelled with the machine's name, found by its
// attributes, never in the machine's folder.
type vmSecretStore struct{ client *keyring.Client }

func (s vmSecretStore) Store(ctx context.Context, m vm.Machine, value []byte) error {
	return s.client.Store(ctx, vm.GuestSecretLabel(m), vm.GuestSecretAttrs(m), value)
}

// Load reads a machine's password. A machine installed by step 1 kept its
// password under the old label; it is read there and moved to the machine's
// own, so an installed Windows keeps the password it was installed with.
func (s vmSecretStore) Load(ctx context.Context, m vm.Machine) ([]byte, error) {
	v, ok, err := s.client.Lookup(ctx, vm.GuestSecretAttrs(m))
	if err != nil {
		return nil, err
	}
	if ok {
		return v, nil
	}
	legacy, ok, err := s.client.Lookup(ctx, vm.LegacyGuestSecretAttrs(m))
	if err != nil || !ok {
		return nil, errors.New("the guest password is missing from the Secret Service")
	}
	if err := s.Store(ctx, m, legacy); err != nil {
		return legacy, nil // it is usable where it is
	}
	if err := s.client.Delete(vm.LegacyGuestSecretAttrs(m)); err != nil {
		logErr("vm secret", err)
	}
	return legacy, nil
}

func (s vmSecretStore) Delete(_ context.Context, m vm.Machine) error {
	if err := s.client.Delete(vm.GuestSecretAttrs(m)); err != nil {
		return err
	}
	return s.client.Delete(vm.LegacyGuestSecretAttrs(m))
}

// newService reads the machines and wires the backends this build has. The
// Secret Service is found in the background: it is where a guest's password
// lives, and a keyring that wants a password nobody can be asked for must not
// keep the window - or `inua-vm --list` - waiting.
func newService() *vmService {
	s := &vmService{
		store:    vm.DefaultStore(),
		host:     vm.DetectHost(""),
		platform: vm.DetectPlatform("", vm.PlatformProbes{}),
		backends: map[vm.BackendKind]vm.Backend{},
		seen:     map[string]observed{},
		busy:     map[string]bool{},
	}
	s.backends[vm.BackendDockur] = vm.NewDockerBackend(s.store, nil)
	s.backends[vm.BackendQEMU] = vm.NewQEMUBackend(s.store, nil)
	go s.connectSecrets()
	return s
}

// connectSecrets finds the Secret Service once, in the background, and keeps
// the session bus open for as long as this service lives: a password read must
// not find the bus closed under it.
func (s *vmService) connectSecrets() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	client, err := keyring.Connect(conn, keyring.AlgDH)
	if err != nil {
		_ = conn.Close()
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = conn.Close()
		return
	}
	s.secretConn, s.secrets = conn, vmSecretStore{client: client}
}

// secretStore is the Secret Service, when it is there.
func (s *vmService) secretStore() (vm.GuestSecretStore, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets, s.secrets != nil
}

// close drops the Secret Service session. The window keeps it for the app's
// life; a run from the command line has nobody else to do it.
func (s *vmService) close() {
	s.mu.Lock()
	conn := s.secretConn
	s.secretConn, s.secrets, s.closed = nil, nil, true
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func logErr(what string, err error) {
	if err != nil {
		log.Printf("%s: %v", what, err)
	}
}

// secretsReady reports whether guest passwords can be kept safely. Without the
// Secret Service nothing may be installed: the password would have nowhere to
// go but the machine's folder.
func (s *vmService) secretsReady() bool {
	_, ok := s.secretStore()
	return ok
}

// checks is the host, in the window's words: one line per check. The Secret
// Service is one of them: it is where a machine's password lives.
func (s *vmService) checks() []vm.DoctorItem {
	s.mu.Lock()
	platform := s.platform
	s.mu.Unlock()
	out := make([]vm.DoctorItem, 0, len(platform.Doctor)+1)
	out = append(out, platform.Doctor...)
	if !s.secretsReady() {
		out = append(out, vm.DoctorItem{Name: "Secret Service", OK: false, Reason: vm.ReasonMissing})
	}
	return out
}

// relevantChecks are the checks this machine's backend uses. Showing a QEMU
// machine that Docker is missing would be noise: it needs no Docker at all.
func (s *vmService) relevantChecks(m vm.Machine) []vm.DoctorItem {
	names := map[vm.BackendKind][]string{
		vm.BackendDockur: {
			"Operating system", "KVM", "Docker", "Docker Compose v2",
			"Network device (TUN)", "FreeRDP", "Browser opener", "Disk", "Secret Service",
		},
		vm.BackendQEMU: {
			// KVM is not among them: a host without it runs the guest emulated,
			// which the machine's own line says rather than blocking it.
			"Operating system", "QEMU", "Systemd", "Disk",
		},
		vm.BackendWaydroid: {
			"Operating system", "Waydroid", "Binder",
		},
	}[m.Backend]
	wants := func(name string) bool {
		for _, want := range names {
			if want == name {
				return true
			}
		}
		return false
	}
	var out []vm.DoctorItem
	for _, c := range s.checks() {
		// The firmware and the SPICE viewer are a machine's own: only a UEFI
		// machine wants OVMF, and only a SPICE machine wants a SPICE viewer.
		switch c.Name {
		case "Firmware (UEFI)":
			if m.Backend != vm.BackendQEMU || m.Boot.Firmware != vm.FirmwareUEFI {
				continue
			}
		case "QEMU":
			// The check names the host's own architecture's binary; a machine
			// that asks for another is reported by the one it needs.
			if m.Backend != vm.BackendQEMU {
				continue
			}
			if !qemuProgramAvailable(m) {
				c.OK, c.Reason = false, vm.ReasonMissing
			}
		case "SPICE viewer":
			if m.Backend != vm.BackendQEMU || m.Display.Kind != vm.DisplaySPICE {
				continue
			}
		case "TUN":
			// The platform names this check "TUN"; the window words it.
			if !wants("Network device (TUN)") {
				continue
			}
		default:
			if !wants(c.Name) {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// serialTail is the end of a console's log: what the window's terminal view
// shows. A guest printing faster than a person reads keeps its last lines.
func serialTail(path string) string {
	const tail = 16 << 10
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	if fi.Size() > tail {
		if _, err := f.Seek(fi.Size()-tail, io.SeekStart); err != nil {
			return ""
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return strings.ToValidUTF8(string(data), "")
}

// availability is whether the host gives this machine's backend what it needs.
// Only the checks that backend uses count: a missing Docker does not keep a
// QEMU machine from running.
func (s *vmService) availability(m vm.Machine) bool {
	s.mu.Lock()
	p := s.platform
	s.mu.Unlock()
	switch m.Backend {
	case vm.BackendDockur:
		return p.Supported && p.KVM.Available && p.KVM.Accessible &&
			p.DockerSocket.Available && p.DockerSocket.Accessible &&
			p.ComposeV2.Available && p.TUN.Available && p.TUN.Accessible &&
			s.secretsReady()
	case vm.BackendQEMU:
		// QEMU needs no Docker and no /dev/kvm: without KVM the guest is
		// emulated, and the machine's own line says so. It does need its QEMU
		// binary (the one for the architecture it asks to run), systemd-run to
		// outlive the window, room for its disk, and the firmware and viewer
		// its settings call for.
		if !p.Supported || !p.SystemdRun.Available || !p.Disk.Available {
			return false
		}
		if !qemuProgramAvailable(m) {
			return false
		}
		if m.Boot.Firmware == vm.FirmwareUEFI && !p.Firmware.Available {
			return false
		}
		if m.Display.Kind == vm.DisplaySPICE && !p.SPICEViewer.Available {
			return false
		}
		return true
	default:
		// The Waydroid checks come with its own backend.
		return false
	}
}

// qemuProgramAvailable says whether this host has the QEMU binary this machine
// asks for: a guest of another architecture is run by qemu-system-<its own>,
// which the host may not have installed.
func qemuProgramAvailable(m vm.Machine) bool {
	program := vm.QEMUProgram(m.Boot.Arch)
	if program == "" {
		return false // an architecture this build has no QEMU for
	}
	_, err := exec.LookPath(program)
	return err == nil
}

// backendOf is the backend that runs this machine, when this build has one.
// A backend that keeps a guest's password is handed the Secret Service here, so
// an install or a start that needs it has it.
func (s *vmService) backendOf(m vm.Machine) (vm.Backend, bool) {
	s.mu.Lock()
	be, ok := s.backends[m.Backend]
	s.mu.Unlock()
	if ok && m.Backend == vm.BackendDockur {
		if docker, isDocker := be.(*vm.DockerBackend); isDocker && docker.Secrets == nil {
			if store, ready := s.secretStore(); ready {
				docker.Secrets = store
			}
		}
	}
	return be, ok
}

// machines reads the store. A machine whose machine.toml does not hold is left
// out: the window's list is what can be run.
func (s *vmService) machines() []vm.Machine {
	list, err := s.store.List()
	if err != nil {
		logErr("vm machines", err)
		return nil
	}
	return list
}

// Machines is the list, for the window's rows: no backend is read here.
func (s *vmService) Machines() []MachineView {
	list := s.machines()
	out := make([]MachineView, 0, len(list))
	for _, m := range list {
		out = append(out, s.view(m))
	}
	return out
}

// Names is the machines' names, for the command line.
func (s *vmService) Names() []string {
	list := s.machines()
	out := make([]string, 0, len(list))
	for _, m := range list {
		out = append(out, m.Name)
	}
	return out
}

// Faults lists store folders that cannot be loaded as machines.
func (s *vmService) Faults() []vm.Fault {
	return s.store.Faults()
}

// DeleteFault removes an unloadable machine folder without consulting a backend.
func (s *vmService) DeleteFault(name string) error {
	if err := s.begin(name); err != nil {
		return err
	}
	err := s.store.DeleteFault(name)
	s.end(name, err)
	if err == nil {
		s.mu.Lock()
		delete(s.seen, name)
		s.mu.Unlock()
	}
	return err
}

// Lookup reads one machine.
func (s *vmService) Lookup(name string) (vm.Machine, error) {
	return s.store.Load(name)
}

// Snapshot is one machine's view as last observed.
func (s *vmService) Snapshot(name string) MachineView {
	m, err := s.store.Load(name)
	if err != nil {
		// A machine whose machine.toml does not hold is shown with what is
		// wrong with it, on the page that explains a host problem.
		return MachineView{Name: name, Err: err, Page: vm.PagePrereq, Checks: s.checks()}
	}
	return s.view(m)
}

// view builds a machine's view from its definition and the last observation.
func (s *vmService) view(m vm.Machine) MachineView {
	_, ready := s.backendOf(m)
	available := ready && s.availability(m)
	s.mu.Lock()
	o, busy := s.seen[m.Name], s.busy[m.Name]
	s.mu.Unlock()
	if !o.prepared {
		o.state = vm.StateStopped
	}
	s.mu.Lock()
	recommended, distribution, hostOS := vm.RecommendProfile(s.host), s.platform.Distribution, s.platform.OS
	s.mu.Unlock()
	// Installed is the guest's own files *and* the backend's project: a removal
	// that kept the disk leaves the files, and its Install makes the project
	// again - so that machine is set up, not stopped.
	installed := o.prepared && o.guest
	v := MachineView{
		Name: m.Name, Machine: m, Problems: m.Problems(),
		State: o.state, Prepared: o.prepared, Installed: installed, Leftovers: o.guest,
		Busy: busy, Progress: vm.ParsePercent(o.progress), ProgressText: o.progress,
		Err: o.err, Ready: ready, Available: available, Checks: s.relevantChecks(m),
		Display: o.display, Recommended: recommended, Distribution: distribution, HostOS: hostOS,
		Accelerated: o.accelerated, AccelerationReason: o.accelerationReason,
		SerialLog: o.serial, DebuggerCmd: o.debugger,
	}
	v.Page = vm.PageFor(v.Available, v.Prepared, v.Installed, v.State, v.Busy)
	// The install images are the page that has not installed anything yet:
	// once the guest's files are there, the list has done its work. It is
	// built here, where the machine is read, rather than on the GTK thread.
	if m.Backend == vm.BackendQEMU && !o.prepared {
		v.ISO = s.isoView(m)
	}
	return v
}

// Refresh reads one machine's backend off the GTK thread and returns its view.
// It is what the window's watch calls, and what --status uses.
func (s *vmService) Refresh(name string) MachineView {
	m, err := s.store.Load(name)
	if err != nil {
		s.mu.Lock()
		s.seen[name] = observed{}
		s.mu.Unlock()
		return MachineView{Name: name, Err: err, Page: vm.PagePrereq, Checks: s.checks()}
	}
	o := observed{guest: vm.GuestDiskPresent(m.Dir(s.store))}
	if be, ok := s.backendOf(m); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if p, ok := be.(vm.Prepared); ok {
			o.prepared = p.Prepared(m)
		}
		if o.prepared {
			o.state, o.err = be.State(ctx, m)
			if o.err == nil {
				if prog, ok := be.(vm.Progress); ok {
					o.progress = prog.RecentOutput(ctx, m)
				}
				if d, err := be.Display(ctx, m); err == nil {
					o.display = d
				}
			}
		}
		if qemu, isQEMU := be.(*vm.QEMUBackend); isQEMU {
			a := qemu.Acceleration(m)
			o.accelerated, o.accelerationReason = a.Accelerated, a.Reason
			o.serial = serialTail(qemu.SerialLogPath(m))
			o.debugger = qemu.GDBCommand(m)
		}
		cancel()
	}
	s.mu.Lock()
	s.seen[name] = o
	s.mu.Unlock()
	return s.view(m)
}

// RefreshAll reads every machine, for the window's slower sweep. The host is
// re-read too: the user may have installed Docker or joined a group since the
// shell started, and the prereq page has to notice without a restart.
func (s *vmService) RefreshAll() []MachineView {
	host := vm.DetectHost("")
	platform := vm.DetectPlatform("", vm.PlatformProbes{})
	s.mu.Lock()
	s.host, s.platform = host, platform
	s.mu.Unlock()
	list := s.machines()
	out := make([]MachineView, 0, len(list))
	for _, m := range list {
		out = append(out, s.Refresh(m.Name))
	}
	return out
}

// Templates are what a new machine can be made from.
func (s *vmService) Templates() []vm.Template { return vm.Templates() }

// Recommended is what suits this computer, for a new machine.
func (s *vmService) Recommended() vm.Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return vm.RecommendProfile(s.host)
}

// Create writes a new machine from a template, with the resources asked for
// (below a minimum, the template's own). It makes nothing and starts nothing:
// which page the machine shows follows from what is on disk.
func (s *vmService) Create(templateID, name string, p vm.Profile) (vm.Machine, error) {
	t, ok := vm.TemplateByID(templateID)
	if !ok {
		return vm.Machine{}, fmt.Errorf("there is no %q template", templateID)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = t.Name
	}
	unique, err := vm.UniqueName(s.store, name)
	if err != nil {
		return vm.Machine{}, err
	}
	m := t.Machine(unique, p)
	if err := s.store.Save(m); err != nil {
		return vm.Machine{}, err
	}
	return m, nil
}

// Save writes a machine's definition and asks its backend to make what it
// needs: the Compose project for Windows, the disk for QEMU. It does not start
// anything, and it never touches a disk that is already there - writing the
// definition again is how a changed memory, CPU count or disk size takes
// effect.
func (s *vmService) Save(m vm.Machine) error {
	if err := s.begin(m.Name); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(m.Name, opErr) }()
	if err := s.store.Save(m); err != nil {
		opErr = err
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		opErr = s.noBackend(m)
		return opErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	opErr = be.Install(ctx, m)
	return opErr
}

// noBackend is what a machine whose backend this build does not have reports.
func (s *vmService) noBackend(m vm.Machine) error {
	return fmt.Errorf("this build has no %s backend", m.Backend)
}

// Install writes the machine and starts it: for Windows that is the image
// download and the unattended setup, which the window then follows on the
// provisioning page.
func (s *vmService) Install(m vm.Machine) error {
	if stored, err := s.store.Load(m.Name); err == nil && stored.Backend == vm.BackendQEMU &&
		strings.TrimSpace(m.Boot.ISO) == "" && strings.TrimSpace(m.Boot.Kernel) == "" {
		disk := strings.TrimSpace(stored.Boot.Disk)
		if disk == "" {
			disk = "disk.qcow2"
		}
		if _, err := os.Stat(filepath.Join(stored.Dir(s.store), disk)); err != nil {
			return errors.New("choose an install image or kernel before installing, or provide an existing guest disk")
		}
	}
	if err := s.Save(m); err != nil {
		return err
	}
	return s.Start(m.Name)

}

// Start brings a machine up and returns before the guest is ready: a first
// Windows install is tens of minutes, and the window shows the progress.
func (s *vmService) Start(name string) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	if m.Backend == vm.BackendDockur && !s.secretsReady() {
		return errors.New("the Secret Service is unavailable: the guest password cannot be kept safely")
	}
	if err := s.begin(name); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(name, opErr) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	opErr = be.Start(ctx, m)
	return opErr
}

// Stop shuts a machine down, keeping its disk and its password.
func (s *vmService) Stop(name string) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	if err := s.begin(name); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(name, opErr) }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	opErr = be.Stop(ctx, m)
	return opErr
}

// Connect opens what shows the running machine: FreeRDP for Windows on a
// loopback port, the web console while it installs, the guest's own window
// when the backend draws it.
func (s *vmService) Connect(name string, display *rdpDisplay) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := be.Display(ctx, m)
	if err != nil {
		return err
	}
	switch d.Kind {
	case vm.DisplayRDP:
		host, port, err := splitHostPort(d.Address)
		if err != nil {
			return err
		}
		password, err := s.password(ctx, m)
		if err != nil {
			return err
		}
		defer clear(password)
		return startRDPClient(host, port, vm.GuestUser, string(password), display)
	case vm.DisplayWeb:
		if d.Console == "" {
			return errors.New("this machine has no web console")
		}
		return openBrowser(d.Console)
	case vm.DisplaySPICE:
		return openSpice(d.Address)
	case vm.DisplayGTK:
		// The guest's own window is already on screen.
		return nil
	case vm.DisplaySerial:
		// The window shows its own terminal view of the console; from the
		// command line there is nothing to open.
		return errors.New("this machine is seen through its serial console")
	}
	return fmt.Errorf("this machine has no viewer (%s)", d.Kind)
}

// TeeSerial mirrors a QEMU guest's output while its serial console is open.
func (s *vmService) TeeSerial(name string) error {
	return s.TeeSerialContext(context.Background(), name)
}

// TeeSerialContext streams serial output until ctx is cancelled.
func (s *vmService) TeeSerialContext(ctx context.Context, name string) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	qemu, ok := be.(*vm.QEMUBackend)
	if !ok {
		return fmt.Errorf("%s does not have a QEMU serial console", name)
	}
	return qemu.TeeSerial(ctx, m)
}

// Console opens the machine's web console in the browser: during a first
// Windows installation it is the only usable view of the guest.
func (s *vmService) Console(name string) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := be.Display(ctx, m)
	if err != nil {
		return err
	}
	if d.Console == "" {
		return errors.New("this machine has no web console")
	}
	return openBrowser(d.Console)
}

// password reads a machine's guest password, for the viewer that needs it.
func (s *vmService) password(ctx context.Context, m vm.Machine) ([]byte, error) {
	store, ok := s.secretStore()
	if !ok {
		return nil, errors.New("the Secret Service is unavailable: the guest password cannot be read")
	}
	return store.Load(ctx, m)
}

// Remove takes the machine's container or process apart and leaves the guest's
// own files alone: its disk, its password and its definition stay, so the same
// Windows can be started again.
func (s *vmService) Remove(name string) error {
	return s.remove(name, true, false)
}

// DeleteDisk is the destructive one: it removes the guest's own files, so the
// machine has nothing installed and can be installed from scratch. The
// machine's definition stays. Callers ask first.
func (s *vmService) DeleteDisk(name string) error {
	return s.remove(name, false, false)
}

// Delete takes the machine away entirely: its guest's files and its definition,
// so it is gone from the list. Callers ask first.

func (s *vmService) Delete(name string) error {
	if err := s.begin(name); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(name, opErr) }()

	m, loadErr := s.store.Load(name)
	if loadErr != nil {
		opErr = s.store.DeleteFault(name)
	} else if be, ok := s.backendOf(m); ok {
		prepared, hasPrepared := be.(vm.Prepared)
		if !hasPrepared || !prepared.Prepared(m) {
			opErr = s.store.Delete(name)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			state, stateErr := be.State(ctx, m)
			cancel()
			if stateErr != nil {
				opErr = fmt.Errorf("cannot confirm %s is stopped: %w", name, stateErr)
				return opErr
			}
			if state == vm.StateRunning || state == vm.StateStarting || state == vm.StateStopping {
				opErr = fmt.Errorf("%s must be stopped before it can be deleted", name)
				return opErr
			}
			ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if opErr = be.Remove(ctx, m, false); opErr != nil {
				return opErr
			}
			opErr = s.store.Delete(name)
		}
	} else {
		opErr = s.store.Delete(name)
	}
	if opErr == nil {
		s.mu.Lock()
		delete(s.seen, name)
		s.mu.Unlock()
	}
	return opErr
}

func (s *vmService) remove(name string, keepDisk, forget bool) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	be, ok := s.backendOf(m)
	if !ok {
		return s.noBackend(m)
	}
	if err := s.begin(name); err != nil {
		return err
	}
	var opErr error
	defer func() { s.end(name, opErr) }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := be.Remove(ctx, m, keepDisk); err != nil {
		opErr = err
		return err
	}
	if forget {
		if err := s.store.Delete(name); err != nil {
			opErr = err
			return err
		}
		s.mu.Lock()
		delete(s.seen, name)
		s.mu.Unlock()
	}
	return nil
}

// Fix applies a failing check's remedy. A privileged one goes through pkexec,
// so the shell's own polkit dialog takes the password; this runs on a
// goroutine, because that dialog is on the main thread and this call waits.
func (s *vmService) Fix(r vm.Remedy) (string, bool, error) {
	done, relogin, err := vm.ApplyRemedy(r)
	s.mu.Lock()
	s.platform = vm.DetectPlatform("", vm.PlatformProbes{})
	s.mu.Unlock()
	return done, relogin, err
}

// Doctor is the host checks as plain lines, for `inua-vm --doctor`.
func (s *vmService) Doctor() string {
	var b strings.Builder
	for _, c := range s.checks() {
		if c.OK {
			fmt.Fprintf(&b, "OK   %s\n", c.Name)
			continue
		}
		fmt.Fprintf(&b, "FAIL %s (%s)\n", c.Name, c.Reason)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Status is what the machines are doing now, for scripts: the host's checks
// and one entry per machine, each read from its backend.
type StatusReport struct {
	Checks   []StatusCheck   `json:"checks"`
	Machines []StatusMachine `json:"machines"`
	Dir      string          `json:"machines_dir"`
}

// StatusCheck is one host check, as --status reports it.
type StatusCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// StatusMachine is one machine, as --status reports it.
type StatusMachine struct {
	Name      string `json:"name"`
	OS        string `json:"os"`
	Backend   string `json:"backend"`
	State     string `json:"state"`
	Installed bool   `json:"installed"`
	Ready     bool   `json:"ready"`
	Problem   string `json:"problem,omitempty"`
}

// Status reads every machine, for `inua-vm --status`.
func (s *vmService) Status() StatusReport {
	st := StatusReport{Dir: s.store.Root}
	for _, c := range s.checks() {
		st.Checks = append(st.Checks, StatusCheck{Name: c.Name, OK: c.OK, Reason: string(c.Reason)})
	}
	for _, v := range s.RefreshAll() {
		e := StatusMachine{
			Name: v.Name, OS: string(v.Machine.OS), Backend: string(v.Machine.Backend),
			State: string(v.State), Installed: v.Installed, Ready: v.Ready,
		}
		switch {
		case v.Err != nil:
			e.Problem = v.Err.Error()
		case len(v.Problems) > 0:
			e.Problem = v.Problems[0].Field + ": " + string(v.Problems[0].Reason)
		}
		st.Machines = append(st.Machines, e)
	}
	return st
}

// StatusText is the status as both the command line and the shell print it.
func StatusText(st StatusReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Machines: %s\n", st.Dir)
	for _, c := range st.Checks {
		if c.OK {
			fmt.Fprintf(&b, "OK: %s\n", c.Name)
			continue
		}
		fmt.Fprintf(&b, "FAIL: %s (%s)\n", c.Name, c.Reason)
	}
	if len(st.Machines) == 0 {
		fmt.Fprintln(&b, "No machines.")
		return b.String()
	}
	for _, m := range st.Machines {
		fmt.Fprintf(&b, "%s: %s, %s, %s", m.Name, m.OS, m.Backend, m.State)
		if !m.Ready {
			fmt.Fprintf(&b, " (no %s backend in this build)", m.Backend)
		}
		if m.Problem != "" {
			fmt.Fprintf(&b, " — %s", m.Problem)
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

// State is what one machine is doing now, for the command line and for a
// script that wants a single answer.
func (s *vmService) State(name string) (vm.State, error) {
	v := s.Refresh(name)
	if v.Err != nil {
		return v.State, v.Err
	}
	if !v.Ready {
		return v.State, s.noBackend(v.Machine)
	}
	return v.State, nil
}

// begin takes a machine's operation lock, so two long operations - a start and
// a delete - never overlap on one machine. Machines are independent: two of
// them can be worked on at once.
func (s *vmService) begin(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[name] {
		return fmt.Errorf("%s already has an operation in progress", name)
	}
	s.busy[name] = true
	return nil
}

func (s *vmService) end(name string, err error) {
	s.mu.Lock()
	s.busy[name] = false
	o := s.seen[name]
	o.err = err
	s.seen[name] = o
	s.mu.Unlock()
}

// splitHostPort reads "127.0.0.1:3389" apart, for a viewer that takes them one
// by one.
func splitHostPort(address string) (string, int, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
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

// openSpice shows a guest whose display is SPICE on a Unix socket.
func openSpice(socket string) error {
	if socket == "" {
		return errors.New("this machine has no SPICE socket")
	}
	bin, err := exec.LookPath("remote-viewer")
	if err != nil {
		return errors.New("remote-viewer (virt-viewer) is not installed")
	}
	cmd := exec.Command(bin, "spice+unix://"+socket)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
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

// startRDPClient opens FreeRDP on a loopback endpoint. The SDL3 client is
// preferred: it is the maintained Wayland client and draws the remote cursor.
// wlfreerdp is deprecated and loses the pointer on Hyprland, so it is only a
// last compatibility fallback. X11 runs through XWayland at scale 1 here.
func startRDPClient(address string, port int, user, password string, d *rdpDisplay) error {
	for _, name := range vm.RDPClients {
		bin, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		// FreeRDP 3 reads its arguments from standard input, so the guest's
		// password stays off the command line, which every process list on
		// this machine can read.
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

// ── install images ──────────────────────────────────────────────────────────
//
// A QEMU machine installs from an image, and the image a user does not have yet
// is downloaded from the catalog osinfo-db publishes. The list and the transfer
// are the service's, GTK-free: the window paints what is here, and the download
// goes on off the GTK thread like every other slow thing. Nothing here knows
// about the window's widgets.
//
// See docs/iso-download.md, sections 2, 4 and 7.

// ISORow is one install medium as the window's download list shows it: the
// medium the catalog offers, and how the file already stands on this host.
type ISORow struct {
	// Media is the medium: the system, its variant, the architecture, whether
	// it is a live image or an installer, its size, and the URL it is at.
	Media vm.ISOMedia
	// Cached is an image that is already in the cache, at Path: the row's
	// action uses it rather than downloading it again.
	Cached bool
	Path   string
	// Partial is an unfinished download, which the row's action resumes
	// instead of starting the file again.
	Partial bool
	// Checksum is how the published checksum of a finished download turned
	// out, when this service downloaded it; empty when there is no answer.
	Checksum vm.ISOChecksum
	// Downloading is this medium the machine's transfer in flight.
	Downloading bool
	// Err is what the last attempt at this medium failed with.
	Err error
}

// ISOSystemRow is one operating system of the download list: the system itself
// and the rows of the media the filters left of it, in the order its own row
// expands to.
type ISOSystemRow struct {
	System vm.ISOSystem
	Rows   []ISORow
}

// ISOView is a machine's install images: the catalog to choose from and the
// transfer in flight, when there is one. A machine whose backend fetches its
// own media has none.
type ISOView struct {
	// Source is where the list came from: vm.ISOSourceOsinfo when this host
	// has osinfo-db, vm.ISOSourceBuiltin when it fell back to inua's own list.
	Source string
	Rows   []ISORow

	// Systems are the same media as the list shows them: one entry per
	// operating system, each holding its own media rows. Filtered and counted
	// the same way Rows is, so the two always describe one list.
	Systems []ISOSystemRow
	// Recommended is the same list's well-known distributions, in the order the
	// catalog names them, at most one release each: the systems the section
	// above the list offers first. It is drawn from Systems, so what the
	// filters leave out is left out of it too.
	Recommended []ISOSystemRow
	// SystemsCount and MediaCount are what the filters left, for the section's
	// own line: how many systems and how many images the user is looking at.
	SystemsCount, MediaCount int
	// ArchFamilies are the architecture families the catalog actually holds, the
	// host's own first: the choices the list's architecture filter offers.
	ArchFamilies []string

	// The transfer in flight: which medium, how far it has got, and what the
	// last attempt failed with. Downloading is false between two attempts, and
	// Total is zero while the server has not said how large the file is.
	Downloading bool
	Media       vm.ISOMedia
	Done, Total int64
	Err         error
}

// isoDownload is one machine's transfer: what it is downloading, how far it
// has got, and how to stop it. Its fields are written by the download's own
// goroutine and read where a machine's view is built, so every one of them is
// touched under the service's lock.
type isoDownload struct {
	media   vm.ISOMedia
	running bool
	done    int64
	total   int64
	err     error
	cancel  context.CancelFunc
}

// isoDownloader is the image cache, made on the first use: the service passes
// no directory, so images go to the usual cache (vm.DefaultISODir).
func (s *vmService) isoDownloader() *vm.ISODownloader {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isoCache == nil {
		s.isoCache = vm.NewISODownloader(s.isoDir)
	}
	return s.isoCache
}

// catalog is the catalog osinfo-db publishes, read once and kept: the list does
// not change while the window is open, and reading it is a walk of several
// hundred files. The zero OSInfoRoots is this host's own two directories.
func (s *vmService) catalog() vm.ISOCatalog {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isoCatalog == nil {
		roots := s.isoRoots
		if roots.System == "" && roots.User == "" {
			roots = vm.DefaultOSInfoRoots()
		}
		cat := vm.LoadISOCatalog(roots)
		s.isoCatalog = &cat
	}
	return *s.isoCatalog
}

// isoRowsOf turns a catalog into the download list. It is separate from the
// service so that what a row says can be read - and tested - without a window:
// what is already in the cache, what is half downloaded, how a finished
// download was verified, and which medium is the transfer in flight.
func isoRowsOf(cat vm.ISOCatalog, cache *vm.ISODownloader, dl *isoDownload) []ISORow {
	return isoRowsOfMedia(cat.Media, cache, dl)
}

// isoRowsOfMedia is isoRowsOf for the media the filters left: the flat list of
// rows and the system rows are built from the same slice, so what the list
// counts and what it shows cannot drift apart.
func isoRowsOfMedia(media []vm.ISOMedia, cache *vm.ISODownloader, dl *isoDownload) []ISORow {
	rows := make([]ISORow, 0, len(media))
	for _, media := range media {
		row := ISORow{Media: media}
		if p, ok := cache.CachedPath(media); ok {
			row.Cached, row.Path = true, p
		}
		if _, ok := cache.PartialPath(media); ok {
			row.Partial = true
		}
		if c, ok := cache.Checksum(media.URL); ok {
			row.Checksum = c
		}
		if dl != nil && dl.media.URL == media.URL {
			row.Downloading = dl.running
			row.Err = dl.err
		}
		rows = append(rows, row)
	}
	return rows
}

// isoSystemsOf pairs each system with the rows of its media. The rows are the
// filtered list's own, in its own order, so a system's rows are looked up by
// the system's short id and never re-sorted here.
func isoSystemsOf(systems []vm.ISOSystem, rows []ISORow) []ISOSystemRow {
	byID := map[string][]ISORow{}
	for _, r := range rows {
		byID[r.Media.OSID] = append(byID[r.Media.OSID], r)
	}
	out := make([]ISOSystemRow, 0, len(systems))
	for _, s := range systems {
		out = append(out, ISOSystemRow{System: s, Rows: byID[s.OSID]})
	}
	return out
}

// isoArchFamilies is what the architecture filter can offer: the families the
// catalog holds, the host's own first, so that the default is the first entry
// and a family no medium has is never offered.
func isoArchFamilies(media []vm.ISOMedia) []string {
	seen := map[string]bool{}
	var others []string
	for _, m := range media {
		f := vm.ISOArchFamily(m.Arch)
		if f == "" || f == vm.HostArchFamily() || seen[f] {
			continue
		}
		seen[f] = true
		others = append(others, f)
	}
	sort.Strings(others)
	return append([]string{vm.HostArchFamily()}, others...)
}

// SetISOFilters is what the download list is showing. The window owns the
// choice - it is the thing with the controls - and the service applies it where
// a machine's view is built, so that both halves of the list agree.
func (s *vmService) SetISOFilters(f vm.ISOFilters) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isoFilters = f
}

// isoFilters is the filters in use, read under the lock the downloads keep.
func (s *vmService) isoFiltersNow() vm.ISOFilters {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isoFilters
}

// isoView is a machine's install images as the window shows them. It is read
// where a machine's view is built, off the GTK thread, and it only ever reads
// the catalog and the download's own state.
func (s *vmService) isoView(m vm.Machine) *ISOView {
	if m.Backend != vm.BackendQEMU {
		return nil
	}
	cache := s.isoDownloader()
	s.mu.Lock()
	var dl *isoDownload
	if d := s.downloads[m.Name]; d != nil {
		copied := *d
		dl = &copied
	}
	s.mu.Unlock()

	cat := s.catalog()
	media := vm.FilterISOMedia(cat.Media, s.isoFiltersNow())
	rows := isoRowsOfMedia(media, cache, dl)
	systems := vm.GroupISOSystems(media)
	v := &ISOView{
		Source:       cat.Source,
		Rows:         rows,
		Systems:      isoSystemsOf(systems, rows),
		Recommended:  isoSystemsOf(vm.RecommendedSystems(systems), rows),
		SystemsCount: len(systems),
		MediaCount:   len(rows),
		ArchFamilies: isoArchFamilies(cat.Media),
	}
	if dl != nil {
		v.Downloading, v.Media = dl.running, dl.media
		v.Done, v.Total, v.Err = dl.done, dl.total, dl.err
	}
	return v
}

// FetchISO downloads an install medium for a machine and makes it that
// machine's install media: the boot field the existing install flow starts
// from. It runs until the transfer is over, so the window calls it off the GTK
// thread; a cancelled one returns context.Canceled and leaves the part file, so
// the next attempt resumes it rather than starting the file again.
//
// One transfer per machine at a time, as one progress bar can show: a second
// request while one is running is refused, and the window's own operation bus
// is what keeps the two from being asked for together.
func (s *vmService) FetchISO(name string, media vm.ISOMedia) error {
	m, err := s.store.Load(name)
	if err != nil {
		return err
	}
	if m.Backend != vm.BackendQEMU {
		return fmt.Errorf("%s does not install from an image", name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.mu.Lock()
	if s.downloads == nil {
		s.downloads = map[string]*isoDownload{}
	}
	if dl := s.downloads[name]; dl != nil && dl.running {
		s.mu.Unlock()
		return fmt.Errorf("%s is already downloading an image", name)
	}
	dl := &isoDownload{media: media, running: true, cancel: cancel}
	s.downloads[name] = dl
	s.mu.Unlock()

	path, err := s.isoDownloader().Download(ctx, media, func(done, total int64) {
		s.mu.Lock()
		if d := s.downloads[name]; d == dl {
			d.done, d.total = done, total
		}
		s.mu.Unlock()
	})

	s.mu.Lock()
	dl.running, dl.cancel, dl.err = false, nil, err
	s.mu.Unlock()
	if err != nil {
		return err
	}
	// The image is this machine's install media now. Writing the definition
	// leaves the disk and the start to the install flow the window already has.
	m.Boot.ISO = path
	return s.store.Save(m)
}

// CancelISO stops the transfer a machine has going, if any. The part file
// stays, so the same medium offered again resumes from where it stopped.
func (s *vmService) CancelISO(name string) {
	s.mu.Lock()
	var cancel context.CancelFunc
	if dl := s.downloads[name]; dl != nil {
		cancel = dl.cancel
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
