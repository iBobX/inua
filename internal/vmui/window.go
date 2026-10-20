package vmui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/vm"
)

// VMManager is the UI-facing boundary for the machines: the list, one
// machine's page, and the actions on it. Everything that reads a backend may
// block (a Docker call, a QMP round trip), so the window calls those off the
// GTK thread and paints what comes back.
type VMManager interface {
	// Machines is the list, from what is on disk. It reads no backend.
	Machines() []MachineView
	Faults() []vm.Fault
	DeleteFault(name string) error
	// Snapshot is one machine as it was last observed.
	Snapshot(name string) MachineView
	// Refresh reads one machine's backend and returns its view.
	Refresh(name string) MachineView
	// RefreshAll reads every machine and re-reads the host.
	RefreshAll() []MachineView
	// Templates are what a new machine can be made from.
	Templates() []vm.Template
	Create(templateID, name string) (vm.Machine, error)
	Save(vm.Machine) error
	Install(vm.Machine) error
	FetchISO(name string, media vm.ISOMedia) error
	CancelISO(name string)
	SetISOFilters(vm.ISOFilters)
	Start(name string) error
	Stop(name string) error
	Connect(name string, display *rdpDisplay) error
	Console(name string) error
	TeeSerialContext(ctx context.Context, name string) error
	Remove(name string) error
	DeleteDisk(name string) error
	Delete(name string) error
	Fix(vm.Remedy) (done string, relogin bool, err error)
}

// machineRow is one row of the machine list: what a repaint updates.
type machineRow struct {
	name  string
	title gtk.Label
	sub   gtk.Label
}

// templateRow is one row of the new-machine page.
type templateRow struct {
	id    string
	title gtk.Label
	sub   gtk.Label
}

// vmWindow is the manager: the machines on the left, the chosen one's page on
// the right, and the page that makes a new one.
type vmWindow struct {
	win     *gtk.ApplicationWindow
	manager VMManager
	c       *appui.Ctx // the app, for a notification the window raises

	title gtk.Label
	sub   gtk.Label

	stack gtk.Stack

	machineList  gtk.ListBox
	faultList    gtk.ListBox
	faultHeading gtk.Label
	faultScroll  gtk.ScrolledWindow
	newButton    gtk.Button
	views        []MachineView
	rows         []machineRow
	faults       []vm.Fault

	// a new machine
	templateList gtk.ListBox
	templateRows []templateRow
	nameEntry    gtk.Entry
	createButton gtk.Button
	createNote   gtk.Label
	createHint   string // the sentence Create shows until something goes wrong
	templateID   string
	templateName string // the template's own suggested name, to know a name was typed
	newOpen      bool

	// the page of a machine whose backend this build does not have
	naNote gtk.Label

	// the serial console of a machine that has one, and its debugger
	consoleOpen                            bool
	consoleBack                            gtk.Button
	consoleSummary, consoleHardware        gtk.Label
	consoleGDB                             gtk.Switch
	consoleCmd                             gtk.Label
	provConsoleNote, bootConsoleNote       gtk.Label
	readyConsoleNote                       gtk.Label
	consoleCopy, consoleStart, consoleStop gtk.Button
	faultError                             gtk.Label
	faultDelete                            gtk.Button
	serial                                 gtk.TextView
	serialBuffer                           *gtk.TextBuffer
	serialText                             string
	serialCancel                           context.CancelFunc
	consoleNotes                           []consoleNote
	onGDB                                  func(gtk.Switch, bool) bool
	onISOSearch                            func(gtk.Entry)
	onISOEntryChanged                      func(ptr, _ uintptr)
	onISODropChanged                       func(ptr, _, _ uintptr)
	onISOFilterSwitch                      func(gtk.Switch, bool) bool
	onISOSystem                            func(gtk.Button)
	filling                                bool // a repaint, not the user, is setting a switch

	// prereq
	checkRows                      gtk.Box
	assistantAsk, assistantRecheck gtk.Button
	assistantNote                  gtk.Label
	assistantHint                  string // what the row says by default

	// setup
	setupSummary                    gtk.Label
	profileDesc                     gtk.Label
	accept, advancedToggle, install gtk.Button
	advanced                        gtk.Revealer
	memory, cpus, disk              gtk.SpinButton
	setupDisk                       gtk.Box
	setupRemoveData, setupDelete    gtk.Button
	setupNote                       gtk.Label

	// the install images of the machine being set up: the list, the transfer
	// in flight, and the buttons the rows are built with
	setupMedia      gtk.Box
	mediaSource     gtk.Label
	mediaNote       gtk.Label
	mediaRec        gtk.Box
	mediaRows       gtk.Box
	mediaProgress   gtk.Box
	isoProgress     gtk.ProgressBar
	isoProgressText gtk.Label
	isoCancel       gtk.Button
	isoButtons      []isoTarget
	// The download list's own controls: what it is searched for, the platform
	// and architecture it is limited to, and the two switches that offer what
	// the guide's rule otherwise leaves out. The window owns the choice; the
	// service applies it where a machine's view is built.
	mediaFilters gtk.Box
	isoSearch    gtk.Entry
	isoPlatform  gtk.DropDown
	isoArch      gtk.DropDown
	isoPre       gtk.Switch
	isoEOL       gtk.Switch
	mediaCount   gtk.Label

	isoFilter      vm.ISOFilters   // what the controls say
	isoSystems     []ISOSystemRow  // the systems as they were built
	isoRecommended []ISOSystemRow  // the recommended ones, as they were built
	isoOpen        map[string]bool // the systems the user opened
	isoArchs       []string        // the architecture families the catalog holds
	isoRevealer    map[string]gtk.Revealer
	isoMark        map[string]gtk.Image

	// provisioning
	progress                                             gtk.ProgressBar
	progressText                                         gtk.Label
	console, provStart, provStop, provRemove, provDelete gtk.Button

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

	mu            sync.Mutex // the chosen machine, read by the window's watch
	selected      string
	closed        bool
	removeArmed   bool
	dataArmed     bool
	deleteArmed   bool
	setupFilled   bool
	readyFilled   bool
	operationBusy bool
	checkRefs     gtkx.Scope // the rows built for the current check list
	fixButtons    []fixTarget
	note          string // what the last action did, shown until the next one
	watchCancel   context.CancelFunc
	lastPage      vm.Page
	lastChecks    []vm.DoctorItem
	onClose       func(gtk.Window) bool
	onClick       func(gtk.Button)
	onRowSelect   func(gtk.ListBox, uintptr)
	onTemplate    func(gtk.ListBox, uintptr)
	onNameSubmit  func(gtk.Entry)
}

