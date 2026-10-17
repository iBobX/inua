package vmui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/vm"
)

// VMManager is the UI-facing boundary for VM inspection and lifecycle actions.
// Implementations should perform long-running operations asynchronously and
// report changes with Update; all methods are invoked from GTK callbacks.
type VMManager interface {
	Snapshot() VMSnapshot
	Refresh() VMSnapshot
	Configure(vm.Config) error
	Install(vm.Config) error
	Start() error
	Connect(*rdpDisplay) error
	Console() error
	Stop() error
	Remove() error
	RemoveData() error
	// Fix applies a failing check's remedy, asking for the password through
	// the session's polkit agent when it needs one.
	Fix(vm.Remedy) (done string, relogin bool, err error)
}

// VMSnapshot is a presentation-ready view of VM state.
type VMSnapshot struct {
	Config vm.Config
	State  vm.State
	Checks []vm.DoctorItem // the host checks, shown on the prereq page
	// HasProject: the Compose project exists. Installed: Windows has come
	// up and a guest disk image is on disk.
	HasProject   bool
	Installed    bool
	HasDisk      bool // leftover files in DataDir (ISO, tmp, or a disk)
	Progress     float64
	ProgressText string
	Busy         bool
	Available    bool
	Recommended  vm.Profile
	Host         vm.Host
	Family       string // the distribution family the package hints are for
	Err          error
}

func (s VMSnapshot) phase() vm.Page {
	return vm.PageFor(s.Available, s.HasProject, s.Installed, s.State, s.Busy)
}

// failedChecks are the host checks that did not pass.
func (s VMSnapshot) failedChecks() []vm.DoctorItem {
	var out []vm.DoctorItem
	for _, c := range s.Checks {
		if !c.OK {
			out = append(out, c)
		}
	}
	return out
}

func doctorItemsEqual(a, b []vm.DoctorItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].OK != b[i].OK || a[i].Reason != b[i].Reason {
			return false
		}
		ar, br := a[i].Remedy, b[i].Remedy
		if (ar == nil) != (br == nil) {
			return false
		}
		if ar != nil && ar.Group != br.Group {
			return false
		}
	}
	return true
}

type vmWindow struct {
	win     *gtk.ApplicationWindow
	manager VMManager
	c       *appui.Ctx // the app, for a notification the window raises

	sub gtk.Label // the headline under the title

	stack gtk.Stack

	// prereq
	checkRows gtk.Box

	// setup
	setupSummary                    gtk.Label
	profileDesc                     gtk.Label
	accept, advancedToggle, install gtk.Button
	advanced                        gtk.Revealer
	memory, cpus, disk              gtk.SpinButton
	setupDisk                       gtk.Box
	setupRemoveData                 gtk.Button
	setupNote                       gtk.Label
	isoDesc                         gtk.Label
	iso, isoClear                   gtk.Button
	isoPath                         string // the installer ISO chosen, "" to download Windows
	isoDialog                       *gtk.FileDialog
	onISOChosen                     gio.AsyncReadyCallback

	// provisioning
	progress                                 gtk.ProgressBar
	progressText                             gtk.Label
	console, provStart, provStop, provRemove gtk.Button

	// stopped
	readySummary              gtk.Label
	readyMemory               gtk.SpinButton
	readyCPUs                 gtk.SpinButton
	diskSize                  gtk.Label
	readySave                 gtk.Button
	start, remove, removeData gtk.Button

	// starting
	bootProgress     gtk.ProgressBar
	bootProgressText gtk.Label
	bootConsole      gtk.Button
	bootStop         gtk.Button

	// running
	runSummary   gtk.Label
	connect      gtk.Button
	consoleReady gtk.Button
	stop         gtk.Button

	closed        bool
	removeArmed   bool
	dataArmed     bool
	setupFilled   bool
	readyFilled   bool
	operationBusy bool
	checkRefs     gtkx.Scope // the rows built for the current check list
	fixButtons    []fixTarget
	fixNote       string // what a fix did, shown until the next refresh
	watchCancel   context.CancelFunc
	lastPage      vm.Page
	lastChecks    []vm.DoctorItem
	onClick       func(gtk.Button)
}

