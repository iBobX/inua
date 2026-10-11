package ui

import (
	"log"
	"strings"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
)

// The dock's drawer: a button at the dock's end (dock.drawer) that opens
// a card of inua's light apps above it. The card is in a full-screen
// overlay, like the control center's, so it slides up and back down
// whichever way it closes: a click outside, Escape, the button again, or
// an app started. The dock stays up while it is open.

// drawerColumns is how many apps a row of the drawer holds.
const drawerColumns = 4

// drawerRevealMS matches the revealer's transition below.
const drawerRevealMS = 220

type drawerPanel struct {
	win      gtk.ApplicationWindow
	backdrop *gtk.Box // borrowed from win
	reveal   *gtk.Revealer
	closing  bool
}

func (s *shellUI) bindDrawerHandlers() {
	s.onDrawerClicked = func(b gtk.Button) {
		for _, bar := range s.bars {
			if d := bar.dock; d != nil && d.drawer != nil && d.drawer.GoPointer() == b.GoPointer() {
				s.toggleDrawer(bar)
				return
			}
		}
	}
	s.onDrawerApp = func(b gtk.Button) {
		id := strings.TrimPrefix(b.GetName(), "drawer:")
		gtkx.Post(s.closeDrawer) // the button is in the drawer
		for _, e := range shell.LightApps(s.dockState.apps) {
			if e.ID == id {
				s.launchApp(e)
			}
		}
	}
	s.onDrawerBackdrop = func(g gtk.GestureClick, _ int32, x, y float64) {
		p := s.dockState.drawer
		if p == nil {
			return
		}
		hit := p.backdrop.Pick(x, y, gtk.PickDefaultValue)
		if hit == nil {
			return
		}
		outside := hit.GoPointer() == p.backdrop.GoPointer()
		hit.Unref() // Pick returned a new reference
		if outside {
			gtkx.Post(s.closeDrawer)
		}
	}
	s.onDrawerKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval != keyEscape {
			return false
		}
		gtkx.Post(s.closeDrawer)
		return true
	}
	s.onDrawerGone = func(uintptr) bool {
		if p := s.dockState.drawer; p != nil && p.closing {
			s.dockState.drawer = nil
			p.win.Destroy()
			s.hideDocksSoon()
		}
		return false
	}
}

// drawerButton is the button at the dock's end, the size of an app's.
func (s *shellUI) drawerButton(size int) *gtk.Button {
	btn := gtk.NewButton()
	btn.AddCssClass("dock-item")
	btn.AddCssClass("dock-drawer")
	btn.SetName("dock-drawer")
	btn.SetTooltipText("Apps")
	btn.SetFocusable(false)
	box := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	frame := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	frame.AddCssClass("drawer-frame")
	frame.SetSizeRequest(int32(size), int32(size))
	img := gtk.NewImageFromIconName("view-app-grid-symbolic")
	img.SetPixelSize(int32(max(14, size*9/20)))
	img.SetHexpand(true)
	img.SetHalign(gtk.AlignCenterValue)
	img.SetValign(gtk.AlignCenterValue)
	gtkx.Append(frame, &img.Widget)
	gtkx.Append(box, &frame.Widget)
	dot := gtk.NewBox(gtk.OrientationHorizontalValue, 0) // keeps it level with the apps
	dot.AddCssClass("dock-dot")
	dot.SetHalign(gtk.AlignCenterValue)
	gtkx.Append(box, &dot.Widget)
	btn.SetChild(&box.Widget)
	box.Unref()
	btn.ConnectClicked(&s.onDrawerClicked)
	return btn
}

// toggleDrawer opens the drawer over a bar's dock, or closes it.
func (s *shellUI) toggleDrawer(b *bar) {
	if p := s.dockState.drawer; p != nil {
		if !p.closing {
			s.closeDrawer()
			return
		}
		// Opened again while sliding away: that one goes now.
		s.dockState.drawer = nil
		p.win.Destroy()
	}
	if s.dockState.menu != nil {
		s.dockState.menu.Popdown()
	}
	s.openDrawer(b)
}