// openWindow builds the manager window from the app's layouts: a second start
// of inua-vm brings this one forward instead of making another (appui).
func openWindow(c *appui.Ctx, manager VMManager) *vmWindow {
	v := &vmWindow{manager: manager, c: c, win: &gtk.ApplicationWindow{}}
	builder := c.Load("vm.ui")
	builder.Get("vm", v.win)
	builder.Get("vm_title", &v.title)
	builder.Get("vm_sub", &v.sub)
	builder.Get("vm_stack", &v.stack)
	builder.Get("vm_machines", &v.machineList)
	builder.Get("vm_fault_heading", &v.faultHeading)
	builder.Get("vm_fault_scroll", &v.faultScroll)
	builder.Get("vm_faults", &v.faultList)
	builder.Get("vm_templates", &v.templateList)
	builder.Get("vm_new_name", &v.nameEntry)
	builder.Get("vm_create", &v.createButton)
	builder.Get("vm_create_note", &v.createNote)
	builder.Get("vm_na_note", &v.naNote)
	builder.Get("vm_assistant_ask", &v.assistantAsk)
	builder.Get("vm_assistant_recheck", &v.assistantRecheck)
	builder.Get("vm_assistant_note", &v.assistantNote)
	builder.Get("vm_console_back", &v.consoleBack)
	builder.Get("vm_console_summary", &v.consoleSummary)
	builder.Get("vm_console_hardware", &v.consoleHardware)
	builder.Get("vm_console_gdb", &v.consoleGDB)
	builder.Get("vm_fault_error", &v.faultError)
	builder.Get("vm_fault_delete", &v.faultDelete)
	builder.Get("vm_console_cmd", &v.consoleCmd)
	builder.Get("vm_console_copy", &v.consoleCopy)
	builder.Get("vm_console_start", &v.consoleStart)
	builder.Get("vm_console_stop", &v.consoleStop)
	builder.Get("vm_serial", &v.serial)
	builder.Get("vm_prov_console_note", &v.provConsoleNote)
	builder.Get("vm_boot_console_note", &v.bootConsoleNote)
	builder.Get("vm_ready_console_note", &v.readyConsoleNote)
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
	builder.Get("vm_setup_delete", &v.setupDelete)
	builder.Get("vm_setup_note", &v.setupNote)
	builder.Get("vm_setup_media", &v.setupMedia)
	builder.Get("vm_media_source", &v.mediaSource)
	builder.Get("vm_media_note", &v.mediaNote)
	builder.Get("vm_media_rows", &v.mediaRows)
	builder.Get("vm_media_recommended", &v.mediaRec)
	builder.Get("vm_media_progress", &v.mediaProgress)
	builder.Get("vm_iso_progress", &v.isoProgress)
	builder.Get("vm_iso_progress_text", &v.isoProgressText)
	builder.Get("vm_iso_cancel", &v.isoCancel)
	builder.Get("vm_media_filters", &v.mediaFilters)
	builder.Get("vm_iso_search", &v.isoSearch)
	builder.Get("vm_iso_platform", &v.isoPlatform)
	builder.Get("vm_iso_arch", &v.isoArch)
	builder.Get("vm_iso_prerelease", &v.isoPre)
	builder.Get("vm_iso_eol", &v.isoEOL)
	builder.Get("vm_media_count", &v.mediaCount)
	builder.Get("vm_progress", &v.progress)
	builder.Get("vm_progress_text", &v.progressText)
	builder.Get("vm_console", &v.console)
	builder.Get("vm_prov_start", &v.provStart)
	builder.Get("vm_prov_stop", &v.provStop)
	builder.Get("vm_prov_remove", &v.provRemove)
	builder.Get("vm_prov_delete", &v.provDelete)
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

	v.isoOpen = map[string]bool{}
	v.isoRevealer = map[string]gtk.Revealer{}
	v.isoMark = map[string]gtk.Image{}
	// The list opens on the guide's own list: the architecture this build runs.
	v.isoFilter = vm.ISOFilters{ArchFamily: vm.HostArchFamily()}
	manager.SetISOFilters(v.isoFilter)
	v.onClick = func(b gtk.Button) { v.clicked(b) }
	for _, b := range []*gtk.Button{&v.accept, &v.advancedToggle, &v.install,
		&v.console, &v.provStart, &v.provStop, &v.provRemove, &v.provDelete, &v.readySave, &v.start,
		&v.connect, &v.consoleReady, &v.stop, &v.remove, &v.removeData, &v.setupRemoveData,
		&v.setupDelete, &v.bootConsole, &v.bootStop, &v.newButton, &v.createButton,
		&v.consoleBack, &v.consoleCopy, &v.consoleStart, &v.consoleStop,
		&v.assistantAsk, &v.assistantRecheck, &v.isoCancel, &v.faultDelete} {
		b.ConnectClicked(&v.onClick)
	}
	v.onRowSelect = func(_ gtk.ListBox, ptr uintptr) {
		if ptr == 0 {
			return
		}
		row := gtk.ListBoxRowNewFromInternalPtr(ptr)
		if row == nil || row.GetName() == "" {
			return
		}
		v.choose(row.GetName())
	}
	v.machineList.ConnectRowSelected(&v.onRowSelect)
	v.faultList.ConnectRowSelected(&v.onRowSelect)
	v.onTemplate = func(_ gtk.ListBox, ptr uintptr) {
		if ptr == 0 {
			return
		}
		if row := gtk.ListBoxRowNewFromInternalPtr(ptr); row != nil {
			v.chooseTemplate(row.GetName())
		}
	}
	v.templateList.ConnectRowSelected(&v.onTemplate)
	v.onNameSubmit = func(_ gtk.Entry) { v.createMachine() }
	v.nameEntry.ConnectActivate(&v.onNameSubmit)
	// The download list's controls: every one of them hands the window's own
	// filter to the service and paints again.
	v.onISOSearch = func(_ gtk.Entry) { v.applyISOFilters() }
	v.onISOEntryChanged = func(_, _ uintptr) { v.applyISOFilters() }
	v.onISODropChanged = func(_, _, _ uintptr) { v.applyISOFilters() }
	// A switch hands the state it is moving to: GtkSwitch's own handler has not
	// run yet, so reading the widget here would read the old one.
	v.onISOFilterSwitch = func(sw gtk.Switch, on bool) bool {
		if v.filling {
			return false
		}
		if sw.GoPointer() == v.isoPre.GoPointer() {
			v.isoFilter.IncludePrerelease = on
		} else {
			v.isoFilter.IncludeEnded = on
		}
		v.manager.SetISOFilters(v.isoFilter)
		v.paintAll(v.manager.Machines())
		return false
	}
	v.isoSearch.ConnectActivate(&v.onISOSearch)
	gtkx.ConnectChanged(&v.isoSearch, &v.onISOEntryChanged)
	gtkx.ConnectNotify(&v.isoPlatform, "selected", &v.onISODropChanged)
	gtkx.ConnectNotify(&v.isoArch, "selected", &v.onISODropChanged)
	v.isoPre.ConnectStateSet(&v.onISOFilterSwitch)
	v.isoEOL.ConnectStateSet(&v.onISOFilterSwitch)
	v.onISOSystem = func(b gtk.Button) {
		id := strings.TrimPrefix(b.GetName(), "iso_system:")
		if id == "" {
			return
		}
		v.openISOSystem(id, !v.isoOpen[id])
	}
	{
		names := make([]string, 0, len(isoPlatformChoices))
		for _, p := range isoPlatformChoices {
			names = append(names, isoPlatformText(p))
		}
		v.isoPlatform.SetModel(gtk.NewStringList(names))
	}
	v.onGDB = func(_ gtk.Switch, on bool) bool {
		// A repaint sets the switch to what the machine says; only the user's
		// own click writes it back.
		if v.filling {
			return false
		}
		m := v.viewOf(v.selectedName()).Machine
		if m.Boot.GDB == on {
			return false
		}
		m.Boot.GDB = on
		v.run(func() error { return v.manager.Save(m) })
		return false
	}
	v.consoleGDB.ConnectStateSet(&v.onGDB)
	// What the console rows say by default, so a machine whose console is its
	// serial one can be worded differently and the others put back.
	v.consoleNotes = []consoleNote{
		{label: &v.provConsoleNote, byDefault: v.provConsoleNote.GetText()},
		{label: &v.bootConsoleNote, byDefault: v.bootConsoleNote.GetText()},
		{label: &v.readyConsoleNote, byDefault: v.readyConsoleNote.GetText()},
	}
	v.serialBuffer = v.serial.GetBuffer()
	v.onClose = func(gtk.Window) bool {
		v.stopSerial()
		v.watchCancel()
		return false
	}
	v.win.ConnectCloseRequest(&v.onClose)

	v.createHint = v.createNote.GetText()
	v.assistantHint = v.assistantNote.GetText()
	v.fillTemplates()
	v.paintAll(manager.Machines())
	v.startWatch()
	v.win.Present()
	return v
}

// startWatch follows the chosen machine while the window is open, and the
// whole list less often, so an install that takes minutes moves the page on
// its own and a machine started elsewhere shows up here.
func (v *vmWindow) startWatch() {
	ctx, cancel := context.WithCancel(context.Background())
	v.watchCancel = cancel
	go func() {
		fast := time.NewTicker(2 * time.Second)
		slow := time.NewTicker(10 * time.Second)
		defer fast.Stop()
		defer slow.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-fast.C:
				if name := v.selectedName(); name != "" {
					view := v.manager.Refresh(name)
					gtkx.Post(func() {
						if v.closed {
							return
						}
						for i, x := range v.views {
							if x.Name == view.Name {
								v.views[i] = view
							}
						}
						v.updateRows()
						v.paint()
					})
				}
			case <-slow.C:
				views := v.manager.RefreshAll()
				gtkx.Post(func() {
					if !v.closed {
						v.paintAll(views)
					}
				})
			}
		}
	}()
}

// selectedName is the machine the panel shows. The watch reads it from its own
// goroutine while GTK callbacks write it, so it has a lock of its own.
func (v *vmWindow) selectedName() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.selected
}

func (v *vmWindow) setSelected(name string) {
	v.mu.Lock()
	v.selected = name
	v.mu.Unlock()
}

// choose shows a machine's page, reading its backend now rather than waiting
// for the next tick: the user just asked for it.
func (v *vmWindow) choose(name string) {
	if v.selectedName() == name && !v.newOpen {
		return
	}
	v.stopSerial()
	v.consoleOpen = false
	v.setSelected(name)
	v.note = ""
	v.rememberArmed(0)
	v.lastPage = ""
	v.setupFilled, v.readyFilled = false, false
	v.paint()
	name = v.selectedName()
	go func() {
		view := v.manager.Refresh(name)
		gtkx.Post(func() {
			if v.closed || v.selectedName() != name {
				return
			}
			for i, x := range v.views {
				if x.Name == view.Name {
					v.views[i] = view
				}
			}
			v.updateRows()
			v.paint()
		})
	}()
}

func (v *vmWindow) paintAll(views []MachineView) {
	if v.closed || v.win == nil {
		return
	}
	v.views = views
	v.faults = v.manager.Faults()
	visible := len(v.faults) > 0
	if v.faultList != (gtk.ListBox{}) {
		v.faultList.SetVisible(visible)
	}
	if v.faultScroll != (gtk.ScrolledWindow{}) {
		v.faultScroll.SetVisible(visible)
	}
	if v.faultHeading != (gtk.Label{}) {
		v.faultHeading.SetVisible(visible)
	}
	selected := v.selectedName()
	if !v.has(selected) && !v.hasFault(selected) {
		selected = ""
		if len(views) > 0 && !v.newOpen {
			selected = views[0].Name
		} else if len(v.faults) > 0 && !v.newOpen {
			selected = v.faults[0].Name
		}
		v.lastPage, v.setupFilled, v.readyFilled = "", false, false
	}
	v.setSelected(selected)
	v.rebuildRows()
	v.rebuildFaultRows()
	v.paint()
}

func (v *vmWindow) has(name string) bool {
	if name == "" {
		return false
	}
	for _, x := range v.views {
		if x.Name == name {
			return true
		}
	}
	return false
}

func (v *vmWindow) hasFault(name string) bool {
	for _, fault := range v.faults {
		if fault.Name == name {
			return true
		}
	}
	return false
}

