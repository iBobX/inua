package appui

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/apps/chart"
	"github.com/lyricat/inua/internal/apps/sysmon"
	"github.com/lyricat/inua/internal/gtkx"
)

// history is how many seconds the meters draw.
const history = 60

// coreWidth is the width of one core's bar in the overview.
const coreWidth = 40

// meter is one block of the overview: a name, a value, a line about it,
// and a chart of its last minute or a bar.
type meter struct {
	box        *gtk.Box
	value, sub *gtk.Label
	chart      *gtk.Picture
	bar, fill  *gtk.Box
	points     []float64
	lo, hi     float64 // the chart's fixed range, hi 0 to follow the points
}

// action is one way to end or hold a process, as the detail page offers
// it: a title and a line about it beside its button.
type action struct {
	key, title, sub, button string
	sig                     syscall.Signal
	confirm                 bool // a second click within a few seconds sends it
	danger                  bool
}

var actions = []action{
	{"quit", "Quit", "Asks it to quit. It can save its work first.", "Quit", syscall.SIGTERM, true, false},
	{"interrupt", "Interrupt", "As Ctrl+C in a terminal.", "Interrupt", syscall.SIGINT, true, false},
	{"hangup", "Hang Up", "As closing its terminal. Many services reload instead.", "Hang Up", syscall.SIGHUP, true, false},
	{"pause", "Pause", "Stops it running until it is resumed.", "Pause", syscall.SIGSTOP, false, false},
	{"kill", "Force Quit", "Ends it at once. Work it has not saved is lost.", "Force Quit", syscall.SIGKILL, true, true},
	{"tree", "Quit with Children", "", "Quit All", syscall.SIGTERM, true, false},
	{"named", "Quit All by Name", "", "Quit All", syscall.SIGTERM, true, false},
}

// actionRow is an action's row on the detail page.
type actionRow struct {
	box        *gtk.Box
	title, sub *gtk.Label
	button     *gtk.Button
}

// detailView is the open process's page: its facts by key, kept to be
// updated in place (a selection in them survives), and its actions.
type detailView struct {
	pid   int
	d     sysmon.Detail
	gone  bool
	facts map[string]*gtk.Label
	acts  map[string]*actionRow
	note  *gtk.Label
}

type sysmonApp struct {
	c                          *Ctx
	pages, procPages           gtk.Stack
	meters, procs, detail, sys gtk.Box
	procCount, procNote        gtk.Label
	detailName, detailSub      gtk.Label
	filter                     gtk.SearchEntry
	byMem, byName              gtk.ToggleButton
	cpu, mem, gpu, net, disk   *meter
	bat                        *meter
	coreFills                  []*gtk.Box
	foot                       *gtk.Label
	cpuInfo                    sysmon.CPUInfo
	lastCPU                    sysmon.CPUTimes
	lastCores                  []sysmon.CPUTimes
	lastNet                    sysmon.NetBytes
	lastDisk                   sysmon.DiskIO
	lastAt                     time.Time
	lastTicks                  map[int]uint64
	lastProcAt, lastSysAt      time.Time
	ps                         []sysmon.Process // the last reading
	open                       *detailView      // the process opened, nil for the list
	armed                      string           // "action:PID" waiting for its second click
	armedAt                    time.Time
	noteAt                     time.Time
	onTick                     glib.SourceFunc
	onSort                     func(gtk.ToggleButton)
	onQuit, onOpen, onAct      func(gtk.Button)
	onBack                     func(gtk.Button)
	onPage                     func(uintptr, uintptr, uintptr)
	onFilter, onStopFilter     func(gtk.SearchEntry)
	onKey                      func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	uid                        int
}

