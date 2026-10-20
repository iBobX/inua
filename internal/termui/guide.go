package termui

import (
	"cmp"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/scheme"
	"github.com/lyricat/inua/internal/term/settings"
)

// The guide shows on the first start (no term.toml yet), before the host
// starts the first pane: three steps, each a list of choices beside a
// preview: the shell new panes start, the terminals' colours and the
// window's theme. A choice applies at once; Done, Escape or closing the
// guide keeps them and writes term.toml, so the guide does not come back.

type guide struct {
	win   *gtk.Window
	stack *gtk.Stack
	step  int
	count *gtk.Label
	back  *gtk.Button
	next  *gtk.Button
	// The previews: a terminal (its text and the 16 colours), and the
	// window in the theme chosen; css colours them.
	terms []*gtk.Label
	css   *gtk.CssProvider
	done  func()

	cbs []any // the callbacks GTK holds
}

var guideSteps = []string{"shell", "colours", "theme"}

// openGuide shows the guide over the window; done runs once it closes.
func (a *App) openGuide(done func()) {
	g := &guide{done: done, css: gtk.NewCssProvider()}
	a.guide = g
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), g.css, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
	g.win = gtk.NewWindow()
	g.win.SetApplication(a.c.App) // the app's class, a dialog of its window
	g.win.SetTitle(i18n.T("Welcome to the Terminal"))
	g.win.SetTransientFor(&a.win.Window)
	g.win.SetModal(true)
	g.win.SetDefaultSize(820, 560)
	for _, c := range []string{"app", "settings", "term", "term-guide"} {
		g.win.AddCssClass(c)
	}
	page := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	page.AddCssClass("term-guide-page")

	head := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	head.AddCssClass("term-guide-head")
	title := gtk.NewLabel(i18n.T("Set up the terminal"))
	title.AddCssClass("term-guide-title")
	title.SetXalign(0)
	title.SetHexpand(true)
	head.Append(&title.Widget)
	g.count = gtk.NewLabel("")
	g.count.AddCssClass("term-guide-count")
	head.Append(&g.count.Widget)
	page.Append(&head.Widget)

	g.stack = gtk.NewStack()
	g.stack.SetTransitionType(gtk.StackTransitionTypeSlideLeftRightValue)
	g.stack.SetVexpand(true)
	s := &a.set
	shells := shellChoices(s.Shell.Program)
	g.stack.AddNamed(g.stepPage(
		i18n.T("Your shell"),
		i18n.T("The shell every new pane starts. inua-term marks the prompts of bash, zsh and fish, so a pane's last command comes back after a restart; other shells run as they are."),
		shells, s.Shell.Program,
		func(v string) {
			s.Shell.Program = v
			g.preview(a)
		}, g.terminal(a)), "shell")
	g.stack.AddNamed(g.stepPage(
		i18n.T("Terminal colours"),
		i18n.T("The colours programs draw with: the window theme's, so the terminal matches the rest of inua, or a classic scheme of its own."),
		schemeChoices(), s.Look.Scheme,
		func(v string) {
			s.Look.Scheme = v
			a.useSettings(*s)
			g.preview(a)
		}, g.terminal(a)), "colours")
	g.stack.AddNamed(g.stepPage(
		i18n.T("Window theme"),
		i18n.T("The look of the window around the terminals. It follows inua's theme, or keeps one of its own whatever inua's is."),
		themeChoices(a.c), s.Look.Theme,
		func(v string) {
			s.Look.Theme = v
			a.useSettings(*s) // the style loads again, and the preview with it
		}, g.windowMock()), "theme")
	page.Append(&g.stack.Widget)

	foot := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	foot.AddCssClass("term-guide-foot")
	skip := gtk.NewButtonWithLabel(i18n.T("Skip"))
	skip.AddCssClass("outlined")
	skip.SetTooltipText(i18n.T("Keep these choices; the settings change them later"))
	spacer := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	spacer.SetHexpand(true)
	g.back = gtk.NewButtonWithLabel(i18n.T("Back"))
	g.back.AddCssClass("outlined")
	g.next = gtk.NewButtonWithLabel("")
	g.next.AddCssClass("suggested-action")
	foot.Append(&skip.Widget)
	foot.Append(&spacer.Widget)
	foot.Append(&g.back.Widget)
	foot.Append(&g.next.Widget)
	page.Append(&foot.Widget)
	g.win.SetChild(&page.Widget)

	g.click(skip, func() { g.win.Close() })
	g.click(g.back, func() { g.show(g.step - 1) })
	g.click(g.next, func() {
		if g.step == len(guideSteps)-1 {
			g.win.Close()
			return
		}
		g.show(g.step + 1)
	})
	keys := gtk.NewEventControllerKey()
	onKey := func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if int32(keyval) == gdk.KEY_Escape {
			g.win.Close()
			return true
		}
		return false
	}
	g.cbs = append(g.cbs, &onKey)
	keys.ConnectKeyPressed(&onKey)
	g.win.AddController(&keys.EventController)
	onClose := func(gtk.Window) bool {
		g.finish(a)
		return false
	}
	g.cbs = append(g.cbs, &onClose)
	g.win.ConnectCloseRequest(&onClose)
	g.show(0)
	g.preview(a)
	g.win.SetDefaultWidget(&g.next.Widget)
	g.win.Present()
}