func (v *vmWindow) rebuildFaultRows() {
	for {
		row := v.faultList.GetRowAtIndex(0)
		if row == nil {
			break
		}
		v.faultList.Remove(&row.Widget)
	}
	for _, fault := range v.faults {
		row := gtk.NewListBoxRow()
		row.SetName(fault.Name)
		label := gtk.NewLabel(fault.Name + " · " + fault.Error)
		label.SetXalign(0)
		label.SetEllipsize(pango.EllipsizeEndValue)
		row.SetChild(&label.Widget)
		v.faultList.Append(&row.Widget)
	}
}

func (v *vmWindow) viewOf(name string) MachineView {
	for _, x := range v.views {
		if x.Name == name {
			return x
		}
	}
	return MachineView{Name: name}
}

// rebuildRows makes a row per machine, and only when the machines themselves
// changed: a plain repaint updates the labels in place, which is what the
// watch does every two seconds.
func (v *vmWindow) rebuildRows() {
	same := len(v.rows) == len(v.views)
	if same {
		for i, r := range v.rows {
			if r.name != v.views[i].Name {
				same = false
				break
			}
		}
	}
	if same {
		v.updateRows()
		return
	}
	for {
		row := v.machineList.GetRowAtIndex(0)
		if row == nil {
			break
		}
		v.machineList.Remove(&row.Widget)
	}
	v.rows = nil
	for _, x := range v.views {
		row, built := v.buildRow(x)
		v.machineList.Append(&row.Widget)
		v.rows = append(v.rows, built)
	}
	v.selectRow()
}

// updateRows brings the rows' labels up to date.
func (v *vmWindow) updateRows() {
	for i := range v.rows {
		if i >= len(v.views) {
			break
		}
		x := v.views[i]
		v.rows[i].title.SetText(x.Name)
		v.rows[i].sub.SetText(vmListSub(x))
	}
}