func (a *sysmonApp) build(c *Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.uid = os.Getuid()
	win := &gtk.ApplicationWindow{}
	var byCPU gtk.ToggleButton
	var back gtk.Button
	ui := c.Load("sysmon.ui")
	ui.Get("sysmon", win)
	ui.Get("pages", &a.pages)
	ui.Get("proc_pages", &a.procPages)
	ui.Get("meters", &a.meters)
	ui.Get("procs", &a.procs)
	ui.Get("detail", &a.detail)
	ui.Get("system", &a.sys)
	ui.Get("proc_count", &a.procCount)
	ui.Get("proc_note", &a.procNote)
	ui.Get("detail_name", &a.detailName)
	ui.Get("detail_sub", &a.detailSub)
	ui.Get("filter", &a.filter)
	ui.Get("by_cpu", &byCPU)
	ui.Get("by_mem", &a.byMem)
	ui.Get("by_name", &a.byName)
	ui.Get("back", &back)
	ui.Close()

	a.cpuInfo = sysmon.ReadCPUInfo()
	a.cpu = a.newMeter("CPU", true, 0, 100)
	a.buildCores()
	a.mem = a.newMeter("MEMORY", true, 0, 100)
	if g := sysmon.ReadGPUs(); len(g) > 0 && g[0].Busy >= 0 {
		a.gpu = a.newMeter("GPU", true, 0, 100)
	}
	a.net = a.newMeter("NETWORK", true, 0, 0)
	a.disk = a.newMeter("DISK", false, 0, 0)
	if sysmon.ReadBattery().Present {
		a.bat = a.newMeter("BATTERY", false, 0, 0)
	}
	a.foot = gtk.NewLabel("")
	a.foot.AddCssClass("foot")
	a.foot.SetXalign(0)
	gtkx.Append(&a.meters, &a.foot.Widget)

	a.onTick = func(uintptr) bool {
		a.sample()
		return true
	}
	a.onSort = func(t gtk.ToggleButton) {
		if t.GetActive() {
			sysmon.Sort(a.ps, a.sortKey())
			a.showProcesses()
		}
	}
	a.onQuit = func(b gtk.Button) {
		pid, _ := strconv.Atoi(strings.TrimPrefix(b.GetName(), "quit:"))
		a.listQuit(pid)
	}
	a.onOpen = func(b gtk.Button) {
		pid, _ := strconv.Atoi(strings.TrimPrefix(b.GetName(), "open:"))
		a.openProcess(pid)
	}
	a.onAct = func(b gtk.Button) { a.act(strings.TrimPrefix(b.GetName(), "act:")) }
	a.onBack = func(gtk.Button) { a.closeProcess() }
	back.ConnectClicked(&a.onBack)
	a.onFilter = func(gtk.SearchEntry) { a.showProcesses() }
	a.filter.ConnectSearchChanged(&a.onFilter)
	a.onStopFilter = func(e gtk.SearchEntry) { e.SetText("") } // Escape clears it
	a.filter.ConnectStopSearch(&a.onStopFilter)
	// The pages fill when first shown, not a tick later.
	a.onPage = func(uintptr, uintptr, uintptr) {
		switch a.pages.GetVisibleChildName() {
		case "processes":
			a.sampleProcesses()
		case "system":
			a.showSystem()
		}
	}
	gtkx.ConnectNotify(&a.pages, "visible-child", &a.onPage)
	// Ctrl+F: the processes, filtered. Escape: back from a process.
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	a.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) && a.open != nil && a.pages.GetVisibleChildName() == "processes" {
			a.closeProcess()
			return true
		}
		if state&gdk.ControlMaskValue == 0 || (keyval != 'f' && keyval != 'F') {
			return false
		}
		a.pages.SetVisibleChildName("processes")
		a.closeProcess()
		a.filter.GrabFocus()
		return true
	}
	keys.ConnectKeyPressed(&a.onKey)
	win.AddController(&keys.EventController)
	byCPU.ConnectToggled(&a.onSort)
	a.byMem.ConnectToggled(&a.onSort)
	a.byName.ConnectToggled(&a.onSort)

	stat := sysmon.ReadFile("proc/stat")
	a.lastCPU, _ = sysmon.ParseCPU(stat)
	a.lastCores = sysmon.ParseCores(stat)
	a.lastNet = sysmon.ParseNet(sysmon.ReadFile("proc/net/dev"))
	a.lastDisk = sysmon.ReadDiskIO()
	a.lastAt = time.Now()
	a.sample()
	glib.TimeoutAdd(1000, &a.onTick, 0)
	return win
}

func (a *sysmonApp) themeChanged() {
	for _, m := range []*meter{a.cpu, a.mem, a.gpu, a.net} {
		a.drawChart(m)
	}
}

func (a *sysmonApp) showPage(name string) {
	if a.pages.GetChildByName(name) != nil {
		a.pages.SetVisibleChildName(name)
	}
}

func (a *sysmonApp) sortKey() string {
	switch {
	case a.byMem.GetActive():
		return "memory"
	case a.byName.GetActive():
		return "name"
	}
	return "cpu"
}

// --- overview ---

func (a *sysmonApp) newMeter(name string, withChart bool, lo, hi float64) *meter {
	m := &meter{lo: lo, hi: hi}
	m.box = gtk.NewBox(gtk.OrientationVerticalValue, 4)
	m.box.AddCssClass("meter")
	head := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	k := gtk.NewLabel(name)
	k.AddCssClass("kicker")
	k.SetHexpand(true)
	k.SetXalign(0)
	k.SetValign(gtk.AlignCenterValue)
	gtkx.Append(head, &k.Widget)
	m.value = gtk.NewLabel("—")
	m.value.AddCssClass("meter-value")
	gtkx.Append(head, &m.value.Widget)
	gtkx.Append(m.box, &head.Widget)
	m.sub = gtk.NewLabel("")
	m.sub.AddCssClass("sub")
	m.sub.SetXalign(0)
	m.sub.SetEllipsize(3)
	gtkx.Append(m.box, &m.sub.Widget)
	if withChart {
		m.chart = gtk.NewPicture()
		m.chart.AddCssClass("meter-chart")
		m.chart.SetCanShrink(true)
		m.chart.SetContentFit(gtk.ContentFitFillValue)
		m.chart.SetSizeRequest(-1, 56)
		gtkx.Append(m.box, &m.chart.Widget)
	} else {
		m.bar, m.fill = meterBar()
		gtkx.Append(m.box, &m.bar.Widget)
	}
	gtkx.Append(&a.meters, &m.box.Widget)
	return m
}

// meterBar is an empty bar and its fill.
func meterBar() (bar, fill *gtk.Box) {
	bar = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	bar.AddCssClass("range")
	fill = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	fill.AddCssClass("range-fill")
	fill.SetHalign(gtk.AlignStartValue)
	gtkx.Append(bar, &fill.Widget)
	return bar, fill
}