// openWindow builds the manager window from the app's layouts: a second start
// of inua-vm brings this one forward instead of making another (appui).
func openWindow(c *appui.Ctx, manager VMManager) *vmWindow {
	v := &vmWindow{manager: manager, c: c, win: &gtk.ApplicationWindow{}}
	builder := c.Load("vm.ui")
	builder.Get("vm", v.win)
	builder.Get("vm_sub", &v.sub)
	builder.Get("vm_stack", &v.stack)
	builder.Get("vm_check_rows", &v.checkRows)
	builder.Get("vm_setup_summary", &v.setupSummary)
	builder.Get("vm_profile_desc", &v.profileDesc)
	builder.Get("vm_accept", &v.accept)
	builder.Get("vm_advanced_toggle", &v.advancedToggle)
	builder.Get("vm_advanced", &v.advanced)
	builder.Get("vm_memory", &v.memory)
	builder.Get("vm_cpus", &v.cpus)
	builder.Get("vm_disk", &v.disk)
	builder.Get("vm_install", &v.install)
	builder.Get("vm_setup_disk", &v.setupDisk)
	builder.Get("vm_setup_remove_data", &v.setupRemoveData)
	builder.Get("vm_setup_note", &v.setupNote)
	builder.Get("vm_iso_desc", &v.isoDesc)
	builder.Get("vm_iso", &v.iso)
	builder.Get("vm_iso_clear", &v.isoClear)
	builder.Get("vm_progress", &v.progress)
	builder.Get("vm_progress_text", &v.progressText)
	builder.Get("vm_console", &v.console)
	builder.Get("vm_prov_start", &v.provStart)
	builder.Get("vm_prov_stop", &v.provStop)
	builder.Get("vm_prov_remove", &v.provRemove)
	builder.Get("vm_ready_summary", &v.readySummary)
	builder.Get("vm_ready_memory", &v.readyMemory)
	builder.Get("vm_ready_cpus", &v.readyCPUs)
	builder.Get("vm_disk_size", &v.diskSize)
	builder.Get("vm_ready_save", &v.readySave)
	builder.Get("vm_start", &v.start)
	builder.Get("vm_remove", &v.remove)
	builder.Get("vm_remove_data", &v.removeData)
	builder.Get("vm_boot_progress", &v.bootProgress)
	builder.Get("vm_boot_progress_text", &v.bootProgressText)
	builder.Get("vm_boot_console", &v.bootConsole)
	builder.Get("vm_boot_stop", &v.bootStop)
	builder.Get("vm_run_summary", &v.runSummary)
	builder.Get("vm_connect", &v.connect)
	builder.Get("vm_console_ready", &v.consoleReady)
	builder.Get("vm_stop", &v.stop)
	builder.Close()
	v.onClick = func(b gtk.Button) { v.clicked(b) }
	v.onISOChosen = func(_, result, _ uintptr) { v.isoChosen(result) }
	for _, b := range []*gtk.Button{&v.accept, &v.advancedToggle, &v.install, &v.iso, &v.isoClear,
		&v.console, &v.provStart, &v.provStop, &v.provRemove, &v.readySave, &v.start,
		&v.connect, &v.consoleReady, &v.stop, &v.remove, &v.removeData, &v.setupRemoveData,
		&v.bootConsole, &v.bootStop} {
		b.ConnectClicked(&v.onClick)
	}
	v.Update(manager.Snapshot())
	v.startWatch()
	v.win.Present()
	return v
}

// startWatch follows the guest while the window is open, so an install that
// takes minutes moves the page on its own.
func (v *vmWindow) startWatch() {
	ctx, cancel := context.WithCancel(context.Background())
	v.watchCancel = cancel
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		paint := func() {
			s := v.manager.Refresh()
			gtkx.Post(func() {
				if !v.closed {
					v.Update(s)
				}
			})
		}
		paint()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				paint()
			}
		}
	}()
}