// buildRow is one machine in the list: its system's mark, its name, and what
// it is and is doing under it.
func (v *vmWindow) buildRow(x MachineView) (gtk.ListBoxRow, machineRow) {
	box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	mark := gtk.NewImageFromIconName(vmOSIcon(x.Machine.OS))
	mark.AddCssClass("vm-os-mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(box, &mark.Widget)

	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	title := gtk.NewLabel(x.Name)
	title.AddCssClass("vm-row-name")
	title.SetXalign(0)
	sub := gtk.NewLabel(vmListSub(x))
	sub.AddCssClass("vm-row-sub")
	sub.SetXalign(0)
	sub.SetEllipsize(pango.EllipsizeEndValue)
	gtkx.Append(text, &title.Widget)
	gtkx.Append(text, &sub.Widget)
	gtkx.Append(box, &text.Widget)

	row := gtk.NewListBoxRow()
	row.SetName(x.Name)
	row.SetChild(&box.Widget)
	return *row, machineRow{name: x.Name, title: *title, sub: *sub}
}

// selectRow marks the chosen machine in the list.
func (v *vmWindow) selectRow() {
	name := v.selectedName()
	for i, r := range v.rows {
		if r.name == name {
			if row := v.machineList.GetRowAtIndex(int32(i)); row != nil {
				v.machineList.SelectRow(row)
			}
			return
		}
	}
}

// paint picks the page and fills it. It runs on the GTK thread and reads only
// what is already in hand: no backend is touched here.
func (v *vmWindow) paint() {
	if v.closed || v.win == nil {
		return
	}
	// Making a new machine is a page of its own, and it outranks one that is
	// merely chosen: the user just asked for it.
	if v.newOpen {
		v.title.SetText(i18n.T("New machine"))
		v.sub.SetText(i18n.T("Pick what the new machine runs."))
		v.stack.SetVisibleChildName("new")
		return
	}
	name := v.selectedName()
	if name == "" {
		v.title.SetText(i18n.T("Virtual machines"))
		v.sub.SetText(i18n.T("Machines keep running with this window closed."))
		v.stack.SetVisibleChildName("empty")
		return
	}
	if v.hasFault(name) {
		v.title.SetText(name)
		v.sub.SetText(i18n.T("Machine folder needs recovery"))
		for _, fault := range v.faults {
			if fault.Name == name {
				v.faultError.SetText(fault.Error)
				break
			}
		}
		v.faultDelete.SetSensitive(!v.operationBusy)
		v.stack.SetVisibleChildName("fault")
		return
	}
	view := v.viewOf(name)
	if !view.Ready {
		v.sub.SetText(i18n.T("No backend for this machine"))
		v.naNote.SetText(i18n.Tf("This machine needs the %s backend, which this build does not have.", vmBackendText(view.Machine.Backend)))
		v.stack.SetVisibleChildName("unavailable")
		return
	}
	if v.note != "" {
		v.sub.SetText(v.note)
	} else {
		v.sub.SetText(vmStateText(view))
	}
	v.setConsoleRow(view)
	if v.consoleOpen {
		v.showConsolePage(view)
		return
	}
	if view.Page != v.lastPage {
		if view.Page == vm.PageSetup {
			v.setupFilled = false
		}
		if view.Page == vm.PageStopped {
			v.readyFilled = false
		}
		v.lastPage = view.Page
	}
	v.stack.SetVisibleChildName(string(view.Page))
	switch view.Page {
	case vm.PagePrereq:
		v.showChecks(view)
	case vm.PageSetup:
		v.showSetup(view)
	case vm.PageProvisioning:
		v.showProvisioning(view)
	case vm.PageStopped:
		v.showStopped(view)
	case vm.PageStarting:
		v.showStarting(view)
	case vm.PageRunning:
		v.showRunning(view)
	}
}

// consoleNote is a sentence under a console button: what it says by default,
// and the label it is written into.
type consoleNote struct {
	label     *gtk.Label
	byDefault string
}

// showConsole opens the console page of the machine that is chosen.
func (v *vmWindow) showConsole() {
	if v.selectedName() == "" {
		return
	}
	v.consoleOpen = true
	v.paint()
}

// showConsolePage fills the console page: what the machine will get, its
// debugger, and what it has printed.
func (v *vmWindow) showConsolePage(s MachineView) {
	v.consoleSummary.SetText(vmProfileText(s.Machine))
	v.consoleHardware.SetText(vmHardwareText(s))
	v.filling = true
	v.consoleGDB.SetActive(s.Machine.Boot.GDB)
	v.filling = false
	line := s.DebuggerCmd
	v.startSerial()
	if line == "" {
		line = i18n.T("Turn the debugger on to hold the guest for gdb.")
	}
	v.consoleCmd.SetText(line)
	v.consoleCopy.SetSensitive(s.DebuggerCmd != "")
	v.consoleStart.SetSensitive(!s.Busy && s.State != vm.StateRunning && s.State != vm.StateStarting)
	v.consoleStop.SetSensitive(!s.Busy && (s.State == vm.StateRunning || s.State == vm.StateStarting))
	v.setSerial(s.SerialLog)
}

func (v *vmWindow) startSerial() {
	name := v.selectedName()
	view := v.viewOf(name)
	if view.Machine.Backend != vm.BackendQEMU || view.Display.Kind != vm.DisplaySerial ||
		view.State != vm.StateRunning && view.State != vm.StateStarting {
		v.stopSerial()
		return
	}
	if v.serialCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.serialCancel = cancel
	go func() {
		if err := v.manager.TeeSerialContext(ctx, name); err != nil && ctx.Err() == nil {
			log.Printf("vm serial %s: %v", name, err)
		}
	}()
}

func (v *vmWindow) stopSerial() {
	if v.serialCancel != nil {
		v.serialCancel()
		v.serialCancel = nil
	}
}

// setConsoleRow words the console rows for this machine: a machine whose
// display is its serial console has no browser console to offer.
func (v *vmWindow) setConsoleRow(s MachineView) {
	serial := s.Display.Kind == vm.DisplaySerial
	label := i18n.T("Open web console")
	note := ""
	if serial {
		label = i18n.T("Serial console")
		note = i18n.T("What this system prints, on its own serial console.")
	}
	for _, b := range []*gtk.Button{&v.console, &v.consoleReady, &v.bootConsole} {
		b.SetLabel(label)
		b.SetTooltipText(label)
	}
	for _, n := range v.consoleNotes {
		if note == "" {
			n.label.SetText(n.byDefault)
			continue
		}
		n.label.SetText(note)
	}
}

// setSerial shows what the guest has printed, and keeps the view at the end of
// it: a console is read from the bottom.
func (v *vmWindow) setSerial(text string) {
	if v.serialBuffer == nil || text == v.serialText {
		return
	}
	v.serialText = text
	v.serialBuffer.SetText(text, int32(len(text)))
	var end gtk.TextIter
	v.serialBuffer.GetEndIter(&end)
	v.serial.ScrollToIter(&end, 0, false, 0, 0)
}

// copyDebuggerLine puts the attach line on the clipboard, so it can be pasted
// into a terminal: the guide's "shown and copied".
func (v *vmWindow) copyDebuggerLine() {
	line := v.consoleCmd.GetText()
	if line == "" || v.manager == nil {
		return
	}
	display := gdk.DisplayGetDefault()
	if display == nil {
		return
	}
	clipboard := display.GetClipboard()
	if clipboard == nil {
		return
	}
	clipboard.SetText(line)
	v.note = i18n.T("The command is on your clipboard.")
	v.paint()
}

// vmHardwareText is what the guest will get on this host: whether it runs on
// the processor or is emulated, and what its graphics are.
func vmHardwareText(s MachineView) string {
	parts := []string{}
	if text := vmAccelerationText(s); text != "" {
		parts = append(parts, text)
	}
	parts = append(parts, vmGraphicsText(s.Machine))
	return strings.Join(parts, " · ")
}

// vmAccelerationText says Accelerated or Emulated, and why. The reasons are the
// backend's stable identifiers; the sentences are the window's.
func vmAccelerationText(s MachineView) string {
	if s.Machine.Backend != vm.BackendQEMU {
		return ""
	}
	if s.Accelerated {
		return i18n.T("Accelerated")
	}
	switch s.AccelerationReason {
	case vm.ReasonRequestedEmulation:
		return i18n.T("Emulated (slow): this machine asks for emulation")
	case vm.ReasonOtherArchitecture:
		return i18n.T("Emulated (slow): the guest is built for another architecture")
	case vm.ReasonNoKVM:
		return i18n.T("Emulated (slow): this host has no /dev/kvm")
	case vm.ReasonKVMDenied:
		return i18n.T("Emulated (slow): your user may not open /dev/kvm")
	}
	return i18n.T("Emulated (slow)")
}

// vmGraphicsText is the display the guest gets: the guide's table, in the
// window's words. A Windows guest has no accelerated driver, and a custom
// system has one only once it has written one.
func vmGraphicsText(m vm.Machine) string {
	if m.Backend != vm.BackendQEMU {
		return ""
	}
	if m.OS == vm.OSWindows {
		return i18n.T("2D: Windows has no accelerated driver here")
	}
	switch m.Display.GPU {
	case vm.GPUVirgl:
		return i18n.T("3D (OpenGL, through the host's GPU)")
	case vm.GPUVenus:
		return i18n.T("3D (Vulkan, through the host's GPU)")
	}
	return i18n.T("2D")
}

// fillTemplates fills the new-machine page, once: the templates do not change
// while the window is open.
func (v *vmWindow) fillTemplates() {
	templates := v.manager.Templates()
	v.templateRows = nil
	for _, t := range templates {
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		mark := gtk.NewImageFromIconName(vmOSIcon(t.OS))
		mark.AddCssClass("vm-os-mark")
		mark.SetValign(gtk.AlignCenterValue)
		gtkx.Append(box, &mark.Widget)

		text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		text.SetHexpand(true)
		text.SetValign(gtk.AlignCenterValue)
		title := gtk.NewLabel(vmTemplateName(t))
		title.AddCssClass("vm-row-name")
		title.SetXalign(0)
		sub := gtk.NewLabel(vmTemplateText(t))
		sub.AddCssClass("vm-template-sub")
		sub.SetXalign(0)
		sub.SetWrap(true)
		gtkx.Append(text, &title.Widget)
		gtkx.Append(text, &sub.Widget)
		gtkx.Append(box, &text.Widget)

		row := gtk.NewListBoxRow()
		row.SetName(t.ID)
		row.SetChild(&box.Widget)
		v.templateList.Append(&row.Widget)
		v.templateRows = append(v.templateRows, templateRow{id: t.ID, title: *title, sub: *sub})
	}
	if len(templates) > 0 {
		v.chooseTemplate(templates[0].ID)
	}
}

// showNew opens the new-machine page.
func (v *vmWindow) showNew() {
	v.newOpen = true
	v.note = ""
	if v.templateID == "" && len(v.templateRows) > 0 {
		v.chooseTemplate(v.templateRows[0].id)
	}
	v.paint()
}

// chooseTemplate picks the template a new machine starts from, and offers the
// name that template usually takes.
func (v *vmWindow) chooseTemplate(id string) {
	t, ok := vm.TemplateByID(id)
	if !ok {
		return
	}
	v.templateID = id
	for i, r := range v.templateRows {
		if r.id == id {
			if row := v.templateList.GetRowAtIndex(int32(i)); row != nil {
				v.templateList.SelectRow(row)
			}
		}
	}
	v.createNote.RemoveCssClass("error")
	v.createNote.SetText(v.createHint)
	if want := t.Name; v.nameEntry.GetText() == "" || v.nameEntry.GetText() == v.templateName {
		v.nameEntry.SetText(want)
	}
	v.templateName = t.Name
}

// createMachine writes the new machine and shows it: nothing is installed or
// started here, so the new machine's page says what it needs next.
func (v *vmWindow) createMachine() {
	if v.operationBusy {
		return
	}
	id, name := v.templateID, v.nameEntry.GetText()
	if id == "" {
		return
	}
	v.operationBusy = true
	go func() {
		m, err := v.manager.Create(id, name)
		views := v.manager.Machines()
		gtkx.Post(func() {
			v.operationBusy = false
			if err != nil {
				v.createNote.SetText(i18n.Tf("That name cannot be used: %v", err))
				v.createNote.AddCssClass("error")
				return
			}
			v.createNote.RemoveCssClass("error")
			v.createNote.SetText(v.createHint)
			v.newOpen = false
			v.setSelected(m.Name)
			v.lastPage, v.setupFilled, v.readyFilled = "", false, false
			v.paintAll(views)
		})
	}()
}

// showChecks lists what the host is missing, one row each, in the user's
// language: the platform reports a reason, not a sentence.
func (v *vmWindow) showChecks(s MachineView) {
	failed := failedChecks(s.Checks)
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
		reason := gtk.NewLabel(vmCheckReason(c.Name, c.Reason))
		reason.AddCssClass("sub")
		reason.SetXalign(0)
		reason.SetWrap(true)
		gtkx.Append(text, &name.Widget)
		gtkx.Append(text, &reason.Widget)
		gtkx.Append(row, &text.Widget)

		// What the user can do about it: a button when Inua can apply the fix
		// itself, the platform's instructions when it cannot.
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
			if hint := vmPackageHint(c.Name, s.Distribution); hint != "" {
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

// failedChecks are the host checks that did not pass.
func failedChecks(checks []vm.DoctorItem) []vm.DoctorItem {
	var out []vm.DoctorItem
	for _, c := range checks {
		if !c.OK {
			out = append(out, c)
		}
	}
	return out
}

// vmPackageHint is how to install a missing tool on this distribution, as a
// line the user can copy. The window never runs a package manager: choosing
// packages and changing a system is the user's call, not a button's.
func vmPackageHint(check, distro string) string {
	if check == "Secret Service" {
		return i18n.T("Start inua-keyring, or install a Secret Service.")
	}
	if check == "Operating system" {
		return i18n.T("This manager needs NixOS, Ubuntu or Debian.")
	}
	plan := vm.PlanPackages(distro)
	pkg := ""
	for _, name := range plan.Packages {
		switch check {
		case "Docker", "Docker Compose v2":
			if strings.Contains(strings.ToLower(name), "docker") {
				pkg = name
			}
		case "FreeRDP":
			if strings.Contains(strings.ToLower(name), "freerdp") {
				pkg = name
			}
		case "Browser opener":
			if strings.Contains(name, "xdg") {
				pkg = name
			}
		case "KVM":
			if strings.Contains(strings.ToLower(name), "qemu") || strings.Contains(name, "kvm") {
				pkg = name
			}
		case "QEMU":
			if strings.Contains(strings.ToLower(name), "qemu") {
				pkg = name
			}
		case "Firmware (UEFI)":
			if strings.Contains(strings.ToLower(name), "ovmf") || strings.Contains(strings.ToLower(name), "edk2") {
				pkg = name
			}
		case "Waydroid":
			if strings.Contains(strings.ToLower(name), "waydroid") {
				pkg = name
			}
		}
		if pkg != "" {
			break
		}
	}
	if pkg == "" {
		return ""
	}
	switch plan.PackageManager {
	case "nix":
		return i18n.Tf("Add %s to your NixOS configuration.", pkg)
	case "apt":
		return "sudo apt install " + pkg
	}
	return i18n.T("Install") + ": " + pkg
}

// vmCheckName is a check's name. The platform names them in English; these are
// the ones the window shows.
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
	case "QEMU":
		return "QEMU"
	case "Firmware (UEFI)":
		return i18n.T("UEFI firmware")
	case "Waydroid":
		return "Waydroid"
	case "Binder":
		return i18n.T("Binder (the kernel's Android support)")
	}
	return name
}

// vmCheckReason words why a check failed. The Docker socket is the one case
// whose reason has to carry a warning: the socket is there and this user may
// not use it, and the fix — joining the docker group — would give everything
// running as this user what amounts to root on this host. Inua explains it,
// shows the command for whoever decides they want it, and points at the two
// ways that do not widen the account's rights; the QEMU backend needs no
// Docker at all.
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
	case vm.ReasonTooOld:
		return i18n.T("This Linux distribution is not supported.")
	}
	return i18n.T("Could not be checked.")
}

// fixTarget ties a button to the remedy it applies.
type fixTarget struct {
	btn    gtk.Button
	remedy vm.Remedy
}