// show shows step i.
func (g *guide) show(i int) {
	g.step = max(0, min(i, len(guideSteps)-1))
	g.stack.SetVisibleChildName(guideSteps[g.step])
	g.count.SetText(fmt.Sprintf("%d / %d", g.step+1, len(guideSteps)))
	g.back.SetSensitive(g.step > 0)
	if g.step == len(guideSteps)-1 {
		g.next.SetLabel(i18n.T("Start"))
	} else {
		g.next.SetLabel(i18n.T("Next"))
	}
	g.next.GrabFocus()
}

// finish keeps the choices in term.toml and starts the terminal.
func (g *guide) finish(a *App) {
	if a.guide != g {
		return
	}
	a.guide = nil
	if err := settings.Save(settings.Path(), a.set); err != nil {
		log.Printf("term: settings: %v", err)
	}
	gtk.StyleContextRemoveProviderForDisplay(gdk.DisplayGetDefault(), g.css)
	g.done()
}

// stepPage is a step: its title and what it is about over the choices
// (one chosen at a time), with the preview beside them.
func (g *guide) stepPage(title, brief string, choices []choice, current string, chose func(string), preview *gtk.Widget) *gtk.Widget {
	body := gtk.NewBox(gtk.OrientationHorizontalValue, 24)
	body.AddCssClass("term-guide-step")
	left := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	left.SetSizeRequest(330, -1)
	t := gtk.NewLabel(title)
	t.AddCssClass("term-guide-step-title")
	t.SetXalign(0)
	left.Append(&t.Widget)
	b := gtk.NewLabel(brief)
	b.AddCssClass("term-guide-brief")
	b.SetXalign(0)
	b.SetWrap(true)
	b.SetMaxWidthChars(44)
	left.Append(&b.Widget)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetVexpand(true)
	list := gtk.NewBox(gtk.OrientationVerticalValue, 4)
	list.AddCssClass("term-guide-list")
	var first *gtk.ToggleButton
	for _, c := range choices {
		btn := gtk.NewToggleButton()
		btn.AddCssClass("term-guide-choice")
		box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		name := gtk.NewLabel(c.label)
		name.AddCssClass("term-guide-choice-name")
		name.SetXalign(0)
		name.SetEllipsize(3)
		box.Append(&name.Widget)
		if c.brief != "" {
			sub := gtk.NewLabel(c.brief)
			sub.AddCssClass("term-guide-choice-brief")
			sub.SetXalign(0)
			sub.SetEllipsize(3)
			box.Append(&sub.Widget)
		}
		btn.SetChild(&box.Widget)
		if first == nil {
			first = btn
		} else {
			btn.SetGroup(first)
		}
		btn.SetActive(c.value == current)
		value := c.value
		toggled := func(t gtk.ToggleButton) {
			if t.GetActive() {
				chose(value)
			}
		}
		g.cbs = append(g.cbs, &toggled)
		btn.ConnectToggled(&toggled)
		list.Append(&btn.Widget)
	}
	scroll.SetChild(&list.Widget)
	left.Append(&scroll.Widget)
	body.Append(&left.Widget)
	preview.SetHexpand(true)
	preview.SetValign(gtk.AlignCenterValue)
	body.Append(preview)
	return &body.Widget
}

// terminal is a preview of a pane: a prompt of the shell chosen, a few
// commands in the colours chosen, and the 16 colours.
func (g *guide) terminal(a *App) *gtk.Widget {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 12)
	box.AddCssClass("term-guide-term")
	text := gtk.NewLabel("")
	text.AddCssClass("term-guide-term-text")
	text.SetXalign(0)
	text.SetYalign(0)
	text.SetUseMarkup(true)
	box.Append(&text.Widget)
	g.terms = append(g.terms, text)
	for row := range 2 {
		chips := gtk.NewBox(gtk.OrientationHorizontalValue, 4)
		for i := range 8 {
			c := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
			c.AddCssClass("term-guide-chip")
			c.AddCssClass(fmt.Sprintf("term-guide-chip-%d", row*8+i))
			c.SetHexpand(true)
			chips.Append(&c.Widget)
		}
		box.Append(&chips.Widget)
	}
	return &box.Widget
}

