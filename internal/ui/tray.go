package ui

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/sni"
	"github.com/lyricat/inua/internal/system"
)

const trayIconSize = 16

type tray struct {
	host    *sni.Host
	items   []sni.Item
	paths   map[string]bool  // icon theme paths already added
	menuKey string           // item whose menu is open
	menu    *gtk.PopoverMenu // the open menu, borrowed from its parent
}

// startTray serves the StatusNotifierWatcher (unless another process does)
// and shows the registered items in every bar.
func (s *shellUI) startTray() {
	s.tray.paths = map[string]bool{}
	ctx := context.Background()
	if conn, err := system.SessionBus(); err != nil {
		log.Printf("tray: %v", err)
	} else {
		go func() { logErr("tray watcher", sni.ServeWatcher(ctx, conn)) }()
	}
	conn, err := system.SessionBus()
	if err != nil {
		log.Printf("tray: %v", err)
		return
	}
	s.tray.host = sni.NewHost(conn, func(items []sni.Item) {
		gtkx.Post(func() {
			s.tray.items = items
			for _, b := range s.bars {
				s.renderTray(b)
			}
		})
	})
	go func() { logErr("tray host", s.tray.host.Run(ctx)) }()
}

func (s *shellUI) bindTrayHandlers() {
	s.onTrayClicked = func(b gtk.Button) {
		key := b.GetName()
		it, ok := s.trayItem(key)
		if !ok {
			return
		}
		if it.ItemIsMenu && it.HasMenu {
			s.showTrayMenu(key, &b.Widget)
			return
		}
		go func() {
			if err := s.tray.host.Activate(key); err != nil {
				log.Printf("tray: activate %s: %v", key, err)
			}
		}()
	}
	// Middle and right clicks; the button itself handles the left one.
	s.onTrayPressed = func(g gtk.GestureClick, _ int32, _, _ float64) {
		w := g.GetWidget()
		if w == nil {
			return
		}
		defer w.Unref() // GetWidget returned a new reference
		key := w.GetName()
		it, ok := s.trayItem(key)
		if !ok {
			return
		}
		switch g.GetCurrentButton() {
		case 2:
			go func() { logErr("tray", s.tray.host.SecondaryActivate(key)) }()
		case 3:
			if it.HasMenu {
				s.showTrayMenu(key, w)
			} else {
				go func() { logErr("tray", s.tray.host.ContextMenu(key)) }()
			}
		}
	}
	s.onTrayMenuAction = func(a gio.SimpleAction, _ uintptr) {
		id, err := strconv.Atoi(strings.TrimPrefix(a.GetName(), "i"))
		if err != nil {
			return
		}
		key := s.tray.menuKey
		go func() { logErr("tray menu", s.tray.host.MenuClicked(key, int32(id))) }()
	}
	s.onTrayMenuClosed = func(p gtk.Popover) {
		// Unparenting inside the signal is unsafe; the parent's reference
		// was the last one, so this frees the popover.
		s.tray.menu = nil
		gtkx.Post(func() { p.Unparent() })
	}
}

func (s *shellUI) trayItem(key string) (sni.Item, bool) {
	for _, it := range s.tray.items {
		if it.Key == key {
			return it, true
		}
	}
	return sni.Item{}, false
}

func (s *shellUI) renderTray(b *bar) {
	gtkx.Clear(&b.tray)
	for _, it := range s.tray.items {
		if it.Status == "Passive" {
			continue
		}
		btn := gtk.NewButton()
		btn.SetName(it.Key)
		btn.SetFocusable(false)
		tip := it.Tooltip
		if tip == "" {
			tip = it.Title
		}
		if tip != "" {
			btn.SetTooltipText(tip)
		}
		img := s.trayImage(it)
		btn.SetChild(&img.Widget)
		img.Unref()
		btn.ConnectClicked(&s.onTrayClicked)
		click := gtk.NewGestureClick()
		click.SetButton(0) // any button; the handler ignores the left one
		click.ConnectPressed(&s.onTrayPressed)
		btn.AddController(&click.EventController)
		gtkx.Append(&b.tray, &btn.Widget)
	}
}

// trayImage picks an item's icon: an absolute file, a themed name (with the
// item's own theme directory), or the pixmap the item sent.
func (s *shellUI) trayImage(it sni.Item) *gtk.Image {
	name, pix := it.IconName, it.Pixmaps
	if it.Status == "NeedsAttention" && (it.AttentionIconName != "" || len(it.AttentionPixmaps) > 0) {
		name, pix = it.AttentionIconName, it.AttentionPixmaps
	}
	theme := gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault())
	defer theme.Unref()
	if p := it.IconThemePath; p != "" && !s.tray.paths[p] {
		s.tray.paths[p] = true
		theme.AddSearchPath(p)
	}
	var img *gtk.Image
	switch {
	case strings.HasPrefix(name, "/"):
		img = gtk.NewImageFromFile(name)
	case name != "" && theme.HasIcon(name):
		img = gtk.NewImageFromIconName(name)
	case len(pix) > 0:
		tex := pixmapTexture(pix)
		img = gtk.NewImageFromPaintable(tex)
		tex.Unref()
	default:
		img = gtk.NewImageFromIconName("application-x-executable-symbolic")
	}
	img.SetPixelSize(trayIconSize)
	return img
}