// showSetup fills the page shown before a machine's guest exists: the profile
// to give it, and the one action that means "install now".
func (v *vmWindow) showSetup(s MachineView) {
	m := s.Machine
	if !v.setupFilled {
		recommended := s.Recommended
		if m.MemoryMiB == 0 {
			m.MemoryMiB, m.CPUs, m.DiskGiB = recommended.MemoryMiB, recommended.CPUs, recommended.DiskGiB
		}
		v.memory.SetValue(float64(m.MemoryMiB))
		v.cpus.SetValue(float64(m.CPUs))
		v.disk.SetValue(float64(m.DiskGiB))
		v.setupFilled = true
	}
	v.setupSummary.SetText(vmProfileText(v.setupMachine()))
	v.profileDesc.SetText(i18n.T("A balanced profile leaves memory and CPU for your system."))
	if s.Machine.Backend == vm.BackendQEMU && strings.TrimSpace(s.Machine.Boot.ISO) == "" && strings.TrimSpace(s.Machine.Boot.Kernel) == "" {
		v.setupNote.SetText(i18n.T("Choose or download an install image before installing this machine."))
	} else if s.Leftovers {
		v.setupNote.SetText(i18n.T("Files from a previous attempt are still here. Install reuses them; delete them to start over."))
	} else {
		v.setupNote.SetText(vmInstallNote(s))
	}
	v.setupDisk.SetVisible(s.Leftovers)
	idle := !s.Busy && !v.operationBusy
	canInstall := s.Machine.Backend != vm.BackendQEMU || strings.TrimSpace(s.Machine.Boot.ISO) != "" ||
		strings.TrimSpace(s.Machine.Boot.Kernel) != ""
	v.accept.SetSensitive(idle)
	v.install.SetSensitive(idle && canInstall)
	v.advancedToggle.SetSensitive(idle)
	v.setupRemoveData.SetSensitive(idle)
	v.setupDelete.SetSensitive(idle)
	v.showSetupMedia(s)
}

// isoTarget ties a row's button to the medium it downloads, as a remedy's
// button is tied to its remedy: one handler serves every button in the window.
type isoTarget struct {
	btn   gtk.Button
	media vm.ISOMedia
}

// showSetupMedia fills the install-image section of the setup page: what the
// machine installs from, the list to choose from, and the download in flight.
// A machine whose backend fetches its own media has no such section.
func (v *vmWindow) showSetupMedia(s MachineView) {
	if s.ISO == nil {
		v.setupMedia.SetVisible(false)
		return
	}
	v.setupMedia.SetVisible(true)
	v.mediaSource.SetText(isoSourceText(s.ISO.Source))
	v.mediaNote.SetText(vmMediaNote(s))

	if s.ISO.Downloading {
		v.mediaProgress.SetVisible(true)
		if s.ISO.Total > 0 {
			v.isoProgress.SetFraction(float64(s.ISO.Done) / float64(s.ISO.Total))
			v.isoProgressText.SetText(i18n.Tf("%s of %s downloaded", isoBytesText(s.ISO.Done), isoBytesText(s.ISO.Total)))
		} else {
			// The server has not said how large the file is, as Boxes'
			// indeterminate bar does in the same case.
			v.isoProgress.Pulse()
			v.isoProgressText.SetText(i18n.Tf("%s downloaded", isoBytesText(s.ISO.Done)))
		}
		v.isoCancel.SetSensitive(true)
	} else {
		v.mediaProgress.SetVisible(false)
	}
	v.showISOFilters(s.ISO)
	v.mediaCount.SetText(isoCountText(s.ISO.SystemsCount, s.ISO.MediaCount))
	v.fillISOSystems(s.ISO.Recommended, s.ISO.Systems)
}

// ── the download list ───────────────────────────────────────────────────────

// showISOFilters puts the filter controls in the state the window's own filter
// says: what the user last chose. It runs while filling, so that setting a
// control does not read as the user changing it.
func (v *vmWindow) showISOFilters(iso *ISOView) {
	v.filling = true
	defer func() { v.filling = false }()

	if !slices.Equal(v.isoArchs, iso.ArchFamilies) {
		v.isoArchs = append(v.isoArchs[:0], iso.ArchFamilies...)
		names := make([]string, 0, len(v.isoArchs)+1)
		names = append(names, i18n.T("Every architecture"))
		for _, a := range v.isoArchs {
			names = append(names, isoArchText(a))
		}
		v.isoArch.SetModel(gtk.NewStringList(names))
	}
	if v.isoSearch.GetText() != v.isoFilter.Search {
		v.isoSearch.SetText(v.isoFilter.Search)
	}
	v.isoPlatform.SetSelected(uint32(isoPlatformChoice(v.isoFilter.Platform)))
	v.isoArch.SetSelected(uint32(isoArchChoice(v.isoArchs, v.isoFilter.ArchFamily)))
	v.isoPre.SetActive(v.isoFilter.IncludePrerelease)
	v.isoEOL.SetActive(v.isoFilter.IncludeEnded)
}

// isoPlatformChoice is the family drop-down's index for a platform: 0 is every
// platform, and the rest follow the list the control is built with.
func isoPlatformChoice(platform string) int {
	for i, p := range isoPlatformChoices {
		if p == platform {
			return i
		}
	}
	return 0
}

// isoArchChoice is the architecture drop-down's index for a family: 0 is every
// architecture and the families the catalog holds follow, so a family the
// catalog does not hold is never offered and never selected.
func isoArchChoice(families []string, family string) int {
	for i, f := range families {
		if f == family {
			return i + 1
		}
	}
	return 0
}

// isoPlatformChoices are the platform filter's values: every platform first,
// then the four the catalog places systems in.
var isoPlatformChoices = []string{
	"", vm.ISOPlatformLinux, vm.ISOPlatformBSD, vm.ISOPlatformWindows,
	vm.ISOPlatformOther,
}

// isoPlatformText names a platform for the filter.
func isoPlatformText(platform string) string {
	switch platform {
	case "":
		return i18n.T("Every platform")
	case vm.ISOPlatformLinux:
		return i18n.T("Linux")
	case vm.ISOPlatformBSD:
		return i18n.T("BSD")
	case vm.ISOPlatformWindows:
		return i18n.T("Windows")
	}
	return i18n.T("Other")
}

// isoArchText names an architecture family for the filter. The families are the
// database's own words - x86, arm64, riscv64 - and need no translation.
func isoArchText(family string) string {
	switch family {
	case vm.ISOArchFamilyX86:
		return "x86"
	case vm.ISOArchFamilyARM:
		return "arm64"
	}
	return family
}

// isoPlatformIcon is a system's mark in the list, from the platform it belongs
// to: the shell installs a mark for each of the machines' systems and they fit
// these four exactly.
func isoPlatformIcon(platform string) string {
	switch platform {
	case vm.ISOPlatformLinux:
		return "vm-os-linux-symbolic"
	case vm.ISOPlatformBSD:
		return "vm-os-freebsd-symbolic"
	case vm.ISOPlatformWindows:
		return "vm-os-windows-symbolic"
	}
	return "vm-os-custom-symbolic"
}

// isoMonthText is the month a system was released in - "2026-04" - which is
// what a row says beside its name instead of a date nobody reads.
func isoMonthText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01")
}

// isoMediaRowTitle is a medium's own line inside its system: the variant the
// database names, or the file's own name when it names none. The system's name
// is the row above, and three installers of one system are told apart by their
// files - the minimal, the dvd, the boot one - rather than by repeating it.
func isoMediaRowTitle(m vm.ISOMedia) string {
	if m.Variant != "" {
		return m.Variant
	}
	if base := path.Base(strings.TrimSpace(m.URL)); base != "" && base != "." && base != "/" {
		return base
	}
	if m.Live {
		return i18n.T("Live image")
	}
	return i18n.T("Installer")
}

// isoSystemSummary is what a system's row says under its name: what the filters
// left of it - how many images install it, how many are live images, and when
// it was released.
func isoSystemSummary(s vm.ISOSystem) string {
	var parts []string
	if n := s.Installers(); n > 0 {
		parts = append(parts, i18n.Nf("%d installer", "%d installers", n, n))
	}
	if n := s.Live(); n > 0 {
		parts = append(parts, i18n.Nf("%d live image", "%d live images", n, n))
	}
	if !s.Newest.IsZero() {
		parts = append(parts, i18n.Tf("newest %s", isoMonthText(s.Newest)))
	}
	if len(parts) == 0 {
		return i18n.Nf("%d install image", "%d install images", len(s.Media), len(s.Media))
	}
	return strings.Join(parts, " · ")
}

// isoCountText is the section's own line: what the filters left, counted the
// way the list is arranged - systems first, then the images inside them.
func isoCountText(systems, media int) string {
	return i18n.Tf("%s · %s", i18n.Nf("%d system", "%d systems", systems, systems),
		i18n.Nf("%d install image", "%d install images", media, media))
}

// fillISOSystems builds the download list: one row per system, which opens to
// its own media. The rows are the window's slowest thing to make and the list
// only changes with the filters and the files on disk, so they are rebuilt when
// that is what changed and left alone otherwise.
func (v *vmWindow) fillISOSystems(recommended, systems []ISOSystemRow) {
	if isoSystemsSame(v.isoRecommended, recommended) && isoSystemsSame(v.isoSystems, systems) {
		return
	}
	v.isoRecommended = append(v.isoRecommended[:0], recommended...)
	v.isoSystems = append(v.isoSystems[:0], systems...)
	v.isoButtons = nil
	fill := func(box *gtk.Box, list []ISOSystemRow) {
		gtkx.Clear(box)
		for _, s := range list {
			gtkx.Append(box, &v.buildISOSystem(s).Widget)
		}
	}
	fill(&v.mediaRec, v.isoRecommended)
	fill(&v.mediaRows, v.isoSystems)
}

// isoSystemsSame says whether two lists of systems would build the same
// widgets: the same systems in the same order, with the same media rows.
func isoSystemsSame(a, b []ISOSystemRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].System.OSID != b[i].System.OSID {
			return false
		}
		if !isoRowsSame(a[i].Rows, b[i].Rows) {
			return false
		}
	}
	return true
}