// clicked dispatches the window's buttons by their id, one handler for all
// of them (puregotk makes one native trampoline per function).
func (v *vmWindow) clicked(b gtk.Button) {
	switch b.GetName() {
	case "advanced_toggle":
		v.advanced.SetRevealChild(!v.advanced.GetRevealChild())
	case "accept":
		v.acceptProfile()
	case "iso":
		v.chooseISO()
	case "iso_clear":
		v.isoPath = ""
		v.showSetup(v.manager.Snapshot())
	case "ready_save":
		v.run(func() error { return v.manager.Configure(v.readyConfig()) })
	case "install":
		v.run(func() error { return v.manager.Install(v.setupConfig()) })
	case "start":
		v.run(v.manager.Start)
	case "connect":
		display := v.display()
		v.run(func() error { return v.manager.Connect(display) })
	case "console", "console_ready", "boot_console":
		v.run(v.manager.Console)
	case "prov_start":
		v.run(v.manager.Start)
	case "prov_stop", "stop", "boot_stop":
		v.run(v.manager.Stop)
	case "remove", "prov_remove":
		if !v.removeArmed {
			v.removeArmed = true
			label := i18n.T("Confirm remove")
			v.remove.SetLabel(label)
			v.provRemove.SetLabel(label)
			return
		}
		v.removeArmed = false
		v.remove.SetLabel(i18n.T("Remove"))
		v.provRemove.SetLabel(i18n.T("Remove"))
		v.run(v.manager.Remove)
	case "remove_data", "setup_remove_data":
		if !v.dataArmed {
			v.dataArmed = true
			label := i18n.T("Confirm delete")
			v.removeData.SetLabel(label)
			v.setupRemoveData.SetLabel(label)
			return
		}
		v.dataArmed = false
		v.removeData.SetLabel(i18n.T("Delete"))
		v.setupRemoveData.SetLabel(i18n.T("Delete disk"))
		v.run(v.manager.RemoveData)
	case "fix":
		for _, t := range v.fixButtons {
			if t.btn.GoPointer() == b.GoPointer() {
				v.applyFix(t.remedy)
				return
			}
		}
	}
}

// applyFix runs a remedy. The polkit dialog it may open lives on the main
// thread, so the work goes to a goroutine (as the other actions do) and the
// result comes back to the window.
func (v *vmWindow) applyFix(r vm.Remedy) {
	if v.operationBusy {
		return
	}
	v.operationBusy = true
	go func() {
		_, relogin, err := v.manager.Fix(r)
		s := v.manager.Refresh()
		gtkx.Post(func() {
			v.operationBusy = false
			switch {
			case err != nil:
				v.fixNote = i18n.Tf("The fix did not work: %v", err)
			case relogin:
				// A process's group list is fixed when it starts, so the
				// change is real but this session cannot see it yet: say so
				// in the window and in a notification.
				v.fixNote = i18n.T("Done. Log out and back in for it to take effect.")
				v.askRelogin()
			default:
				v.fixNote = i18n.T("Done.")
			}
			v.Update(s)
		})
	}()
}

// display is the monitor the manager window is on: the screen on which its
// Connect opens FreeRDP, so the guest follows that screen's scale. Without a
// monitor to read, FreeRDP keeps its own resolution (rdpArgs).
func (v *vmWindow) display() *rdpDisplay {
	if v.win == nil {
		return nil
	}
	surface := v.win.GetSurface()
	if surface == nil {
		return nil
	}
	defer surface.Unref()
	gdkDisplay := surface.GetDisplay()
	if gdkDisplay == nil {
		return nil
	}
	defer gdkDisplay.Unref()
	mon := gdkDisplay.GetMonitorAtSurface(surface)
	if mon == nil {
		return nil
	}
	defer mon.Unref()
	var rect gdk.Rectangle
	mon.GetGeometry(&rect)
	return &rdpDisplay{Width: int(rect.Width), Height: int(rect.Height), Scale: float64(mon.GetScaleFactor())}
}

// askRelogin says, in a notification, that the change needs a fresh login.
// Logging out is the shell's to do — Hyprland 0.55 runs a Lua configuration
// and rejects the classic dispatcher, and the shell already knows which form
// its compositor takes — so this points at the control center's power row
// rather than ending the session from here.
func (v *vmWindow) askRelogin() {
	if v.c == nil || v.c.App == nil {
		return
	}
	n := gio.NewNotification(i18n.T("Log out to finish"))
	n.SetBody(i18n.T("The change takes effect the next time you log in."))
	v.c.App.SendNotification("vm-relogin", n)
	n.Unref()
}