// fillTo fills a bar of width w to a share, 0 to 1.
func fillTo(fill *gtk.Box, w int32, share float64) {
	fill.SetSizeRequest(int32(float64(w)*max(0, min(share, 1))), -1)
}

// buildCores puts a small bar for each core under the CPU meter.
func (a *sysmonApp) buildCores() {
	n := len(sysmon.ParseCores(sysmon.ReadFile("proc/stat")))
	if n < 2 {
		return
	}
	grid := gtk.NewGrid()
	grid.AddCssClass("cores")
	grid.SetColumnSpacing(6)
	grid.SetRowSpacing(6)
	for i := range n {
		bar, fill := meterBar()
		bar.AddCssClass("core")
		bar.SetSizeRequest(coreWidth, -1)
		grid.Attach(&bar.Widget, int32(i%8), int32(i/8), 1, 1)
		a.coreFills = append(a.coreFills, fill)
	}
	gtkx.Append(a.cpu.box, &grid.Widget)
}

func (m *meter) push(v float64) {
	m.points = append(m.points, v)
	if len(m.points) > history {
		m.points = m.points[len(m.points)-history:]
	}
}

// setBar fills a meter's bar to a share, 0 to 1.
func (m *meter) setBar(share float64) {
	w := m.bar.GetWidth()
	if w <= 0 {
		w = 380
	}
	fillTo(m.fill, w, share)
}

func (a *sysmonApp) drawChart(m *meter) {
	if m == nil || m.chart == nil || len(m.points) < 2 {
		return
	}
	scale := max(1, int(m.chart.GetScaleFactor()))
	w, h := int(m.chart.GetWidth()), int(m.chart.GetHeight())
	if w < 50 || h < 20 {
		w, h = 400, 56
	}
	// Always the full minute wide: pad the start while it fills.
	pts := m.points
	if len(pts) < history {
		pad := make([]float64, history-len(pts))
		for i := range pad {
			pad[i] = pts[0]
		}
		pts = append(pad, pts...)
	}
	o := chart.Options{Lo: m.lo, Hi: m.hi}
	if m.hi == 0 { // follow the points, from zero
		hi := 1.0
		for _, p := range pts {
			hi = max(hi, p)
		}
		o.Lo, o.Hi = 0, hi*1.15
	}
	img := chart.Line(pts, w*scale, h*scale, hexColor(a.c.Colors["accent"]), o)
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return
	}
	data := buf.Bytes()
	tex, err := gdk.NewTextureFromBytes(glib.NewBytes(data, uint(len(data))))
	if err != nil {
		return
	}
	m.chart.SetPaintable(tex)
	tex.Unref()
}