// buildISOSystem is one system's row: its mark, its name, what it holds, and a
// button that opens and closes the media inside it. The open state lives in the
// window, so a repaint does not close what the user opened.
func (v *vmWindow) buildISOSystem(s ISOSystemRow) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	box.AddCssClass("vm-iso-system")
	open := v.isoOpen[s.System.OSID]

	head := gtk.NewButton()
	head.AddCssClass("plain")
	head.AddCssClass("vm-iso-system-head")
	head.SetName("iso_system:" + s.System.OSID)
	head.ConnectClicked(&v.onISOSystem)

	headBox := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	mark := gtk.NewImageFromIconName(isoPlatformIcon(s.System.Platform))
	mark.AddCssClass("vm-os-mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(headBox, &mark.Widget)

	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	title := gtk.NewLabel(s.System.OSName)
	title.AddCssClass("label")
	title.SetXalign(0)
	sub := gtk.NewLabel(isoSystemSummary(s.System))
	sub.AddCssClass("sub")
	sub.SetXalign(0)
	sub.SetEllipsize(pango.EllipsizeEndValue)
	gtkx.Append(text, &title.Widget)
	gtkx.Append(text, &sub.Widget)
	gtkx.Append(headBox, &text.Widget)

	chevron := gtk.NewImageFromIconName("pan-end-symbolic")
	chevron.AddCssClass("vm-iso-chevron")
	chevron.SetValign(gtk.AlignCenterValue)
	if open {
		chevron.SetFromIconName("pan-down-symbolic")
	}
	gtkx.Append(headBox, &chevron.Widget)
	head.SetChild(&headBox.Widget)
	v.isoMark[s.System.OSID] = *chevron
	gtkx.Append(box, &head.Widget)

	revealer := gtk.NewRevealer()
	revealer.SetTransitionType(gtk.RevealerTransitionTypeSlideDownValue)
	revealer.SetRevealChild(open)
	body := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	body.AddCssClass("vm-iso-media")
	for _, r := range s.Rows {
		gtkx.Append(body, &v.isoMediaRow(r).Widget)
	}
	revealer.SetChild(&body.Widget)
	v.isoRevealer[s.System.OSID] = *revealer
	gtkx.Append(box, &revealer.Widget)
	return box
}

// isoMediaRow is one medium inside an opened system: the variant's own name,
// what the medium is - architecture, live image or installer, its size when the
// database knows it, how the file already stands here - and the one action it
// allows.
func (v *vmWindow) isoMediaRow(r ISORow) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	row.AddCssClass("row")
	row.AddCssClass("vm-iso-media-row")

	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	title := gtk.NewLabel(isoMediaRowTitle(r.Media))
	title.AddCssClass("label")
	title.SetXalign(0)
	sub := gtk.NewLabel(isoMediaSub(r))
	sub.AddCssClass("sub")
	sub.SetXalign(0)
	sub.SetWrap(true)
	gtkx.Append(text, &title.Widget)
	gtkx.Append(text, &sub.Widget)
	// A download that failed says so in the downloader's own words: it is
	// where the explanation is - how much room is missing, which answer
	// the server gave - and a bare "failed" would throw it away.
	if r.Err != nil {
		reason := gtk.NewLabel(r.Err.Error())
		reason.AddCssClass("vm-media-error")
		reason.SetXalign(0)
		reason.SetWrap(true)
		gtkx.Append(text, &reason.Widget)
	}
	gtkx.Append(row, &text.Widget)

	btn := gtk.NewButtonWithLabel(isoActionText(r))
	btn.AddCssClass("outlined")
	btn.SetValign(gtk.AlignCenterValue)
	btn.SetName("iso_fetch")
	btn.SetSensitive(!r.Downloading)
	v.isoButtons = append(v.isoButtons, isoTarget{btn: *btn, media: r.Media})
	btn.ConnectClicked(&v.onClick)
	gtkx.Append(row, &btn.Widget)
	return row
}

// openISOSystem opens or closes one system's media. The media rows are built
// with the system, so this is only the revealer and the chevron.
func (v *vmWindow) openISOSystem(id string, open bool) {
	v.isoOpen[id] = open
	if r, ok := v.isoRevealer[id]; ok {
		r.SetRevealChild(open)
	}
	if m, ok := v.isoMark[id]; ok {
		name := "pan-end-symbolic"
		if open {
			name = "pan-down-symbolic"
		}
		m.SetFromIconName(name)
	}
	if !open {
		delete(v.isoOpen, id)
	}
}

// isoFiltersFromControls is what the filter controls currently say.
func (v *vmWindow) isoFiltersFromControls() vm.ISOFilters {
	f := v.isoFilter
	f.Search = v.isoSearch.GetText()
	if i := int(v.isoPlatform.GetSelected()); i >= 0 && i < len(isoPlatformChoices) {
		f.Platform = isoPlatformChoices[i]
	}
	// A fresh GtkStringList has no selection until GTK has chosen one, and its
	// invalid position must leave the filter as it is rather than reading as
	// "every architecture"; index 0 is the "every" entry and the rest are the
	// families the catalog holds.
	switch i := int(v.isoArch.GetSelected()); {
	case i > 0 && i <= len(v.isoArchs):
		f.ArchFamily = v.isoArchs[i-1]
	case i == 0:
		f.ArchFamily = ""
	}
	f.IncludePrerelease = v.isoPre.GetActive()
	f.IncludeEnded = v.isoEOL.GetActive()
	return f
}

// applyISOFilters hands the window's own filter to the service and paints
// again: the list is built where a machine's view is built, so the choice
// travels through the service rather than being applied to the widgets.
func (v *vmWindow) applyISOFilters() {
	if v.filling {
		return
	}
	v.isoFilter = v.isoFiltersFromControls()
	v.manager.SetISOFilters(v.isoFilter)
	v.paintAll(v.manager.Machines())
}

// isoRowsSame says whether two lists of rows would build the same widgets: the
// same media in the same order, and the same state of their files.
func isoRowsSame(a, b []ISORow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Media.URL != y.Media.URL || x.Cached != y.Cached || x.Partial != y.Partial ||
			x.Checksum != y.Checksum || x.Downloading != y.Downloading || (x.Err == nil) != (y.Err == nil) {
			return false
		}
	}
	return true
}

// showProvisioning fills the page shown while a machine's guest is being
// installed: the image is being pulled, the system is installing, or it is
// stopped in the middle of it.
func (v *vmWindow) showProvisioning(s MachineView) {
	if s.Progress > 0 {
		v.progress.SetFraction(s.Progress)
	} else {
		v.progress.Pulse()
	}
	switch {
	case s.ProgressText != "":
		v.progressText.SetText(s.ProgressText)
	case s.State == vm.StateStarting, s.State == vm.StateRunning:
		v.progressText.SetText(i18n.T("The system is installing; this takes a while."))
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
	v.provDelete.SetSensitive(!s.Busy && !up && !v.operationBusy)
}

// showStopped fills the page shown when the guest is installed and off: start
// it, change its memory or CPU count, remove it, or delete its files.
func (v *vmWindow) showStopped(s MachineView) {
	m := s.Machine
	v.readySummary.SetText(vmProfileText(m))
	if !v.readyFilled {
		v.readyMemory.SetValue(float64(m.MemoryMiB))
		v.readyCPUs.SetValue(float64(m.CPUs))
		v.readyFilled = true
	}
	v.diskSize.SetText(fmt.Sprintf("%d GiB", m.DiskGiB))
	v.start.SetSensitive(!s.Busy)
	v.readySave.SetSensitive(!s.Busy)
	v.remove.SetSensitive(!s.Busy)
	v.removeData.SetSensitive(!s.Busy)
}

// showStarting fills the page shown while an installed guest is coming up.
func (v *vmWindow) showStarting(s MachineView) {
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
		v.bootProgressText.SetText(i18n.T("The system is starting; ready in a minute or two."))
	}
	v.bootConsole.SetSensitive(!s.Busy)
	v.bootStop.SetSensitive(!s.Busy)
}

// showRunning fills the page shown while the guest is up: connect or stop.
func (v *vmWindow) showRunning(s MachineView) {
	v.runSummary.SetText(vmProfileText(s.Machine))
	v.connect.SetSensitive(!s.Busy)
	v.consoleReady.SetSensitive(!s.Busy)
	v.stop.SetSensitive(!s.Busy)
}