// windowMock is a preview of the window: the columns on the panel, a
// tile with its head and the marks in the theme's colours. It is drawn
// by the style, which loads again with the theme chosen.
func (g *guide) windowMock() *gtk.Widget {
	mock := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mock.AddCssClass("term-guide-mock")
	mock.SetSizeRequest(-1, 260)
	panel := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	panel.AddCssClass("g-panel")
	panel.SetSizeRequest(110, -1)
	for i, w := range []int32{70, 52, 84, 60} {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		mark.AddCssClass("mark")
		mark.SetValign(gtk.AlignCenterValue)
		switch i {
		case 0:
			mark.AddCssClass("s-on")
		case 2:
			mark.AddCssClass("s-warn")
		}
		row.Append(&mark.Widget)
		bar := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		bar.AddCssClass("g-text")
		bar.SetSizeRequest(w, 6)
		bar.SetValign(gtk.AlignCenterValue)
		row.Append(&bar.Widget)
		panel.Append(&row.Widget)
	}
	mock.Append(&panel.Widget)
	tile := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	tile.AddCssClass("g-tile")
	tile.SetHexpand(true)
	thead := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	thead.AddCssClass("g-head")
	accent := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	accent.AddCssClass("mark")
	accent.AddCssClass("s-on")
	accent.SetValign(gtk.AlignCenterValue)
	thead.Append(&accent.Widget)
	bar := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	bar.AddCssClass("g-text")
	bar.SetSizeRequest(90, 6)
	bar.SetValign(gtk.AlignCenterValue)
	thead.Append(&bar.Widget)
	tile.Append(&thead.Widget)
	mock.Append(&tile.Widget)
	return &mock.Widget
}

// preview draws the terminal previews in the colours chosen, with the
// prompt of the shell chosen.
func (g *guide) preview(a *App) {
	sc := a.scheme()
	hex := func(v uint32) string { return fmt.Sprintf("#%06x", v) }
	var css strings.Builder
	fmt.Fprintf(&css, "window.term-guide .term-guide-term { background: %s; color: %s; }\n", hex(sc.BG), hex(sc.FG))
	for i, c := range sc.Palette {
		fmt.Fprintf(&css, "window.term-guide .term-guide-chip-%d { background: %s; }\n", i, hex(c))
	}
	g.css.LoadFromString(css.String())
	text := sampleSession(a.set.Shell.Program, sc)
	for _, l := range g.terms {
		l.SetMarkup(text)
	}
}

// themeChanged draws the previews again in the new theme's colours.
func (g *guide) themeChanged(a *App) { g.preview(a) }

// sampleSession is a few commands at the prompt of a shell, as Pango
// markup in a scheme's colours.
func sampleSession(program string, sc scheme.Scheme) string {
	c := func(i int, s string) string {
		return fmt.Sprintf("<span foreground='#%06x'>%s</span>", sc.Palette[i], escape(s))
	}
	user := cmp.Or(os.Getenv("USER"), "you")
	host, _ := os.Hostname()
	host = cmp.Or(host, "localhost")
	var prompt string
	switch filepath.Base(cmp.Or(program, os.Getenv("SHELL"))) {
	case "zsh":
		prompt = c(2, host) + c(8, " ~") + " % "
	case "fish":
		prompt = c(2, user+"@"+host) + " " + c(4, "~") + "&gt; "
	default:
		prompt = c(2, user+"@"+host) + ":" + c(4, "~") + "$ "
	}
	cursor := fmt.Sprintf("<span background='#%06x'> </span>", sc.Cursor)
	lines := []string{
		prompt + "ls",
		c(4, "code") + "  " + c(4, "notes") + "  " + c(2, "build.sh") + "  README.md",
		prompt + "git status --short",
		c(2, " M") + " main.go",
		c(1, "??") + " todo.txt",
		prompt + "echo " + c(3, "\"hello\""),
		"hello",
		prompt + cursor,
	}
	return strings.Join(lines, "\n")
}

func (g *guide) click(b *gtk.Button, f func()) {
	cb := func(gtk.Button) { f() }
	g.cbs = append(g.cbs, &cb)
	b.ConnectClicked(&cb)
}