func (a *sysmonApp) sample() {
	now := time.Now()
	dt := now.Sub(a.lastAt).Seconds()
	a.lastAt = now

	stat := sysmon.ReadFile("proc/stat")
	cpu, threads := sysmon.ParseCPU(stat)
	use := sysmon.Usage(a.lastCPU, cpu)
	a.lastCPU = cpu
	a.cpu.push(use)
	a.cpu.value.SetText(fmt.Sprintf("%.0f%%", use))
	parts := []string{fmt.Sprintf("%d threads", threads)}
	if mhz := sysmon.ReadFrequency(); mhz > 0 {
		parts = append(parts, ghz(mhz))
	}
	load := sysmon.Load()
	parts = append(parts, fmt.Sprintf("load %.2f %.2f %.2f", load[0], load[1], load[2]))
	if t := sysmon.ReadTemperature(); t > 0 {
		parts = append(parts, fmt.Sprintf("%.0f °C", t))
	}
	a.cpu.sub.SetText(strings.Join(parts, " · "))
	cores := sysmon.ParseCores(stat)
	for i, fill := range a.coreFills {
		if i < len(cores) && i < len(a.lastCores) {
			u := sysmon.Usage(a.lastCores[i], cores[i])
			fillTo(fill, coreWidth, u/100)
			fill.GetParent().SetTooltipText(fmt.Sprintf("CPU %d: %.0f%%", i, u))
		}
	}
	a.lastCores = cores

	mem := sysmon.ParseMemory(sysmon.ReadFile("proc/meminfo"))
	if mem.Total > 0 {
		a.mem.push(100 * float64(mem.Used()) / float64(mem.Total))
		a.mem.value.SetText(sysmon.Bytes(mem.Used()))
		sub := "of " + sysmon.Bytes(mem.Total) + " · cache " + sysmon.Bytes(mem.Cache())
		if mem.SwapTotal > 0 {
			sub += fmt.Sprintf(" · swap %s of %s", sysmon.Bytes(mem.SwapTotal-mem.SwapFree), sysmon.Bytes(mem.SwapTotal))
		}
		a.mem.sub.SetText(sub)
	}

	if a.gpu != nil {
		if gs := sysmon.ReadGPUs(); len(gs) > 0 && gs[0].Busy >= 0 {
			g := gs[0]
			a.gpu.push(g.Busy)
			a.gpu.value.SetText(fmt.Sprintf("%.0f%%", g.Busy))
			sub := g.Name
			if sub == "" {
				sub = g.Driver
			}
			if g.VRAMTotal > 0 {
				sub += fmt.Sprintf(" · video memory %s of %s", sysmon.Bytes(g.VRAMUsed), sysmon.Bytes(g.VRAMTotal))
			}
			a.gpu.sub.SetText(sub)
		}
	}

	n := sysmon.ParseNet(sysmon.ReadFile("proc/net/dev"))
	if dt > 0 && n.Rx >= a.lastNet.Rx && n.Tx >= a.lastNet.Tx {
		rx := float64(n.Rx-a.lastNet.Rx) / dt
		tx := float64(n.Tx-a.lastNet.Tx) / dt
		a.net.push(rx)
		a.net.value.SetText("↓ " + sysmon.Rate(rx))
		a.net.sub.SetText("↑ " + sysmon.Rate(tx) + " · received " + sysmon.Bytes(n.Rx) + ", sent " + sysmon.Bytes(n.Tx))
	}
	a.lastNet = n

	io := sysmon.ReadDiskIO()
	if d, err := sysmon.ReadDisk("/"); err == nil && d.Total > 0 {
		a.disk.value.SetText(sysmon.Bytes(d.Used()))
		sub := fmt.Sprintf("of %s on / · %s free", sysmon.Bytes(d.Total), sysmon.Bytes(d.Free))
		if dt > 0 && io.Read >= a.lastDisk.Read && io.Written >= a.lastDisk.Written {
			sub += fmt.Sprintf(" · read %s · write %s",
				sysmon.Rate(float64(io.Read-a.lastDisk.Read)/dt), sysmon.Rate(float64(io.Written-a.lastDisk.Written)/dt))
		}
		a.disk.sub.SetText(sub)
		a.disk.setBar(float64(d.Used()) / float64(d.Total))
	}
	a.lastDisk = io

	if a.bat != nil {
		b := sysmon.ReadBattery()
		a.bat.value.SetText(fmt.Sprintf("%d%%", b.Percent))
		parts := []string{b.Status}
		if b.Watts > 0 {
			parts = append(parts, fmt.Sprintf("%.1f W", b.Watts))
		}
		if b.Capacity > 0 {
			parts = append(parts, fmt.Sprintf("health %.0f%%", b.Capacity))
		}
		a.bat.sub.SetText(strings.Join(parts, " · "))
		a.bat.setBar(float64(b.Percent) / 100)
	}
	a.foot.SetText("UP " + strings.ToUpper(sysmon.Duration(sysmon.Uptime())))

	for _, m := range []*meter{a.cpu, a.mem, a.gpu, a.net} {
		a.drawChart(m)
	}
	if a.armed != "" && time.Since(a.armedAt) > 4*time.Second {
		a.armed = ""
		a.showProcesses()
		a.showDetail()
	}
	if !a.noteAt.IsZero() && time.Since(a.noteAt) > 6*time.Second {
		a.note("", false)
	}
	switch a.pages.GetVisibleChildName() {
	case "processes":
		if time.Since(a.lastProcAt) >= 2*time.Second {
			a.sampleProcesses()
		}
	case "system":
		if time.Since(a.lastSysAt) >= 5*time.Second {
			a.showSystem()
		}
	}
}

// --- processes ---

// sampleProcesses reads the processes, sorts them and shows them, and the
// open one's details.
func (a *sysmonApp) sampleProcesses() {
	now := time.Now()
	ps := sysmon.ReadProcesses()
	if a.lastTicks != nil {
		sysmon.SetCPU(ps, a.lastTicks, now.Sub(a.lastProcAt))
	}
	a.lastTicks = map[int]uint64{}
	for _, p := range ps {
		a.lastTicks[p.PID] = p.Ticks
	}
	a.lastProcAt = now
	sysmon.Sort(ps, a.sortKey())
	a.ps = ps
	a.showProcesses()
	if a.open != nil {
		a.readDetail()
	}
}

func (a *sysmonApp) process(pid int) (sysmon.Process, bool) {
	for _, p := range a.ps {
		if p.PID == pid {
			return p, true
		}
	}
	return sysmon.Process{}, false
}