// Update refreshes the window on the GTK main thread: it picks the page the
// guest's state calls for, fills the headline, and shows only the controls
// that can do something useful there.
func (v *vmWindow) Update(s VMSnapshot) {
	gtkx.Post(func() {
		if v.closed || v.win == nil {
			return
		}
		if v.operationBusy {
			s.Busy = true
		}
		page := s.phase()
		if page != v.lastPage {
			if page == vm.PageSetup {
				v.setupFilled = false
			}
			if page == vm.PageStopped {
				v.readyFilled = false
			}
			v.lastPage = page
		}
		v.stack.SetVisibleChildName(string(page))
		// A fix's outcome outranks the state line: it is what the user just
		// did, and a group change only takes hold at the next login. It stays
		// up until something else replaces it.
		if v.fixNote != "" {
			v.sub.SetText(v.fixNote)
		} else {
			v.sub.SetText(vmStateText(s))
		}

		switch page {
		case vm.PagePrereq:
			v.showChecks(s)
		case vm.PageSetup:
			v.showSetup(s)
		case vm.PageProvisioning:
			v.showProvisioning(s)
		case vm.PageStopped:
			v.showStopped(s)
		case vm.PageStarting:
			v.showStarting(s)
		case vm.PageRunning:
			v.showRunning(s)
		}
	})
}

// showChecks lists what the host is missing, one row each, in the user's
// language: the platform reports a reason, not a sentence.
func (v *vmWindow) showChecks(s VMSnapshot) {
	failed := s.failedChecks()
	if doctorItemsEqual(v.lastChecks, failed) {
		return
	}
	v.lastChecks = failed
	v.checkRefs.Release()
	v.fixButtons = nil
	gtkx.Clear(&v.checkRows)
	for _, c := range failed {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("row")

		mark := gtk.NewLabel("")
		mark.AddCssClass("mark")
		mark.SetValign(gtk.AlignCenterValue)
		gtkx.Append(row, &mark.Widget)

		text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		text.SetHexpand(true)
		text.SetValign(gtk.AlignCenterValue)
		name := gtk.NewLabel(vmCheckName(c.Name))
		name.AddCssClass("label")
		name.SetXalign(0)
		text2 := gtk.NewLabel(vmCheckReason(c.Name, c.Reason))
		text2.AddCssClass("sub")
		text2.SetXalign(0)
		text2.SetWrap(true)
		gtkx.Append(text, &name.Widget)
		gtkx.Append(text, &text2.Widget)
		gtkx.Append(row, &text.Widget)

		// What the user can do about it: a button when Inua can apply the
		// fix itself, the platform's instructions when it cannot.
		switch {
		case c.Remedy != nil:
			label := i18n.T("Fix")
			if c.Remedy.Group != "" {
				label = i18n.Tf("Add me to %s", c.Remedy.Group)
			}
			btn := gtk.NewButtonWithLabel(label)
			btn.AddCssClass("outlined")
			btn.SetValign(gtk.AlignCenterValue)
			btn.SetName("fix")
			v.fixButtons = append(v.fixButtons, fixTarget{btn: *btn, remedy: *c.Remedy})
			btn.ConnectClicked(&v.onClick)
			gtkx.Append(row, &btn.Widget)
		default:
			// A package to install, named for this distribution.
			if hint := vmPackageHint(c.Name, s.Family); hint != "" {
				hintLabel := gtk.NewLabel(hint)
				hintLabel.AddCssClass("sub")
				hintLabel.AddCssClass("vm-hint")
				hintLabel.SetValign(gtk.AlignCenterValue)
				gtkx.Append(row, &hintLabel.Widget)
			}
		}
		gtkx.Append(&v.checkRows, &row.Widget)
	}
}

// fixTarget ties a button to the remedy it applies.
type fixTarget struct {
	btn    gtk.Button
	remedy vm.Remedy
}

// vmPackageHint is how to install a missing tool on this distribution, as a
// line the user can copy. The window never runs a package manager: choosing
// packages and changing a system is the user's call, not a button's.
func vmPackageHint(check, family string) string {
	switch check {
	case "Secret Service":
		return i18n.T("Start inua-keyring, or install a Secret Service.")
	case "KVM":
		// The guest brings its own QEMU: no package makes /dev/kvm, the
		// processor's virtualization does.
		return i18n.T("Turn on virtualization (Intel VT-x or AMD-V) in the firmware settings.")
	}
	manager, pkg := vm.PackageFor(family, check)
	switch manager {
	case "nix":
		return i18n.Tf("Add %s to your NixOS configuration.", pkg)
	case "apt":
		return "sudo apt install " + pkg
	case "dnf":
		return "sudo dnf install " + pkg
	case "pacman":
		return "sudo pacman -S " + pkg
	}
	return ""
}