// clicked dispatches the window's buttons by their id, one handler for all of
// them (puregotk makes one native trampoline per function).
func (v *vmWindow) clicked(b gtk.Button) {
	name := v.selectedName()
	switch b.GetName() {
	case "new_machine":
		v.showNew()
	case "create_machine":
		v.createMachine()
	case "advanced_toggle":
		v.advanced.SetRevealChild(!v.advanced.GetRevealChild())
	case "accept":
		v.acceptProfile()
	case "ready_save":
		v.run(func() error { return v.manager.Save(v.readyMachine()) })
	case "install":
		m := v.setupMachine()
		v.run(func() error { return v.manager.Install(m) })
	case "iso_fetch":
		media, ok := v.isoMediaOf(b)
		if ok {
			v.fetchISO(media)
		}
	case "iso_cancel":
		// Stopping is a cancellation, not an operation: it only reaches the
		// goroutine that is downloading, so the window is free to keep
		// painting while it happens.
		v.manager.CancelISO(name)
	case "start", "prov_start":
		v.run(func() error { return v.manager.Start(name) })
	case "connect":
		// A machine whose only screen is its serial console has no viewer to
		// open: the window's own console page is where it is seen.
		if view := v.viewOf(name); view.Display.Kind == vm.DisplaySerial {
			v.showConsole()
			return
		}
		display := v.display()
		v.run(func() error { return v.manager.Connect(name, display) })
	case "console", "console_ready", "boot_console":
		if view := v.viewOf(name); view.Display.Kind == vm.DisplaySerial {
			v.showConsole()
			return
		}
		v.run(func() error { return v.manager.Console(name) })
	case "console_back":
		v.stopSerial()
		v.consoleOpen = false
		v.paint()
	case "console_copy":
		v.copyDebuggerLine()
	case "console_start":
		v.run(func() error { return v.manager.Start(name) })
	case "console_stop":
		v.run(func() error { return v.manager.Stop(name) })
	case "prov_stop", "stop", "boot_stop":
		v.run(func() error { return v.manager.Stop(name) })
	case "remove", "prov_remove":
		if !v.removeArmed {
			v.rememberArmed(1)
			label := i18n.T("Confirm remove")
			v.remove.SetLabel(label)
			v.provRemove.SetLabel(label)
			return
		}
		v.rememberArmed(0)
		v.remove.SetLabel(i18n.T("Remove"))
		v.provRemove.SetLabel(i18n.T("Remove"))
		v.run(func() error { return v.manager.Remove(name) })
	case "remove_data":
		if !v.dataArmed {
			v.rememberArmed(2)
			v.removeData.SetLabel(i18n.T("Confirm delete"))
			return
		}
		v.rememberArmed(0)
		v.removeData.SetLabel(i18n.T("Delete disk"))
		v.run(func() error { return v.manager.DeleteDisk(name) })
	case "setup_remove_data":
		if !v.dataArmed {
			v.rememberArmed(2)
			v.setupRemoveData.SetLabel(i18n.T("Confirm delete"))
			return
		}
		v.rememberArmed(0)
		v.setupRemoveData.SetLabel(i18n.T("Delete disk"))
		v.run(func() error { return v.manager.DeleteDisk(name) })
	case "setup_delete":
		if !v.deleteArmed {
			v.rememberArmed(3)
			v.setupDelete.SetLabel(i18n.T("Confirm delete"))
			return
		}
		v.rememberArmed(0)
		v.setupDelete.SetLabel(i18n.T("Delete"))
		v.run(func() error { return v.manager.Delete(name) })
	case "prov_delete":
		if !v.deleteArmed {
			v.rememberArmed(3)
			v.setupDelete.SetLabel(i18n.T("Delete"))
			v.provDelete.SetLabel(i18n.T("Confirm delete"))
			return
		}
		v.rememberArmed(0)
		v.provDelete.SetLabel(i18n.T("Delete"))
		v.run(func() error { return v.manager.Delete(name) })
	case "fault_delete":
		if !v.deleteArmed {
			v.rememberArmed(3)
			v.faultDelete.SetLabel(i18n.T("Confirm delete"))
			return
		}
		v.rememberArmed(0)
		v.faultDelete.SetLabel(i18n.T("Delete folder"))
		v.run(func() error { return v.manager.DeleteFault(name) })
	case "assistant_ask":
		v.askAboutTheHost()
	case "assistant_recheck":
		v.recheck()
	case "fix":
		for _, t := range v.fixButtons {
			if t.btn.GoPointer() == b.GoPointer() {
				v.applyFix(t.remedy)
				return
			}
		}
	}
}

// rememberArmed keeps one destructive action armed at a time: arming a second
// disarms the first, so "Confirm delete" is never left waiting somewhere the
// user is not looking.
func (v *vmWindow) rememberArmed(which int) {
	if v.removeArmed {
		v.remove.SetLabel(i18n.T("Remove"))
		v.provRemove.SetLabel(i18n.T("Remove"))
	}
	if v.dataArmed {
		v.removeData.SetLabel(i18n.T("Delete disk"))
		v.setupRemoveData.SetLabel(i18n.T("Delete disk"))
	}
	if v.deleteArmed {
		v.setupDelete.SetLabel(i18n.T("Delete"))
		v.provDelete.SetLabel(i18n.T("Delete"))
		v.faultDelete.SetLabel(i18n.T("Delete folder"))
	}
	v.removeArmed = which == 1
	v.dataArmed = which == 2
	v.deleteArmed = which == 3
}

// askAboutTheHost hands the host's missing pieces to inua's own assistant: the
// window runs no package manager and no privileged install, and what the
// assistant changes is its own, approved, decision. The prompt is built here,
// where the machine and the checks are, and the assistant's window - running
// or started by this - is where it goes.
func (v *vmWindow) askAboutTheHost() {
	name := v.selectedName()
	if name == "" {
		return
	}
	prompt := assistantPrompt(v.viewOf(name))
	if err := askAssistant(prompt); err != nil {
		v.assistantNote.SetText(err.Error())
		v.assistantNote.AddCssClass("error")
	} else {
		v.assistantNote.RemoveCssClass("error")
		v.assistantNote.SetText(i18n.T("Request sent to the assistant window."))
	}
	v.paint()
}

// recheck reads the host again, so a fix the assistant made shows up here: the
// page moves on by itself once every check this machine needs passes.
func (v *vmWindow) recheck() {
	if v.closed || v.operationBusy {
		return
	}
	v.operationBusy = true
	v.paint()
	go func() {
		views := v.manager.RefreshAll()
		gtkx.Post(func() {
			v.operationBusy = false
			v.note = ""
			v.paintAll(views)
		})
	}()
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
		views := v.manager.RefreshAll()
		gtkx.Post(func() {
			v.operationBusy = false
			switch {
			case err != nil:
				v.note = i18n.Tf("The fix did not work: %v", err)
			case relogin:
				// A process's group list is fixed when it starts, so the
				// change is real but this session cannot see it yet: say so in
				// the window and in a notification.
				v.note = i18n.T("Done. Log out and back in for it to take effect.")
				v.askRelogin()
			default:
				v.note = i18n.T("Done.")
			}
			v.paintAll(views)
		})
	}()
}

// run performs a machine's action off the GTK thread and repaints what it left
// behind: the state of every machine may have moved (a start, a removal).
func (v *vmWindow) run(f func() error) {
	name := v.selectedName()
	if v.closed || v.operationBusy || name == "" {
		return
	}
	v.operationBusy = true
	v.paint()
	go func() {
		err := f()
		views := v.manager.RefreshAll()
		gtkx.Post(func() {
			v.operationBusy = false
			if err != nil {
				v.note = i18n.Tf("That did not work: %v", err)
			} else {
				v.note = ""
			}
			v.paintAll(views)
		})
	}()
}

// fetchISO downloads an install medium and hands the machine over to the
// install flow: the service does the transfer off the GTK thread, and the same
// watch that follows a machine's state follows its download, so a repaint
// every two seconds shows how far it has got. The transfer is not an action in
// the window's own sense - it is stopped by its own button, not cancelled by a
// click elsewhere - so the two are kept apart.
func (v *vmWindow) fetchISO(media vm.ISOMedia) {
	name := v.selectedName()
	if v.closed || v.operationBusy || name == "" {
		return
	}
	v.operationBusy = true
	v.paint()
	go func() {
		err := v.manager.FetchISO(name, media)
		views := v.manager.RefreshAll()
		gtkx.Post(func() {
			v.operationBusy = false
			switch {
			case err == nil:
				v.note = ""
			case errors.Is(err, context.Canceled):
				// The user's own stop is not a failure: the part file is
				// still there, and the row offers to resume it.
				v.note = i18n.T("Stopped.")
			default:
				v.note = i18n.Tf("That did not work: %v", err)
			}
			v.paintAll(views)
		})
	}()
}