// showProcesses lists the last reading's top 50, of those the filter
// keeps.
func (a *sysmonApp) showProcesses() {
	ps := sysmon.Filter(a.ps, a.filter.GetText())
	if len(ps) == len(a.ps) {
		a.procCount.SetText(fmt.Sprintf("%d PROCESSES", len(ps)))
	} else {
		a.procCount.SetText(fmt.Sprintf("%d OF %d PROCESSES", len(ps), len(a.ps)))
	}
	gtkx.Clear(&a.procs)
	if len(ps) == 0 && len(a.ps) > 0 {
		l := gtk.NewLabel("No process matches.")
		l.AddCssClass("empty")
		l.SetXalign(0)
		gtkx.Append(&a.procs, &l.Widget)
	}
	for _, p := range ps[:min(len(ps), 50)] {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		row.AddCssClass("row")
		row.AddCssClass("proc")
		// The name and figures open the process; Quit sits beside them.
		inner := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		sub := "PID " + strconv.Itoa(p.PID)
		if p.UID != a.uid && p.User != "" {
			sub += " · " + p.User
		}
		if p.State != 'R' && p.State != 'S' && p.State != 'I' {
			sub += " · " + sysmon.StateName(p.State)
		}
		t := text(p.Name, sub)
		t.SetHexpand(true)
		gtkx.Append(inner, &t.Widget)
		cpu := gtk.NewLabel(fmt.Sprintf("%.1f%%", p.CPU))
		cpu.AddCssClass("readout")
		cpu.SetSizeRequest(52, -1)
		cpu.SetXalign(1)
		gtkx.Append(inner, &cpu.Widget)
		mem := gtk.NewLabel(sysmon.Bytes(p.RSS))
		mem.AddCssClass("readout")
		mem.SetSizeRequest(68, -1)
		mem.SetXalign(1)
		gtkx.Append(inner, &mem.Widget)
		open := rowButton("open:"+strconv.Itoa(p.PID), &inner.Widget, &a.onOpen)
		open.SetHexpand(true)
		open.SetTooltipText("Details and more ways to end " + p.Name)
		gtkx.Append(row, &open.Widget)
		if p.UID == a.uid {
			b := gtk.NewButtonWithLabel("Quit")
			b.AddCssClass("text")
			if a.armed == "list:"+strconv.Itoa(p.PID) {
				b.SetLabel("Quit?")
				b.AddCssClass("danger")
			}
			b.SetName("quit:" + strconv.Itoa(p.PID))
			b.SetTooltipText("Ask " + p.Name + " to quit")
			b.SetValign(gtk.AlignCenterValue)
			b.SetSizeRequest(56, -1)
			b.ConnectClicked(&a.onQuit)
			gtkx.Append(row, &b.Widget)
		} else {
			pad := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
			pad.SetSizeRequest(56, -1)
			gtkx.Append(row, &pad.Widget)
		}
		gtkx.Append(&a.procs, &row.Widget)
	}
}

// listQuit asks one of the user's processes to quit, on a second click
// within a few seconds.
func (a *sysmonApp) listQuit(pid int) {
	p, ok := a.process(pid)
	if !ok {
		return
	}
	if key := "list:" + strconv.Itoa(pid); a.armed != key {
		a.armed, a.armedAt = key, time.Now()
		a.showProcesses()
		return
	}
	a.armed = ""
	a.send(syscall.SIGTERM, []int{pid}, "Asked "+p.Name+" to quit.")
	a.showProcesses()
}