// vmCheckName is a check's name. The platform names them in English; these
// are the ones the window shows.
func vmCheckName(name string) string {
	switch name {
	case "Operating system":
		return i18n.T("Operating system")
	case "KVM":
		return i18n.T("Hardware virtualization (KVM)")
	case "Docker":
		return i18n.T("Docker")
	case "Docker Compose v2":
		return i18n.T("Docker Compose v2")
	case "FreeRDP":
		return i18n.T("FreeRDP")
	case "Browser opener":
		return i18n.T("Browser opener")
	case "Disk":
		return i18n.T("Disk space")
	case "TUN":
		return i18n.T("Network device (TUN)")
	case "Secret Service":
		return i18n.T("Secret Service")
	}
	return name
}

// vmCheckReason words why a check failed. The Docker socket is the one case
// whose reason has to carry a warning: the socket is there and this user may
// not use it, and the fix — joining the docker group — would give everything
// running as this user what amounts to root on this host. Inua explains it,
// shows the command for whoever decides they want it, and points at the two
// ways that do not widen the account's rights; the guide's later steps add a
// machine run with QEMU, which needs no Docker at all.
func vmCheckReason(name string, r vm.DoctorReason) string {
	if name == "Docker" && r == vm.ReasonDenied {
		return i18n.T("Your user may not use Docker's socket. Joining the docker group would let anything running as you control this machine as root, so Inua will not do it for you: run `sudo gpasswd -a $USER docker` yourself if you accept that, or use rootless Docker or Podman instead.")
	}
	switch r {
	case vm.ReasonMissing:
		return i18n.T("Not installed on this system.")
	case vm.ReasonDenied:
		return i18n.T("Installed, but your user may not use it.")
	case vm.ReasonStopped:
		return i18n.T("Installed, but not running.")
	case vm.ReasonUnsupported:
		return i18n.T("Windows VM needs Linux.")
	}
	return i18n.T("Could not be checked.")
}

// showSetup fills the page shown before a project exists: the profile to
// use, and the one action that means "install now".
func (v *vmWindow) showSetup(s VMSnapshot) {
	c := vm.DefaultConfig()
	c.MemoryMiB, c.CPUs, c.DiskGiB = s.Recommended.MemoryMiB, s.Recommended.CPUs, s.Recommended.DiskGiB
	if s.Config.Name != "" && (s.HasProject || s.HasDisk) {
		c = s.Config
	}
	if !v.setupFilled {
		v.memory.SetValue(float64(c.MemoryMiB))
		v.cpus.SetValue(float64(c.CPUs))
		v.disk.SetValue(float64(c.DiskGiB))
		v.isoPath = c.ISOPath
		v.setupFilled = true
	}
	v.setupSummary.SetText(vmProfileText(v.setupConfig()))
	v.profileDesc.SetText(i18n.T("A balanced profile leaves memory and CPU for your system."))
	v.showInstaller(s)
	v.setupDisk.SetVisible(s.HasDisk)
	v.accept.SetSensitive(!s.Busy)
	// An ISO that is gone cannot be installed from; the line above says so.
	v.install.SetSensitive(!s.Busy && (v.isoPath == "" || vm.ISOPresent(v.isoPath)))
	v.advancedToggle.SetSensitive(!s.Busy)
	v.setupRemoveData.SetSensitive(!s.Busy)
	v.iso.SetSensitive(!s.Busy && v.isoDialog == nil)
	v.isoClear.SetSensitive(!s.Busy)
}

// showInstaller says where Windows will come from, and what installing it
// takes from there.
func (v *vmWindow) showInstaller(s VMSnapshot) {
	v.isoClear.SetVisible(v.isoPath != "")
	switch {
	case v.isoPath == "":
		v.isoDesc.SetText(i18n.T("Windows 11, downloaded from Microsoft."))
		v.iso.SetLabel(i18n.T("Choose ISO…"))
	case vm.ISOPresent(v.isoPath):
		v.isoDesc.SetText(filepath.Base(v.isoPath))
		v.iso.SetLabel(i18n.T("Change…"))
	default:
		v.isoDesc.SetText(i18n.Tf("%s is not there any more.", filepath.Base(v.isoPath)))
		v.iso.SetLabel(i18n.T("Change…"))
	}
	switch {
	case s.HasDisk:
		v.setupNote.SetText(i18n.T("Files from a previous attempt are still here. Install reuses them; delete them to start over."))
	case v.isoPath != "":
		v.setupNote.SetText(i18n.T("Installs Windows from the ISO, which stays as it is; takes 20+ minutes."))
	default:
		v.setupNote.SetText(i18n.T("Downloads about 8 GB and installs Windows; takes 30+ minutes."))
	}
}