// pixmapTexture turns the best-fitting pixmap into a texture: the smallest
// one that is sharp at twice the icon size, else the largest.
func pixmapTexture(pix []sni.Pixmap) *gdk.MemoryTexture {
	best := pix[0]
	for _, p := range pix[1:] {
		fits, bestFits := p.Width >= 2*trayIconSize, best.Width >= 2*trayIconSize
		if (fits && (!bestFits || p.Width < best.Width)) || (!fits && !bestFits && p.Width > best.Width) {
			best = p
		}
	}
	bytes := glib.NewBytes(best.ARGB, uint(len(best.ARGB)))
	defer bytes.Unref()
	// SNI pixmaps are ARGB32 in network byte order, unpremultiplied.
	return gdk.NewMemoryTexture(best.Width, best.Height, gdk.MemoryA8r8g8b8Value, bytes, uint(best.Width*4))
}

// showTrayMenu reads the item's menu and pops it up under w.
func (s *shellUI) showTrayMenu(key string, w *gtk.Widget) {
	w.Ref() // kept until the menu is built
	go func() {
		entries, err := s.tray.host.Menu(key)
		gtkx.Post(func() {
			defer w.Unref()
			if err != nil || len(entries) == 0 {
				if err != nil {
					log.Printf("tray: menu %s: %v", key, err)
				}
				// Some items list a menu but leave it empty; let them
				// show their own.
				go func() { logErr("tray", s.tray.host.ContextMenu(key)) }()
				return
			}
			if !w.GetMapped() { // the tray was redrawn meanwhile
				return
			}
			group := gio.NewSimpleActionGroup()
			menu := s.buildMenu(entries, group)
			w.InsertActionGroup("tray", group)
			group.Unref()
			s.tray.menuKey = key

			pop := gtk.NewPopoverMenuFromModel(&menu.MenuModel)
			menu.Unref()
			pop.SetHasArrow(false)
			pop.SetParent(w)
			pop.ConnectClosed(&s.onTrayMenuClosed)
			pop.Unref() // the parent holds it now
			pop.Popup()
			s.tray.menu = pop
		})
	}()
}

// buildMenu converts dbusmenu entries. Each enabled entry gets an action
// "i<id>"; disabled ones point at a missing action, which GTK greys out.
// Separators split the menu into sections.
func (s *shellUI) buildMenu(entries []sni.MenuEntry, group *gio.SimpleActionGroup) *gio.Menu {
	menu := gio.NewMenu()
	section := gio.NewMenu()
	flush := func() {
		if section.GetNItems() > 0 {
			item := gio.NewMenuItemSection("", &section.MenuModel)
			item.SetAttributeValue("label", nil) // no section heading
			menu.AppendItem(item)
			item.Unref()
		}
		section.Unref()
		section = gio.NewMenu()
	}
	for _, e := range entries {
		if e.Separator {
			flush()
			continue
		}
		label := e.Label
		switch {
		case e.Toggle == "checkmark" && e.Checked:
			label = "✓  " + label
		case e.Toggle == "radio" && e.Checked:
			label = "●  " + label
		case e.Toggle != "":
			label = "     " + label
		}
		if len(e.Children) > 0 {
			sub := s.buildMenu(e.Children, group)
			section.AppendSubmenu(label, &sub.MenuModel)
			sub.Unref()
			continue
		}
		action := "i" + strconv.Itoa(int(e.ID))
		if e.Enabled {
			a := gio.NewSimpleAction(action, nil)
			a.ConnectActivate(&s.onTrayMenuAction)
			group.AddAction(a)
			a.Unref()
		} else {
			action = "disabled"
		}
		section.Append(label, "tray."+action)
	}
	flush()
	section.Unref()
	return menu
}

// trayMenu opens the menu of the n-th tray button on the first bar, as a
// right click would (for testing).
func (s *shellUI) trayMenu(n int) error {
	bars := s.sortedBars()
	if len(bars) == 0 {
		return fmt.Errorf("no bar")
	}
	child := bars[0].tray.GetFirstChild()
	for i := 0; child != nil && i < n; i++ {
		next := child.GetNextSibling()
		child.Unref()
		child = next
	}
	if child == nil {
		return fmt.Errorf("no tray item %d", n)
	}
	defer child.Unref()
	key := child.GetName()
	if it, ok := s.trayItem(key); !ok || !it.HasMenu {
		return fmt.Errorf("item %q has no menu", key)
	}
	s.showTrayMenu(key, child)
	return nil
}
