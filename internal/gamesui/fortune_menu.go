package gamesui

import (
	"fmt"
	"html"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// Small Fortune's game menu, at the top right where the other games have
// New game: Restart plays the same game again from the start (the same
// city, rules and seats; on the network, the same visitors); Compendium
// shows every card, the kinds of squares and the rules; Quit to menu goes
// back to the setup.

// setupMenu puts the game menu in the header in place of New game.
func (p *fortune) setupMenu(f *frame) {
	f.restart.SetVisible(false)
	head := gtk.BoxNewFromInternalPtr(f.restart.GetParent().GoPointer())
	p.menu = gtk.NewMenuButton()
	p.menu.SetLabel(i18n.T("Game"))
	p.menu.AddCssClass("fortune-menu")
	p.menu.SetValign(gtk.AlignCenterValue)
	pop := gtk.NewPopover()
	pop.AddCssClass("fortune-menu-pop")
	items := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	p.onMenu = func(b gtk.Button) {
		p.menu.Popdown()
		switch b.GetName() {
		case "restart":
			p.restart()
		case "compendium":
			p.showCompendium()
		case "quit":
			p.reset()
		}
	}
	for _, it := range []struct{ name, label string }{
		{"restart", i18n.T("Restart game")},
		{"compendium", i18n.T("Compendium")},
		{"quit", i18n.T("Quit to menu")},
	} {
		b := gtk.NewButtonWithLabel(it.label)
		b.SetName(it.name)
		b.AddCssClass("flat")
		b.AddCssClass("fortune-menu-item")
		b.ConnectClicked(&p.onMenu)
		if it.name == "restart" {
			p.restartItem = b
		}
		gtkx.Append(items, &b.Widget)
	}
	pop.SetChild(&items.Widget)
	p.menu.SetPopover(pop)
	head.Append(&p.menu.Widget)
	p.onMenuOpen = func(uintptr, uintptr, uintptr) {
		p.restartItem.SetSensitive(p.pages.GetVisibleChildName() == "play" && p.g != nil)
	}
	gtkx.ConnectNotify(p.menu, "active", &p.onMenuOpen)
}

// restart plays the game shown again from its start: the same setup, or
// on the network the same seats, visitors and all.
func (p *fortune) restart() {
	if p.g == nil {
		return
	}
	if p.net != nil {
		if p.net.Restart() {
			p.newGame(p.net.Game, p.here)
		}
		return
	}
	p.begin()
}

// showCompendium turns to the compendium; its back button returns to the
// page it was opened from.
func (p *fortune) showCompendium() {
	if name := p.pages.GetVisibleChildName(); name != "compendium" {
		p.backTo = name
	}
	p.pages.SetVisibleChildName("compendium")
}

// setupCompendium is the compendium's page: every card as printed, with
// its price and what it does; each kind of square with its sign and what
// it does; the rules.
func (p *fortune) setupCompendium() *gtk.ScrolledWindow {
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	page := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	page.AddCssClass("fortune-setup")
	page.AddCssClass("fortune-compendium")
	page.SetHalign(gtk.AlignCenterValue)
	page.SetSizeRequest(980, -1)

	head := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	title := gtk.NewLabel(i18n.T("Compendium"))
	title.AddCssClass("fortune-setup-title")
	title.SetXalign(0)
	title.SetHexpand(true)
	gtkx.Append(head, &title.Widget)
	p.onBack = func(gtk.Button) { p.pages.SetVisibleChildName(p.backTo) }
	back := button(i18n.T("Back"), &p.onBack)
	back.SetValign(gtk.AlignCenterValue)
	gtkx.Append(head, &back.Widget)
	gtkx.Append(page, &head.Widget)

	section := func(name string) *gtk.Box {
		box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		box.AddCssClass("fortune-setup-col")
		box.AddCssClass("fortune-compendium-section")
		k := gtk.NewLabel(name)
		k.AddCssClass("kicker")
		k.AddCssClass("fortune-kicker")
		k.SetXalign(0)
		gtkx.Append(box, &k.Widget)
		gtkx.Append(page, &box.Widget)
		return box
	}

	// The cards, three to a row: the card, then its name, price and
	// words.
	cards := gtk.NewGrid()
	cards.SetColumnSpacing(18)
	cards.SetRowSpacing(14)
	cards.SetColumnHomogeneous(true)
	for i := range games.Cards {
		c := games.CardKind(i)
		info := games.Cards[c]
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		cv := newCanvas(cardW+4, cardH+4, func(pn pen) {
			pn.Translate(2, 2)
			paintCard(pn, c, false, 0)
		})
		cv.area.SetSizeRequest(int32(cardW+4), int32(cardH+4))
		cv.area.SetFocusable(false)
		cv.area.SetValign(gtk.AlignStartValue)
		cv.area.SetHexpand(false)
		cv.area.SetHalign(gtk.AlignStartValue)
		p.cities = append(p.cities, cv) // redrawn with the theme
		gtkx.Append(row, &cv.area.Widget)
		words := gtk.NewBox(gtk.OrientationVerticalValue, 3)
		words.SetHexpand(true)
		name := gtk.NewLabel(fmt.Sprintf("%s · $%s", i18n.T(info.Name), thousands(info.Price)))
		name.AddCssClass("fortune-city-name")
		name.SetXalign(0)
		gtkx.Append(words, &name.Widget)
		text := gtk.NewLabel(i18n.T(info.Text))
		text.AddCssClass("fortune-square-note")
		text.SetWrap(true)
		text.SetMaxWidthChars(26)
		text.SetXalign(0)
		gtkx.Append(words, &text.Widget)
		gtkx.Append(row, &words.Widget)
		cards.Attach(&row.Widget, int32(i%3), int32(i/3), 1, 1)
	}
	gtkx.Append(section(i18n.T("CARDS")), &cards.Widget)

	// The squares, one of each kind, two to a row.
	squares := gtk.NewGrid()
	squares.SetColumnSpacing(18)
	squares.SetRowSpacing(12)
	squares.SetColumnHomogeneous(true)
	kinds := []struct {
		sq         int
		name, note string
	}{
		{0, i18n.T("Start"), i18n.Tf("Collect $%d every time you pass.", games.StartPay)},
		{games.JailAt, i18n.T("Jail"), i18n.Tf("Only visiting, unless sent here. In jail, roll %s to leave, or pay $50 before rolling; after three tries you pay and go.", i18n.T("a double"))},
		{games.FountainAt, i18n.T("Fountain"), i18n.T("Taxes and fines go into the fountain; the next player to land there takes them all.")},
		{3 * games.Side, i18n.T("Go to Jail"), i18n.T("Straight to jail, without passing Start.")},
		{1, i18n.T("Street"), i18n.T("With the whole group: bare rent doubled, built rents half again.")},
		{10, i18n.T("Station or pier"), i18n.T("The rent is by how many piers the owner has.")},
		{25, i18n.T("Utility"), i18n.T("The rent is the dice times this.")},
		{2, i18n.T("Chance"), i18n.T("Draw a card: money, a fine, a trip, or a card for your hand.")},
		{7, i18n.T("Card shop"), i18n.Tf("Three cards on offer: buy any of them, or none. A hand holds up to %d cards.", games.HandMax)},
		{5, i18n.T("Income Tax"), i18n.Tf("Pay $%d into the fountain.", games.Board[5].Price)},
	}
	for n, k := range kinds {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		sq := k.sq
		cv := newCanvas(40, 40, func(pn pen) { paintSquareSign(pn, p.city(), sq) })
		cv.area.SetSizeRequest(40, 40)
		cv.area.SetFocusable(false)
		cv.area.SetValign(gtk.AlignStartValue)
		cv.area.SetHexpand(false)
		cv.area.SetHalign(gtk.AlignStartValue)
		p.cities = append(p.cities, cv)
		gtkx.Append(row, &cv.area.Widget)
		words := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		words.SetHexpand(true)
		name := gtk.NewLabel(k.name)
		name.AddCssClass("fortune-city-name")
		name.SetXalign(0)
		gtkx.Append(words, &name.Widget)
		text := gtk.NewLabel(k.note)
		text.AddCssClass("fortune-square-note")
		text.SetWrap(true)
		text.SetMaxWidthChars(40)
		text.SetXalign(0)
		gtkx.Append(words, &text.Widget)
		gtkx.Append(row, &words.Widget)
		squares.Attach(&row.Widget, int32(n%2), int32(n/2), 1, 1)
	}
	gtkx.Append(section(i18n.T("SQUARES")), &squares.Widget)

	// The computer players, each its character.
	rivals := gtk.NewGrid()
	rivals.SetColumnSpacing(18)
	rivals.SetRowSpacing(12)
	rivals.SetColumnHomogeneous(true)
	for i, r := range games.Rivals {
		words := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		words.SetHexpand(true)
		name := gtk.NewLabel("")
		col := p.playerColor(i + 1)
		name.SetMarkup(fmt.Sprintf(`<span foreground="#%02x%02x%02x">●</span>  %s · %s`, col.R, col.G, col.B,
			html.EscapeString(i18n.T(r.Name)), html.EscapeString(i18n.T(r.Title))))
		name.AddCssClass("fortune-city-name")
		name.SetXalign(0)
		gtkx.Append(words, &name.Widget)
		text := gtk.NewLabel(i18n.T(r.About))
		text.AddCssClass("fortune-square-note")
		text.SetWrap(true)
		text.SetMaxWidthChars(30)
		text.SetXalign(0)
		gtkx.Append(words, &text.Widget)
		rivals.Attach(&words.Widget, int32(i), 0, 1, 1)
	}
	gtkx.Append(section(i18n.T("RIVALS")), &rivals.Widget)

	rules := gtk.NewLabel(p.f.rules.GetText())
	rules.AddCssClass("fortune-compendium-rules")
	rules.SetWrap(true)
	rules.SetXalign(0)
	gtkx.Append(section(i18n.T("RULES")), &rules.Widget)

	sw.SetChild(&page.Widget)
	return sw
}