// isoMediaOf is the medium a download button stands for.
func (v *vmWindow) isoMediaOf(b gtk.Button) (vm.ISOMedia, bool) {
	for _, t := range v.isoButtons {
		if t.btn.GoPointer() == b.GoPointer() {
			return t.media, true
		}
	}
	return vm.ISOMedia{}, false
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
// Logging out is the shell's to do — Hyprland 0.55 runs a Lua configuration and
// rejects the classic dispatcher, and the shell already knows which form its
// compositor takes — so this points at the control center's power row rather
// than ending the session from here.
func (v *vmWindow) askRelogin() {
	if v.c == nil || v.c.App == nil {
		return
	}
	n := gio.NewNotification(i18n.T("Log out to finish"))
	n.SetBody(i18n.T("The change takes effect the next time you log in."))
	v.c.App.SendNotification("vm-relogin", n)
	n.Unref()
}

// setupMachine is the chosen machine with the setup page's editors applied.
func (v *vmWindow) setupMachine() vm.Machine {
	m := v.viewOf(v.selectedName()).Machine
	m.MemoryMiB = int(v.memory.GetValueAsInt())
	m.CPUs = int(v.cpus.GetValueAsInt())
	m.DiskGiB = int(v.disk.GetValueAsInt())
	return m
}

// readyMachine is the chosen machine with the stopped page's editors applied.
// The disk keeps its size: it is the one field that cannot change under an
// installed guest.
func (v *vmWindow) readyMachine() vm.Machine {
	m := v.viewOf(v.selectedName()).Machine
	m.MemoryMiB = int(v.readyMemory.GetValueAsInt())
	m.CPUs = int(v.readyCPUs.GetValueAsInt())
	return m
}

func (v *vmWindow) acceptProfile() {
	s := v.viewOf(v.selectedName())
	v.memory.SetValue(float64(s.Recommended.MemoryMiB))
	v.cpus.SetValue(float64(s.Recommended.CPUs))
	v.disk.SetValue(float64(s.Recommended.DiskGiB))
	v.advanced.SetRevealChild(true)
	v.setupFilled = true
	v.setupSummary.SetText(vmProfileText(v.setupMachine()))
}

// vmStateText is the headline under the machine's name: what it is doing.
func vmStateText(s MachineView) string {
	if s.Err != nil {
		return i18n.Tf("Something went wrong: %s", s.Err.Error())
	}
	switch s.Page {
	case vm.PagePrereq:
		return i18n.T("This system is missing something the guest needs")
	case vm.PageSetup:
		if s.Leftovers {
			return i18n.T("Files from a previous attempt are still here")
		}
		return i18n.T("Not installed yet")
	case vm.PageProvisioning:
		if s.State == vm.StateStopping {
			return i18n.T("Stopping")
		}
		if s.State == vm.StateStarting || s.State == vm.StateRunning {
			return i18n.T("Installing")
		}
		return i18n.T("Installation paused")
	case vm.PageStarting:
		if s.State == vm.StateStopping {
			return i18n.T("Stopping")
		}
		return i18n.T("Starting")
	case vm.PageRunning:
		return i18n.T("Running")
	case vm.PageStopped:
		return i18n.T("Stopped")
	}
	return i18n.T("Virtual machines")
}

// vmListSub is a machine's line in the list: what it is, and what it is doing.
func vmListSub(s MachineView) string {
	if !s.Ready {
		return i18n.Tf("%s · no backend in this build", vmOSText(s.Machine.OS))
	}
	return vmOSText(s.Machine.OS) + " · " + vmShortState(s)
}

// vmShortState is one or two words for the list, where the headline's sentence
// would not fit.
func vmShortState(s MachineView) string {
	if s.Busy {
		return i18n.T("Working")
	}
	switch s.Page {
	case vm.PagePrereq:
		return i18n.T("Needs the host")
	case vm.PageSetup:
		return i18n.T("Not installed")
	case vm.PageProvisioning:
		if s.State == vm.StateStopping {
			return i18n.T("Stopping")
		}
		return i18n.T("Installing")
	case vm.PageStarting:
		return i18n.T("Starting")
	case vm.PageRunning:
		return i18n.T("Running")
	case vm.PageStopped:
		return i18n.T("Stopped")
	}
	return i18n.T("Unknown")
}

// vmOSText names a system the way the window says it. The systems' own names
// are proper nouns and stay as they are: the catalog already translates the
// word "windows" as the thing a program draws in, which is not this.
func vmOSText(os vm.OS) string {
	switch os {
	case vm.OSWindows:
		return "Windows"
	case vm.OSLinux:
		return "Linux"
	case vm.OSFreeBSD:
		return "FreeBSD"
	case vm.OSAndroid:
		return "Android"
	case vm.OSCustom:
		return i18n.T("Custom system")
	}
	return string(os)
}

// vmBackendText names a backend. QEMU, Dockur and Waydroid are their own names
// too, and only the phrase around them is translated.
func vmBackendText(b vm.BackendKind) string { return string(b) }

// vmOSIcon is a system's mark, from the icons the shell installs.
func vmOSIcon(os vm.OS) string {
	switch os {
	case vm.OSWindows:
		return "vm-os-windows-symbolic"
	case vm.OSLinux:
		return "vm-os-linux-symbolic"
	case vm.OSFreeBSD:
		return "vm-os-freebsd-symbolic"
	case vm.OSAndroid:
		return "vm-os-android-symbolic"
	}
	return "vm-os-custom-symbolic"
}

// vmInstallNote says what installing this machine costs, which is not the same
// for a container that downloads Windows and a system booted off an ISO.
func vmInstallNote(s MachineView) string {
	switch s.Machine.Backend {
	case vm.BackendDockur:
		return i18n.T("Downloads about 8 GB and installs Windows; takes 30+ minutes. Windows needs a licence.")
	default:
		return i18n.T("The guest installs from its own media, as it would on a real machine.")
	}
}

// isoSourceText names where the download list came from. osinfo-db is a data
// package's own name and stays as it is; a list inua wrote itself is said so,
// because it is short and ages.
func isoSourceText(source string) string {
	if source == vm.ISOSourceBuiltin {
		return i18n.T("inua's own list")
	}
	return "osinfo-db"
}

// vmMediaNote is the sentence above the download list: what the machine
// installs from, or what to do while there is nothing yet.
func vmMediaNote(s MachineView) string {
	if iso := strings.TrimSpace(s.Machine.Boot.ISO); iso != "" {
		return i18n.Tf("Installs from %s.", iso)
	}
	if s.ISO != nil && s.ISO.Source == vm.ISOSourceBuiltin {
		return i18n.T("osinfo-db is not installed, so this is inua's short built-in list.")
	}
	return i18n.T("Choose an install image to download; the machine boots it as a disc.")
}

// isoMediaTitle is a medium's own line: the system, and the variant when the
// database names one that adds something to the system's name.
func isoMediaTitle(m vm.ISOMedia) string {
	if m.Variant == "" || m.Variant == m.OSName {
		return m.OSName
	}
	return m.OSName + " · " + m.Variant
}

// isoMediaSub is what a row says under the medium: the architecture, whether
// it is a live image or an installer, its size when the database knows it, and
// how the file already stands here.
func isoMediaSub(r ISORow) string {
	parts := []string{r.Media.Arch}
	switch {
	case r.Media.Live:
		parts = append(parts, i18n.T("Live image"))
	case r.Media.Installer:
		parts = append(parts, i18n.T("Installer"))
	}
	if r.Media.VolumeSize > 0 {
		parts = append(parts, isoBytesText(r.Media.VolumeSize))
	}
	switch {
	case r.Cached:
		// An image that is here says how it got here: verified against a
		// published sum, with none published, or already in the cache from
		// before, which nothing was downloaded for and nothing verified.
		text := isoChecksumText(r.Checksum)
		if text == "" {
			text = i18n.T("Already in the cache")
		}
		parts = append(parts, text)
	case r.Partial:
		parts = append(parts, i18n.T("Unfinished download"))
	}
	return strings.Join(parts, " · ")
}

// isoActionText is what a row's button does next: download the image, resume
// an unfinished one, use the one already here, or try again after a failure.
func isoActionText(r ISORow) string {
	switch {
	case r.Err != nil:
		return i18n.T("Retry")
	case r.Cached:
		return i18n.T("Use this image")
	case r.Partial:
		return i18n.T("Resume")
	}
	return i18n.T("Download")
}

// isoChecksumText words how the published checksum of a finished download
// turned out. Most media publish none, which is not a failure: the file is
// trusted because it came from the distribution's own URL over HTTPS. An image
// that was already in the cache was not downloaded here and so was not checked.
func isoChecksumText(c vm.ISOChecksum) string {
	switch c {
	case vm.ISOChecksumVerified:
		return i18n.T("Checksum verified")
	case vm.ISOChecksumNotPublished:
		return i18n.T("No published checksum")
	case vm.ISOChecksumNotChecked:
		return i18n.T("Already in the cache")
	}
	return ""
}

// isoBytesText is a byte count the way a size reads on this page.
func isoBytesText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
}

// vmTemplateName is a template's title: the system it installs. The two
// Windows templates differ by how they install it, which their own line says.
func vmTemplateName(t vm.Template) string {
	switch t.ID {
	case "windows-dockur", "windows-qemu":
		return "Windows 11"
	case "ubuntu":
		return "Ubuntu"
	case "fedora":
		return "Fedora"
	case "arch":
		return "Arch Linux"
	case "freebsd":
		return "FreeBSD"
	case "android":
		return "Android"
	case "custom":
		return i18n.T("Custom system")
	}
	return vmOSText(t.OS)
}

// vmTemplateText says what a template gives, in one line.
func vmTemplateText(t vm.Template) string {
	switch t.ID {
	case "windows-dockur":
		return i18n.T("Windows 11, installed for you in a container.")
	case "windows-qemu":
		return i18n.T("Windows 11 from your own ISO, with the virtio drivers.")
	case "ubuntu":
		return i18n.T("Ubuntu, booted with QEMU from its ISO.")
	case "fedora":
		return i18n.T("Fedora, booted with QEMU from its ISO.")
	case "arch":
		return i18n.T("Arch Linux, booted with QEMU from its ISO.")
	case "freebsd":
		return i18n.T("FreeBSD, booted with QEMU from its ISO.")
	case "android":
		return i18n.T("Android in Waydroid, with the host's display and GPU.")
	case "custom":
		return i18n.T("Your own system: an ISO, a disk or a kernel, on any architecture.")
	}
	return string(t.Backend)
}

// vmProfileText is a machine's resources, as the section meta reads them.
func vmProfileText(m vm.Machine) string {
	return fmt.Sprintf("%s · %d CPU · %d GiB", humanMiB(m.MemoryMiB), m.CPUs, m.DiskGiB)
}

// humanMiB shows a memory size the way the other windows do.
func humanMiB(mib int) string {
	if mib >= 1024 {
		return fmt.Sprintf("%d GiB", mib/1024)
	}
	return fmt.Sprintf("%d MiB", mib)
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