// own reports whether a process is the user's, so it can be signalled
// without asking for a password.
func (a *sysmonApp) own(pid int) bool {
	if p, ok := a.process(pid); ok {
		return p.UID == a.uid
	}
	fi, err := os.Stat("/proc/" + strconv.Itoa(pid))
	if err != nil {
		return true // gone; sending to it is harmless
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == a.uid
}

// send signals processes, off the main loop, as pkexec may wait for a
// password; done is the note to show when it has gone through.
func (a *sysmonApp) send(sig syscall.Signal, pids []int, done string) {
	go func() {
		err := sysmon.Signal(sig, pids, a.own)
		gtkx.Post(func() {
			switch {
			case errors.Is(err, sysmon.ErrCanceled):
				a.note("Not sent: an administrator's password is needed.", true)
			case err != nil:
				a.note(err.Error(), true)
			default:
				a.note(done, false)
			}
			a.sampleProcesses()
		})
	}()
}

// note shows a line about the last action, in the list and on the open
// process's page; "" hides it.
func (a *sysmonApp) note(text string, bad bool) {
	a.noteAt = time.Time{}
	if text != "" {
		a.noteAt = time.Now()
	}
	labels := []*gtk.Label{&a.procNote}
	if a.open != nil && a.open.note != nil {
		labels = append(labels, a.open.note)
	}
	for _, l := range labels {
		l.SetText(text)
		l.SetVisible(text != "")
		if bad {
			l.AddCssClass("error")
		} else {
			l.RemoveCssClass("error")
		}
	}
}

// --- one process ---

func (a *sysmonApp) openProcess(pid int) {
	a.armed = ""
	a.open = &detailView{pid: pid, facts: map[string]*gtk.Label{}, acts: map[string]*actionRow{}}
	if !a.readDetail() {
		a.open = nil
		a.note(fmt.Sprintf("Process %d has ended.", pid), false)
		return
	}
	a.buildDetail()
	a.showDetail()
	a.procPages.SetVisibleChildName("detail")
}

func (a *sysmonApp) closeProcess() {
	if a.open == nil {
		return
	}
	a.open = nil
	a.armed = ""
	a.procPages.SetVisibleChildName("list")
	a.showProcesses()
}

// readDetail reads the open process again; false when it has ended.
func (a *sysmonApp) readDetail() bool {
	v := a.open
	d, err := sysmon.ReadDetail(v.pid)
	if err != nil {
		if !v.gone {
			v.gone = true
			a.showDetail()
		}
		return false
	}
	v.d = d
	a.showDetail()
	return true
}

// section adds a heading to box.
func section(box *gtk.Box, name string) {
	k := gtk.NewLabel(name)
	k.AddCssClass("kicker")
	k.AddCssClass("section-head")
	k.SetXalign(0)
	gtkx.Append(box, &k.Widget)
}

// fact adds a name and its value to box, returning the value's label.
func fact(box *gtk.Box, name, value string, selectable bool) *gtk.Label {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	row.AddCssClass("fact")
	k := gtk.NewLabel(name)
	k.AddCssClass("fact-key")
	k.SetXalign(0)
	k.SetYalign(0)
	k.SetSizeRequest(112, -1)
	gtkx.Append(row, &k.Widget)
	v := gtk.NewLabel(value)
	v.AddCssClass("fact-value")
	v.SetXalign(0)
	v.SetHexpand(true)
	v.SetWrap(true)
	v.SetWrapMode(pango.WrapWordCharValue) // a long address breaks rather than widening the window
	v.SetSelectable(selectable)
	gtkx.Append(row, &v.Widget)
	gtkx.Append(box, &row.Widget)
	return v
}

// buildDetail lays out the open process's page; showDetail fills it.
func (a *sysmonApp) buildDetail() {
	v := a.open
	gtkx.Clear(&a.detail)

	cells := gtk.NewGrid()
	cells.AddCssClass("cells")
	cells.SetColumnSpacing(8)
	cells.SetRowSpacing(8)
	cells.SetColumnHomogeneous(true)
	for i, k := range []string{"CPU", "MEMORY", "THREADS"} {
		c := gtk.NewBox(gtk.OrientationVerticalValue, 4)
		c.AddCssClass("cell")
		name := gtk.NewLabel(k)
		name.AddCssClass("label")
		name.SetXalign(0)
		gtkx.Append(c, &name.Widget)
		value := gtk.NewLabel("—")
		value.AddCssClass("value")
		value.SetXalign(0)
		gtkx.Append(c, &value.Widget)
		v.facts["cell:"+k] = value
		cells.Attach(&c.Widget, int32(i), 0, 1, 1)
	}
	gtkx.Append(&a.detail, &cells.Widget)

	section(&a.detail, "END OR PAUSE")
	v.note = gtk.NewLabel("")
	v.note.AddCssClass("proc-note")
	v.note.SetXalign(0)
	v.note.SetWrap(true)
	v.note.SetVisible(false)
	gtkx.Append(&a.detail, &v.note.Widget)
	acts := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	acts.AddCssClass("rows")
	for _, ac := range actions {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("row")
		row.AddCssClass("action")
		box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		box.SetHexpand(true)
		box.SetValign(gtk.AlignCenterValue)
		title := gtk.NewLabel(ac.title)
		title.AddCssClass("title")
		title.SetXalign(0)
		gtkx.Append(box, &title.Widget)
		sub := gtk.NewLabel(ac.sub)
		sub.AddCssClass("sub")
		sub.SetXalign(0)
		sub.SetWrap(true)
		gtkx.Append(box, &sub.Widget)
		gtkx.Append(row, &box.Widget)
		b := gtk.NewButtonWithLabel(ac.button)
		b.AddCssClass("outlined")
		if ac.danger {
			b.AddCssClass("danger")
		}
		b.SetName("act:" + ac.key)
		b.SetValign(gtk.AlignCenterValue)
		b.SetSizeRequest(96, -1)
		b.ConnectClicked(&a.onAct)
		gtkx.Append(row, &b.Widget)
		gtkx.Append(acts, &row.Widget)
		v.acts[ac.key] = &actionRow{box: row, title: title, sub: sub, button: b}
	}
	gtkx.Append(&a.detail, &acts.Widget)

	section(&a.detail, "PROCESS")
	for _, k := range []string{"Parent", "User", "State", "Started", "CPU time", "Priority", "Unit"} {
		v.facts[k] = fact(&a.detail, k, "", true)
	}
	section(&a.detail, "MEMORY")
	for _, k := range []string{"Resident", "Private", "Shared", "Swapped out", "Peak"} {
		v.facts[k] = fact(&a.detail, k, "", false)
	}
	section(&a.detail, "FILES")
	for _, k := range []string{"Command", "Program", "Folder", "Open files", "Disk read", "Disk written"} {
		v.facts[k] = fact(&a.detail, k, "", true)
	}
}

// showDetail fills the open process's page from its last reading.
func (a *sysmonApp) showDetail() {
	v := a.open
	if v == nil || len(v.facts) == 0 {
		return
	}
	d := v.d
	p, listed := a.process(v.pid)
	a.detailName.SetText(d.Name)
	sub := fmt.Sprintf("PID %d · %s", d.PID, strings.ToUpper(sysmon.StateName(d.State)))
	if v.gone {
		sub = fmt.Sprintf("PID %d · ENDED", d.PID)
	}
	a.detailSub.SetText(sub)

	set := func(k, s string) {
		if l := v.facts[k]; l != nil && l.GetText() != s {
			l.SetText(s)
		}
	}
	unknown := func(s string) string {
		if s == "" {
			return "Not readable: not your process"
		}
		return s
	}
	cpu := "—"
	if listed {
		cpu = fmt.Sprintf("%.1f%%", p.CPU)
	}
	set("cell:CPU", cpu)
	set("cell:MEMORY", sysmon.Bytes(d.RSS))
	set("cell:THREADS", strconv.Itoa(d.Threads))
	parent := strconv.Itoa(d.PPID)
	if d.Parent != "" {
		parent = fmt.Sprintf("%s · PID %d", d.Parent, d.PPID)
	}
	set("Parent", parent)
	user := d.User
	if user == "" {
		user = strconv.Itoa(d.UID)
	}
	set("User", fmt.Sprintf("%s (%d)", user, d.UID))
	set("State", sysmon.StateName(d.State))
	if !d.Started.IsZero() {
		set("Started", d.Started.Format("Jan 2 15:04:05")+" · "+sysmon.Elapsed(d.Started)+" ago")
	}
	set("CPU time", cpuTime(d.CPUTime))
	set("Priority", fmt.Sprintf("nice %d", d.Nice))
	set("Unit", cmpOr(d.Unit, "—"))
	set("Resident", sysmon.Bytes(d.RSS))
	set("Private", sysmon.Bytes(d.Anon))
	set("Shared", sysmon.Bytes(d.Shared))
	set("Swapped out", sysmon.Bytes(d.Swap))
	set("Peak", sysmon.Bytes(d.Peak))
	set("Command", unknown(d.Cmdline))
	set("Program", unknown(d.Exe))
	set("Folder", unknown(d.Cwd))
	set("Open files", count(d.FDs))
	set("Disk read", bytesOrUnknown(d.ReadBytes))
	set("Disk written", bytesOrUnknown(d.WriteBytes))

	ours := d.UID == a.uid
	kids := sysmon.Descendants(a.ps, d.PID)
	named := sysmon.Named(a.ps, d.Name, d.UID)
	for _, ac := range actions {
		r := v.acts[ac.key]
		title, sub, label, danger := ac.title, ac.sub, ac.button, ac.danger
		visible := !v.gone
		switch ac.key {
		case "pause":
			if d.State == 'T' || d.State == 't' {
				title, sub, label = "Resume", "It is paused. Lets it go on.", "Resume"
			}
		case "tree":
			visible = visible && len(kids) > 0
			sub = fmt.Sprintf("Also its %s, the youngest first.", plural(len(kids), "child process", "child processes"))
		case "named":
			visible = visible && len(named) > 1
			sub = fmt.Sprintf("All %d processes named %s.", len(named), d.Name)
		}
		if !ours && visible {
			sub += " Asks for an administrator's password."
		}
		if ac.confirm && a.armed == ac.key+":"+strconv.Itoa(d.PID) {
			label, danger = "Sure?", true
		}
		r.box.SetVisible(visible)
		r.title.SetText(title)
		r.sub.SetText(sub)
		r.button.SetLabel(label)
		if danger {
			r.button.AddCssClass("danger")
		} else {
			r.button.RemoveCssClass("danger")
		}
	}
	if v.gone && !a.noteAt.IsZero() {
		return // keep the action's note
	}
	if v.gone {
		a.note("This process has ended.", false)
	}
}

// act runs one of the open process's actions, those that confirm on a
// second click.
func (a *sysmonApp) act(key string) {
	v := a.open
	if v == nil || v.gone {
		return
	}
	var ac action
	for _, x := range actions {
		if x.key == key {
			ac = x
		}
	}
	if ac.key == "" {
		return
	}
	d := v.d
	armKey := key + ":" + strconv.Itoa(d.PID)
	if ac.confirm && a.armed != armKey {
		a.armed, a.armedAt = armKey, time.Now()
		a.showDetail()
		return
	}
	a.armed = ""
	sig, pids, done := ac.sig, []int{d.PID}, ""
	switch key {
	case "quit":
		done = "Asked " + d.Name + " to quit."
	case "interrupt":
		done = "Interrupted " + d.Name + "."
	case "hangup":
		done = "Hung up on " + d.Name + "."
	case "kill":
		done = "Ended " + d.Name + "."
	case "pause":
		done = "Paused " + d.Name + "."
		if d.State == 'T' || d.State == 't' {
			sig, done = syscall.SIGCONT, "Resumed "+d.Name+"."
		}
	case "tree":
		pids = append(sysmon.Descendants(a.ps, d.PID), d.PID)
		done = fmt.Sprintf("Asked %s and its %s to quit.", d.Name, plural(len(pids)-1, "child", "children"))
	case "named":
		pids = sysmon.Named(a.ps, d.Name, d.UID)
		done = fmt.Sprintf("Asked %d processes named %s to quit.", len(pids), d.Name)
	}
	a.send(sig, pids, done)
	a.showDetail()
}

// --- system ---

// showSystem lists the computer and its parts.
func (a *sysmonApp) showSystem() {
	a.lastSysAt = time.Now()
	gtkx.Clear(&a.sys)
	w := a.sys.GetWidth() - 56 // the list's padding
	if w <= 0 {
		w = 380
	}

	si := sysmon.ReadSystemInfo()
	section(&a.sys, "COMPUTER")
	fact(&a.sys, "Model", cmpOr(si.Model, "—"), true)
	fact(&a.sys, "Name", si.Host, true)
	fact(&a.sys, "System", cmpOr(si.OS, "—"), true)
	fact(&a.sys, "Kernel", si.Kernel, true)
	up := sysmon.Duration(sysmon.Uptime())
	if !si.Boot.IsZero() {
		up += " · since " + si.Boot.Format("Jan 2 15:04")
	}
	fact(&a.sys, "Up", up, false)

	c := a.cpuInfo
	section(&a.sys, "PROCESSOR")
	fact(&a.sys, "Model", cmpOr(c.Model, "—"), true)
	fact(&a.sys, "Cores", fmt.Sprintf("%s, %s", plural(c.Cores, "core", "cores"), plural(c.Threads, "thread", "threads")), false)
	speed := ""
	if c.MaxMHz > 0 {
		speed = "up to " + ghz(c.MaxMHz)
	}
	if now := sysmon.ReadFrequency(); now > 0 {
		speed = strings.TrimPrefix(speed+" · now "+ghz(now), " · ")
	}
	if speed != "" {
		fact(&a.sys, "Speed", speed, false)
	}

	mem := sysmon.ParseMemory(sysmon.ReadFile("proc/meminfo"))
	section(&a.sys, "MEMORY")
	fact(&a.sys, "Installed", sysmon.Bytes(mem.Total), false)
	fact(&a.sys, "In use", sysmon.Bytes(mem.Used()), false)
	fact(&a.sys, "Cache", sysmon.Bytes(mem.Cache())+" · given back when needed", false)
	fact(&a.sys, "Free", sysmon.Bytes(mem.Free), false)
	if mem.SwapTotal > 0 {
		fact(&a.sys, "Swap", fmt.Sprintf("%s of %s", sysmon.Bytes(mem.SwapTotal-mem.SwapFree), sysmon.Bytes(mem.SwapTotal)), false)
	}

	if gs := sysmon.ReadGPUs(); len(gs) > 0 {
		section(&a.sys, "GRAPHICS")
		for _, g := range gs {
			fact(&a.sys, "Card", cmpOr(g.Name, g.Driver), true)
			fact(&a.sys, "Driver", g.Driver, false)
			if g.Busy >= 0 {
				fact(&a.sys, "Busy", fmt.Sprintf("%.0f%%", g.Busy), false)
			}
			if g.VRAMTotal > 0 {
				fact(&a.sys, "Video memory", fmt.Sprintf("%s of %s", sysmon.Bytes(g.VRAMUsed), sysmon.Bytes(g.VRAMTotal)), false)
			}
		}
	}

	if fss := sysmon.ReadFilesystems(); len(fss) > 0 {
		section(&a.sys, "STORAGE")
		for _, fs := range fss {
			box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
			box.AddCssClass("volume")
			head := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
			t := text(fs.Mount, fs.Device+" · "+fs.Type)
			t.SetHexpand(true)
			gtkx.Append(head, &t.Widget)
			r := gtk.NewLabel(fmt.Sprintf("%s of %s", sysmon.Bytes(fs.Used()), sysmon.Bytes(fs.Total)))
			r.AddCssClass("readout")
			gtkx.Append(head, &r.Widget)
			gtkx.Append(box, &head.Widget)
			if fs.Total > 0 {
				bar, fill := meterBar()
				fillTo(fill, w, float64(fs.Used())/float64(fs.Total))
				gtkx.Append(box, &bar.Widget)
			}
			gtkx.Append(&a.sys, &box.Widget)
		}
	}

	if ifs := sysmon.ReadInterfaces(); len(ifs) > 0 {
		section(&a.sys, "NETWORK")
		for _, it := range ifs {
			kind := "wired"
			if it.Wireless {
				kind = "Wi-Fi"
			}
			state := "down"
			if it.Up {
				state = "up"
			}
			fact(&a.sys, it.Name, fmt.Sprintf("%s · %s", kind, state), false)
			if len(it.Addrs) > 0 {
				fact(&a.sys, "Addresses", strings.Join(it.Addrs, "\n"), true)
			}
			if it.MAC != "" {
				fact(&a.sys, "Hardware", it.MAC, true)
			}
			if it.Traffic.Rx > 0 || it.Traffic.Tx > 0 {
				fact(&a.sys, "Traffic", fmt.Sprintf("received %s · sent %s", sysmon.Bytes(it.Traffic.Rx), sysmon.Bytes(it.Traffic.Tx)), false)
			}
		}
	}

	if ss := sysmon.ReadSensors(); len(ss) > 0 {
		section(&a.sys, "SENSORS")
		for _, s := range ss {
			value := fmt.Sprintf("%.0f °C", s.Value)
			if s.Fan {
				value = fmt.Sprintf("%.0f RPM", s.Value)
			}
			fact(&a.sys, s.Chip+" "+s.Label, value, false)
		}
	}
	foot := gtk.NewLabel("")
	foot.AddCssClass("foot")
	gtkx.Append(&a.sys, &foot.Widget)
}

// --- words ---

func ghz(mhz float64) string { return fmt.Sprintf("%.1f GHz", mhz/1000) }

func cpuTime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1f s", d.Seconds())
	}
	return sysmon.Duration(d)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func count(n int) string {
	if n < 0 {
		return "Not readable: not your process"
	}
	return strconv.Itoa(n)
}

func bytesOrUnknown(n int64) string {
	if n < 0 {
		return "Not readable: not your process"
	}
	return sysmon.Bytes(uint64(n))
}

func cmpOr(s, or string) string {
	if strings.TrimSpace(s) == "" {
		return or
	}
	return s
}