// drawerContent is the card: each group's title, then its apps in rows of
// drawerColumns, every cell as wide as the others, so a long name wraps
// instead of widening the row.
func (s *shellUI) drawerContent() *gtk.Box {
	sections := shell.LightAppSections(s.dockState.apps)
	content := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	content.AddCssClass("drawer")
	for _, g := range sections {
		title := gtk.NewLabel(g.Title)
		title.AddCssClass("drawer-title")
		title.SetXalign(0)
		gtkx.Append(content, &title.Widget)
		grid := gtk.NewGrid()
		grid.AddCssClass("drawer-grid")
		grid.SetColumnHomogeneous(true)
		grid.SetColumnSpacing(4)
		grid.SetRowSpacing(4)
		for i, e := range g.Apps {
			b := gtk.NewButton()
			b.AddCssClass("drawer-app")
			b.SetName("drawer:" + e.ID)
			b.SetTooltipText(e.Comment)
			col := gtk.NewBox(gtk.OrientationVerticalValue, 7)
			img := gtk.NewImage()
			setIcon(img, e.Icon)
			img.SetPixelSize(44)
			gtkx.Append(col, &img.Widget)
			l := gtk.NewLabel(e.Name)
			l.AddCssClass("drawer-name")
			l.SetWrap(true)
			l.SetJustify(gtk.JustifyCenterValue)
			l.SetLines(2)
			l.SetEllipsize(3) // end
			l.SetMaxWidthChars(10)
			l.SetValign(gtk.AlignStartValue)
			gtkx.Append(col, &l.Widget)
			b.SetChild(&col.Widget)
			col.Unref()
			b.ConnectClicked(&s.onDrawerApp)
			grid.Attach(&b.Widget, int32(i%drawerColumns), int32(i/drawerColumns), 1, 1)
			b.Unref()
		}
		// A short group keeps all the columns, so its apps are as wide
		// as the others' rather than sharing the whole row.
		for i := len(g.Apps); i < drawerColumns; i++ {
			empty := gtk.NewBox(gtk.OrientationVerticalValue, 0)
			grid.Attach(&empty.Widget, int32(i), 0, 1, 1)
			empty.Unref()
		}
		gtkx.Append(content, &grid.Widget)
	}
	if len(sections) == 0 {
		l := gtk.NewLabel("The light apps are not installed (make install).")
		l.AddCssClass("drawer-empty")
		gtkx.Append(content, &l.Widget)
	}
	return content
}