// chooseISO asks for the installer ISO, starting where downloads land.
func (v *vmWindow) chooseISO() {
	if v.isoDialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Choose a Windows ISO"))
	filter := gtk.NewFileFilter()
	filter.SetName(i18n.T("Disc images (ISO)"))
	filter.AddSuffix("iso")
	d.SetDefaultFilter(filter)
	filter.Unref()
	dir := filepath.Dir(v.isoPath)
	if v.isoPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, "Downloads")
		}
	}
	if dir != "" && dir != "." {
		f := gio.FileNewForPath(dir)
		d.SetInitialFolder(f)
		(&gobject.Object{Ptr: f.GoPointer()}).Unref()
	}
	v.isoDialog = d
	v.iso.SetSensitive(false)
	d.Open(&v.win.Window, nil, &v.onISOChosen, 0)
}

// isoChosen takes the dialog's answer: a file, or nothing when cancelled.
func (v *vmWindow) isoChosen(result uintptr) {
	d := v.isoDialog
	v.isoDialog = nil
	if d == nil {
		return
	}
	defer d.Unref()
	v.iso.SetSensitive(true)
	file, err := d.OpenFinish(&gio.AsyncResultBase{Ptr: result})
	if err != nil || file == nil {
		return // cancelled
	}
	path := file.GetPath()
	(&gobject.Object{Ptr: file.GoPointer()}).Unref()
	if path == "" || v.closed {
		return
	}
	v.isoPath = filepath.Clean(path)
	v.showSetup(v.manager.Snapshot())
}

// showProvisioning fills the page shown while there is a project whose
// Windows has not come up yet: the image is being pulled, Windows is
// installing, or the container is stopped in the middle of it. It offers the
// web console (the only view of the guest that works yet) and either
// starting the install or stopping it without losing the disk.
func (v *vmWindow) showProvisioning(s VMSnapshot) {
	if s.Progress > 0 {
		v.progress.SetFraction(s.Progress)
	} else {
		v.progress.Pulse()
	}
	switch {
	case s.ProgressText != "":
		v.progressText.SetText(s.ProgressText)
	case s.State == vm.StateStarting, s.State == vm.StateRunning:
		v.progressText.SetText(i18n.T("Windows is installing; this takes a while."))
	case s.State == vm.StateStopping:
		v.progressText.SetText(i18n.T("Stopping…"))
	default:
		v.progressText.SetText(i18n.T("The installation is paused. Continue to resume."))
	}
	up := s.State == vm.StateRunning || s.State == vm.StateStarting
	v.provStart.SetVisible(!up)
	v.provStop.SetVisible(up)
	v.provStart.SetSensitive(!s.Busy)
	v.provStop.SetSensitive(!s.Busy)
	v.console.SetSensitive(!s.Busy && up)
	v.provRemove.SetSensitive(!s.Busy && !up)
}

// showStopped fills the page shown when Windows is installed and off:
// start it, change RAM/CPU, or remove it.
func (v *vmWindow) showStopped(s VMSnapshot) {
	c := s.Config
	if c.Name == "" {
		c = vm.DefaultConfig()
	}
	v.readySummary.SetText(vmProfileText(c))
	if !v.readyFilled {
		v.readyMemory.SetValue(float64(c.MemoryMiB))
		v.readyCPUs.SetValue(float64(c.CPUs))
		v.readyFilled = true
	}
	v.diskSize.SetText(fmt.Sprintf("%d GiB", c.DiskGiB))
	v.start.SetSensitive(!s.Busy)
	v.readySave.SetSensitive(!s.Busy)
	v.remove.SetSensitive(!s.Busy)
	v.removeData.SetSensitive(!s.Busy)
}

// showStarting fills the page shown while an installed guest is coming up.
func (v *vmWindow) showStarting(s VMSnapshot) {
	if s.Progress > 0 {
		v.bootProgress.SetFraction(s.Progress)
	} else {
		v.bootProgress.Pulse()
	}
	if s.ProgressText != "" {
		v.bootProgressText.SetText(s.ProgressText)
	} else if s.State == vm.StateStopping {
		v.bootProgressText.SetText(i18n.T("Stopping…"))
	} else {
		v.bootProgressText.SetText(i18n.T("Windows is starting; ready in a minute or two."))
	}
	v.bootConsole.SetSensitive(!s.Busy)
	v.bootStop.SetSensitive(!s.Busy)
}

// showRunning fills the page shown while Windows is up: connect or stop.
func (v *vmWindow) showRunning(s VMSnapshot) {
	c := s.Config
	if c.Name == "" {
		c = vm.DefaultConfig()
	}
	v.runSummary.SetText(vmProfileText(c))
	v.connect.SetSensitive(!s.Busy)
	v.consoleReady.SetSensitive(!s.Busy)
	v.stop.SetSensitive(!s.Busy)
}

// vmStateText is the headline for a state, in the shell's voice.
func vmStateText(s VMSnapshot) string {
	if s.Err != nil {
		return i18n.Tf("Something went wrong: %s", s.Err.Error())
	}
	switch s.phase() {
	case vm.PagePrereq:
		return i18n.T("This system is missing something Windows needs")
	case vm.PageSetup:
		if s.HasDisk {
			return i18n.T("Files from a previous attempt are still here")
		}
		return i18n.T("Not installed yet")
	case vm.PageProvisioning:
		if s.State == vm.StateStopping {
			return i18n.T("Stopping")
		}
		if s.State == vm.StateStarting || s.State == vm.StateRunning {
			return i18n.T("Installing Windows")
		}
		return i18n.T("Installation paused")
	case vm.PageStarting:
		if s.State == vm.StateStopping {
			return i18n.T("Stopping")
		}
		return i18n.T("Windows is starting")
	case vm.PageRunning:
		return i18n.T("Windows is running")
	case vm.PageStopped:
		return i18n.T("Stopped")
	}
	return i18n.T("Windows VM")
}

// vmProfileText is the guest's resources, as the section meta reads them.
func vmProfileText(c vm.Config) string {
	return fmt.Sprintf("%s · %d CPU · %d GiB", humanMiB(c.MemoryMiB), c.CPUs, c.DiskGiB)
}

// humanMiB shows a memory size the way the other windows do.
func humanMiB(mib int) string {
	if mib >= 1024 {
		return fmt.Sprintf("%d GiB", mib/1024)
	}
	return fmt.Sprintf("%d MiB", mib)
}

// setupConfig is the profile as the setup page's editors show it.
func (v *vmWindow) setupConfig() vm.Config {
	c := vm.DefaultConfig()
	c.MemoryMiB = int(v.memory.GetValueAsInt())
	c.CPUs = int(v.cpus.GetValueAsInt())
	c.DiskGiB = int(v.disk.GetValueAsInt())
	c.ISOPath = v.isoPath
	return c
}

// readyConfig is the guest's configuration with the editable resources
// taken from the ready page; the disk keeps its configured size.
func (v *vmWindow) readyConfig() vm.Config {
	c := v.manager.Snapshot().Config
	if c.Name == "" {
		return vm.DefaultConfig()
	}
	c.MemoryMiB = int(v.readyMemory.GetValueAsInt())
	c.CPUs = int(v.readyCPUs.GetValueAsInt())
	return c
}

func (v *vmWindow) acceptProfile() {
	s := v.manager.Snapshot()
	v.memory.SetValue(float64(s.Recommended.MemoryMiB))
	v.cpus.SetValue(float64(s.Recommended.CPUs))
	v.disk.SetValue(float64(s.Recommended.DiskGiB))
	v.advanced.SetRevealChild(true)
	v.setupFilled = true
	v.setupSummary.SetText(vmProfileText(v.setupConfig()))
}

func (v *vmWindow) run(f func() error) {
	if v.closed || v.operationBusy {
		return
	}
	s := v.manager.Snapshot()
	if s.Busy {
		return
	}
	v.operationBusy = true
	s.Busy, s.Err = true, nil
	v.Update(s)
	go func() {
		err := f()
		s := v.manager.Refresh()
		if err != nil {
			s.Err = err
		}
		s.Busy = false
		gtkx.Post(func() { v.operationBusy = false; v.Update(s) })
	}()
}