// openDrawer slides the launcher out from the dock edge, beside its button.
func (s *shellUI) openDrawer(b *bar) {
	d := b.dock
	if d == nil || d.drawer == nil {
		return
	}
	p := &drawerPanel{win: *gtk.NewApplicationWindow(s.app)}
	p.win.AddCssClass("drawer-overlay")
	backdrop := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	backdrop.SetHexpand(true)
	backdrop.SetVexpand(true)
	reveal := gtk.NewRevealer()
	reveal.SetTransitionDuration(drawerRevealMS)
	reveal.SetHalign(gtk.AlignStartValue)
	reveal.SetValign(gtk.AlignStartValue)
	card := s.drawerContent()
	reveal.SetChild(&card.Widget)
	gtkx.Append(backdrop, &reveal.Widget)
	p.win.SetChild(&backdrop.Widget)
	backdrop.Unref()
	card.Unref()
	p.backdrop, p.reveal = backdrop, reveal

	// Position the launcher next to its button. The old layout always
	// placed it above a bottom dock; on a vertical dock that bottom margin
	// was the height of the whole monitor, which pushed the card to the top.
	var cardSize gtk.Requisition
	card.GetPreferredSize(nil, &cardSize)
	var geo gdk.Rectangle
	b.mon.GetGeometry(&geo)
	var r graphene.Rect
	var f [4]float32
	hasBounds := d.drawer.ComputeBounds(&d.win.Widget, &r)
	if hasBounds {
		f = *(*[4]float32)(unsafe.Pointer(&r))
	}
	position := d.position
	switch position {
	case "left":
		reveal.SetTransitionType(gtk.RevealerTransitionTypeSlideRightValue)
		reveal.SetMarginStart(int32(d.win.GetWidth()) + 2)
		y := (float32(d.win.GetHeight()) - float32(cardSize.Height)) / 2
		if hasBounds {
			y = f[1] + f[3]/2 - float32(cardSize.Height)/2
		}
		reveal.SetMarginTop(clampDrawerOffset(y, geo.Height, cardSize.Height))
	case "right":
		reveal.SetTransitionType(gtk.RevealerTransitionTypeSlideLeftValue)
		x := geo.Width - d.win.GetWidth() - cardSize.Width - 2
		reveal.SetMarginStart(max(0, x))
		y := (float32(d.win.GetHeight()) - float32(cardSize.Height)) / 2
		if hasBounds {
			y = f[1] + f[3]/2 - float32(cardSize.Height)/2
		}
		reveal.SetMarginTop(clampDrawerOffset(y, geo.Height, cardSize.Height))
	case "top":
		reveal.SetTransitionType(gtk.RevealerTransitionTypeSlideDownValue)
		x := float32(geo.Width-cardSize.Width) / 2
		if hasBounds {
			x = (float32(geo.Width)-float32(d.win.GetWidth()))/2 + f[0] + f[2]/2 - float32(cardSize.Width)/2
		}
		reveal.SetMarginStart(clampDrawerOffset(x, geo.Width, cardSize.Width))
		reveal.SetMarginTop(int32(d.win.GetHeight()) + 2)
	default: // bottom
		reveal.SetTransitionType(gtk.RevealerTransitionTypeSlideUpValue)
		x := float32(geo.Width-cardSize.Width) / 2
		if hasBounds {
			x = (float32(geo.Width)-float32(d.win.GetWidth()))/2 + f[0] + f[2]/2 - float32(cardSize.Width)/2
		}
		reveal.SetMarginStart(clampDrawerOffset(x, geo.Width, cardSize.Width))
		reveal.SetMarginBottom(int32(d.win.GetHeight()) + 2)
	}

	click := gtk.NewGestureClick()
	click.ConnectPressed(&s.onDrawerBackdrop)
	backdrop.AddController(&click.EventController)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&s.onDrawerKey)
	p.win.AddController(&keys.EventController)

	w := p.win.GoPointer()
	layershell.InitForWindow(w)
	layershell.SetNamespace(w, "inua-drawer")
	layershell.SetLayer(w, layershell.LayerOverlay)
	layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
	for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
		layershell.SetAnchor(w, e, true)
	}
	layershell.SetExclusiveZone(w, -1)
	layershell.SetMonitor(w, b.mon.GoPointer())
	s.dockState.drawer = p
	p.win.Present()
	reveal.SetRevealChild(true) // after Present, so it slides in
}

// clampDrawerOffset keeps a card inside a monitor with a small breathing edge.
func clampDrawerOffset(offset float32, extent, card int32) int32 {
	maxOffset := max(0, extent-card-8)
	return max(8, min(int32(offset), maxOffset))
}

// closeDrawer slides the drawer away; the window goes when it is down.
func (s *shellUI) closeDrawer() {
	p := s.dockState.drawer
	if p == nil || p.closing {
		return
	}
	p.closing = true
	p.reveal.SetRevealChild(false)
	glib.TimeoutAdd(drawerRevealMS, &s.onDrawerGone, 0)
}

// drawerRequest answers `inua call drawer toggle|open|close` with the
// drawer of the dock on the focused monitor.
func (s *shellUI) drawerRequest(fn string) bool {
	p := s.dockState.drawer
	open := p != nil && !p.closing
	switch fn {
	case "toggle", "open", "close":
	default:
		return false
	}
	if fn == "close" || (fn == "toggle" && open) {
		s.closeDrawer()
		return true
	}
	if open {
		return true
	}
	for _, b := range s.bars {
		if b.dock == nil || b.connector() != s.snap.FocusedMonitor || b.dock.drawer == nil {
			continue
		}
		s.showDock(b.dock)
		s.toggleDrawer(b)
	}
	return true
}

// openLightApp opens a light app by its desktop entry, or runs it
// directly when the entry is not installed.
func (s *shellUI) openLightApp(id, name string) {
	for _, e := range shell.LightApps(s.dockState.apps) {
		if e.ID == id {
			s.launchApp(e)
			return
		}
	}
	if err := desktop.Spawn([]string{"inua-apps", name}, ""); err != nil {
		log.Printf("%s: %v", name, err)
	}
}
