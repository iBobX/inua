package gamesui

import (
	"fmt"
	"html"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The panel's square, and the cards over the board. The square in focus:
// its sign, name and kind, who owns it, its rents level by level (the
// level it stands at lit), its group; then building on it. The cards: the
// player's hand along the board's foot, each drawn as printed, its words
// on hover, played with a click; at a shop, what it offers, over the
// middle of the board, bought with a click.

const popTime = 0.6 // a card come into the hand pops in

// iconButton is an outlined button with an icon before its label.
func iconButton(icon string, do *func(gtk.Button)) (*gtk.Button, *gtk.Image, *gtk.Label) {
	b := gtk.NewButton()
	b.AddCssClass("outlined")
	box := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	box.SetHalign(gtk.AlignCenterValue)
	img := gtk.NewImageFromIconName(icon)
	img.SetPixelSize(16)
	l := gtk.NewLabel("")
	box.Append(&img.Widget)
	box.Append(&l.Widget)
	b.SetChild(&box.Widget)
	box.Unref()
	b.ConnectClicked(do)
	return b, img, l
}

// setupSquare lays out the square's part of the panel in box: what stays,
// the sign, name and kind, the owner; the rents, laid out again for each
// square; the buttons that build and sell.
func (p *fortune) setupSquare(box *gtk.Box) {
	info := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	head := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	head.AddCssClass("fortune-square-head")
	p.sign = newCanvas(40, 40, func(pn pen) { paintSquareSign(pn, p.city(), p.focus()) })
	p.sign.area.SetSizeRequest(40, 40)
	p.sign.area.SetHexpand(false)
	p.sign.area.SetVexpand(false)
	p.sign.area.SetFocusable(false)
	p.sign.area.SetValign(gtk.AlignStartValue)
	head.Append(&p.sign.area.Widget)
	names := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	names.SetValign(gtk.AlignCenterValue)
	p.cardTitle = gtk.NewLabel("")
	p.cardTitle.AddCssClass("fortune-card-title")
	p.cardTitle.SetXalign(0)
	p.cardTitle.SetEllipsize(3) // end
	p.cardTitle.SetMaxWidthChars(24)
	names.Append(&p.cardTitle.Widget)
	p.squareKind = gtk.NewLabel("")
	p.squareKind.AddCssClass("fortune-square-kind")
	p.squareKind.SetXalign(0)
	names.Append(&p.squareKind.Widget)
	gtkx.Append(head, &names.Widget)
	gtkx.Append(info, &head.Widget)
	p.squareOwner = gtk.NewLabel("")
	p.squareOwner.AddCssClass("fortune-square-owner")
	p.squareOwner.SetXalign(0)
	p.squareOwner.SetWrap(true)
	info.Append(&p.squareOwner.Widget)
	p.rents = *gtk.NewBox(gtk.OrientationVerticalValue, 1)
	info.Append(&p.rents.Widget)
	p.rents.Unref()
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	sw.SetVexpand(true)
	sw.SetChild(&info.Widget)
	info.Unref()
	gtkx.Append(box, &sw.Widget)

	p.cardButtons = *gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	p.cardButtons.AddCssClass("fortune-buttons")
	p.cardButtons.SetHomogeneous(true)
	p.onBuild = func(gtk.Button) { p.act(func() bool { return p.g.Build(p.focus()) }) }
	p.onSell = func(gtk.Button) { p.act(func() bool { return p.g.Sell(p.focus()) }) }
	p.build, p.buildIcon, p.buildLabel = iconButton("fortune-house-symbolic", &p.onBuild)
	p.sell, _, p.sellLabel = iconButton("fortune-sell-symbolic", &p.onSell)
	gtkx.Append(&p.cardButtons, &p.build.Widget)
	gtkx.Append(&p.cardButtons, &p.sell.Widget)
	gtkx.Append(box, &p.cardButtons.Widget)
}

// refreshSquare shows the square in focus as the board shows it.
func (p *fortune) refreshSquare() {
	g, v, sq := p.g, p.shown, p.focus()
	s := games.Board[sq]
	o := v.Owner[sq]
	houses := v.Houses[sq]
	// A whole group, as the board shows it.
	whole := o >= 0 && s.Kind == games.Street
	if whole {
		for _, i := range games.Group(s.Group) {
			whole = whole && v.Owner[i] == o
		}
	}
	mine := s.Kind == games.Street && o == 0
	idle := !p.busy() && g.Turn == 0
	p.cardButtons.SetVisible(mine)
	p.build.SetSensitive(idle && g.CanBuild(sq))
	p.sell.SetSensitive(idle && g.CanSell(sq))
	switch {
	case houses == games.Hotel:
		p.buildIcon.SetFromIconName("fortune-hotel-symbolic")
		p.buildLabel.SetText(i18n.T("Hotel built"))
	case houses == games.Hotel-1:
		p.buildIcon.SetFromIconName("fortune-hotel-symbolic")
		p.buildLabel.SetText(i18n.Tf("Hotel · $%d", games.HouseCost(sq)))
	default:
		p.buildIcon.SetFromIconName("fortune-house-symbolic")
		p.buildLabel.SetText(i18n.Tf("House · $%d", games.HouseCost(sq)))
	}
	p.sellLabel.SetText(i18n.Tf("Sell · $%d", games.HouseCost(sq)/2))

	key := fmt.Sprint(p.city(), sq, v.Owner, houses, v.Pot, g.OneDie)
	if key == p.squareFor {
		return
	}
	p.squareFor = key
	p.sign.redraw()
	p.cardTitle.SetText(g.SquareName(sq))
	switch s.Kind {
	case games.Street:
		p.squareKind.SetText(i18n.Tf("Street · price $%d · building $%d", s.Price, games.HouseCost(sq)))
	case games.Pier:
		p.squareKind.SetText(i18n.Tf("Station or pier · price $%d", s.Price))
	case games.Utility:
		p.squareKind.SetText(i18n.Tf("Utility · price $%d", s.Price))
	case games.ChanceSquare:
		p.squareKind.SetText(i18n.T("Chance"))
	case games.CardShop:
		p.squareKind.SetText(i18n.T("Card shop"))
	case games.TaxSquare:
		p.squareKind.SetText(i18n.Tf("Tax · $%d", s.Price))
	default:
		p.squareKind.SetText(i18n.T("Corner"))
	}
	dot := func(pi int) string {
		c := p.playerColor(pi)
		return fmt.Sprintf(`<span foreground="#%02x%02x%02x">●</span>`, c.R, c.G, c.B)
	}
	p.squareOwner.SetVisible(games.IsProperty(sq))
	switch {
	case !games.IsProperty(sq):
	case o < 0:
		p.squareOwner.SetMarkup(html.EscapeString(i18n.Tf("For sale: $%d", s.Price)))
	default:
		owns := i18n.Tf("%s owns it", g.Players[o].Name)
		if o == 0 {
			owns = i18n.T("You own it")
		}
		rent := ""
		if s.Kind != games.Utility {
			rent = " · " + html.EscapeString(i18n.T("rent now")) + fmt.Sprintf(" <b>$%s</b>", thousands(rentOf(v, sq)))
		}
		p.squareOwner.SetMarkup(dot(o) + " " + html.EscapeString(owns) + rent)
	}

	gtkx.Clear(&p.rents)
	// The rents, a tier to a cell, two to a row: the tier in icons (its
	// words on hover), what it charges; lit when it is how the square
	// stands.
	tiers := gtk.NewGrid()
	tiers.AddCssClass("fortune-rents")
	tiers.SetColumnHomogeneous(true)
	tiers.SetColumnSpacing(6)
	tiers.SetRowSpacing(4)
	gtkx.Append(&p.rents, &tiers.Widget)
	n := 0
	icon := func(box *gtk.Box, name string) {
		img := gtk.NewImageFromIconName(name)
		img.SetPixelSize(14)
		gtkx.Append(box, &img.Widget)
	}
	cell := func(icons []string, dice bool, amount, words string, now bool) {
		c := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		c.AddCssClass("fortune-rent")
		if now {
			c.AddCssClass("now")
		}
		c.SetTooltipText(words)
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 1)
		row.SetHexpand(true)
		for _, name := range icons {
			icon(row, name)
		}
		gtkx.Append(c, &row.Widget)
		if dice {
			icon(c, "fortune-dice-symbolic")
		}
		a := gtk.NewLabel(amount)
		a.AddCssClass("fortune-rent-amount")
		gtkx.Append(c, &a.Widget)
		tiers.Attach(&c.Widget, int32(n%2), int32(n/2), 1, 1)
		c.Unref()
		n++
	}
	repeat := func(name string, k int) []string { return slices.Repeat([]string{name}, k) }
	note := func(text string) {
		l := gtk.NewLabel("")
		l.SetMarkup(text)
		l.AddCssClass("fortune-square-note")
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetMaxWidthChars(36)
		gtkx.Append(&p.rents, &l.Widget)
	}
	count := func(kind games.SquareKind) int {
		n := 0
		for i, w := range v.Owner {
			if w == o && games.Board[i].Kind == kind {
				n++
			}
		}
		return n
	}
	switch s.Kind {
	case games.Street:
		for lvl := 0; lvl <= games.Hotel; lvl++ {
			icons, words := []string{"fortune-lot-symbolic"}, i18n.T("Bare")
			switch {
			case lvl == games.Hotel:
				icons, words = []string{"fortune-hotel-symbolic"}, i18n.T("Hotel")
			case lvl > 0:
				icons, words = repeat("fortune-house-symbolic", lvl), i18n.Nf("%d house", "%d houses", lvl, lvl)
			}
			cell(icons, false, "$"+thousands(games.RentAt(sq, lvl, whole)), words, o >= 0 && lvl == houses)
		}
		if whole {
			note(html.EscapeString(i18n.T("The whole group: bare rent doubled, built rents half again.")))
		} else {
			note(html.EscapeString(i18n.T("With the whole group: bare rent doubled, built rents half again.")))
		}
		var group []string
		for _, i := range games.Group(s.Group) {
			if i == sq {
				continue
			}
			name := html.EscapeString(g.SquareName(i))
			if w := v.Owner[i]; w >= 0 {
				name = dot(w) + " " + name
			}
			group = append(group, name)
		}
		note(html.EscapeString(i18n.T("Group:")) + " " + strings.Join(group, ",  "))
	case games.Pier:
		have := count(games.Pier)
		for k := 1; k <= 4; k++ {
			cell(repeat("fortune-pier-symbolic", k), false, "$"+thousands(games.PierRent(k)), i18n.Nf("%d pier", "%d piers", k, k), o >= 0 && k == have)
		}
		note(html.EscapeString(i18n.T("The rent is by how many piers the owner has.")))
	case games.Utility:
		have := count(games.Utility)
		for k, icons := range [][]string{{"fortune-power-symbolic"}, {"fortune-power-symbolic", "fortune-water-symbolic"}} {
			cell(icons, true, i18n.Tf("× %d", games.UtilityTimes(k+1)), i18n.Nf("%d utility", "%d utilities", k+1, k+1), o >= 0 && k+1 == have)
		}
		note(html.EscapeString(i18n.T("The rent is the dice times this.")))
	case games.StartSquare:
		note(html.EscapeString(i18n.Tf("Collect $%d every time you pass.", games.StartPay)))
	case games.JailSquare:
		note(html.EscapeString(i18n.Tf("Only visiting, unless sent here. In jail, roll %s to leave, or pay $50 before rolling; after three tries you pay and go.",
			map[bool]string{false: i18n.T("a double"), true: i18n.T("a six")}[g.OneDie])))
	case games.Fountain:
		note(html.EscapeString(i18n.Tf("Taxes and fines go into the fountain; the next player to land here takes it all: $%d now.", v.Pot)))
	case games.GoToJail:
		note(html.EscapeString(i18n.T("Straight to jail, without passing Start.")))
	case games.ChanceSquare:
		note(html.EscapeString(i18n.T("Draw a card: money, a fine, a trip, or a card for your hand.")))
	case games.CardShop:
		note(html.EscapeString(i18n.Tf("Three cards on offer: buy any of them, or none. A hand holds up to %d cards.", games.HandMax)))
	case games.TaxSquare:
		note(html.EscapeString(i18n.Tf("Pay $%d into the fountain.", s.Price)))
	}
}

// paintSquareSign draws the sign of square sq: a tile of the board in
// small, a street's band in its group's colour, the square's mark.
func paintSquareSign(pn pen, city, sq int) {
	s := games.Board[sq]
	art := cityArts[city]
	pn.RoundRect(0.5, 0.5, 39, 39, 4)
	pn.Color(washiTile)
	pn.FillKeep()
	pn.Save()
	pn.Clip()
	if s.Kind == games.Street {
		pn.Color(groupColors[s.Group])
		pn.Rect(0, 0, 40, 12)
		pn.Fill()
	}
	pn.Restore()
	pn.RoundRect(0.5, 0.5, 39, 39, 4)
	pn.Width(1)
	pn.Alpha(washiInk, 0.6)
	pn.Stroke()
	switch s.Kind {
	case games.Street: // a little house
		pn.MoveTo(12, 34)
		pn.LineTo(12, 24)
		pn.LineTo(20, 17)
		pn.LineTo(28, 24)
		pn.LineTo(28, 34)
		pn.Close()
		pn.Color(washiIvory)
		pn.FillKeep()
		pn.Width(1.2)
		pn.Color(washiInk)
		pn.Stroke()
		pn.Rect(18, 28, 4, 6)
		pn.Fill()
	case games.Pier:
		ship(pn, 20, 22, washiIndigo)
	case games.Utility:
		if sq < games.FountainAt {
			bolt(pn, 20, 20, groupColors[6])
		} else {
			drop(pn, 20, 21, washiWater)
		}
	case games.ChanceSquare:
		pn.Text(art.chance, minchoBold+" 16", 20, 20, 0.5, 0.5, washiShu)
	case games.TaxSquare:
		pn.Text(art.tax, minchoBold+" 16", 20, 20, 0.5, 0.5, washiInk)
	case games.CardShop:
		shopSign(pn, 20, 22, art.shop)
	default:
		pn.TextBox(art.corners[sq/games.Side], minchoBold+" 9", 20, 13, 36, washiShu)
	}
}

// setupHand lays the cards over the board in ov: the hand along its foot,
// the shop over its middle.
func (p *fortune) setupHand(ov *gtk.Overlay) {
	hand := gtk.NewBox(gtk.OrientationVerticalValue, 4)
	hand.AddCssClass("fortune-hand")
	hand.SetHalign(gtk.AlignCenterValue)
	hand.SetValign(gtk.AlignEndValue)
	p.handCaption = gtk.NewLabel("")
	p.handCaption.AddCssClass("fortune-hand-caption")
	p.handCaption.SetHalign(gtk.AlignCenterValue)
	hand.Append(&p.handCaption.Widget)
	p.hand = *gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	hand.Append(&p.hand.Widget)
	p.hand.Unref()
	p.handBar = hand
	ov.AddOverlay(&hand.Widget)

	shop := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	shop.AddCssClass("fortune-shop")
	shop.SetHalign(gtk.AlignCenterValue)
	shop.SetValign(gtk.AlignStartValue) // over the top, clear of the card coming into the hand
	title := gtk.NewLabel(i18n.T("Card Shop"))
	title.AddCssClass("fortune-shop-title")
	gtkx.Append(shop, &title.Widget)
	sub := gtk.NewLabel(i18n.T("Click a card to buy it: any of them, or none."))
	sub.AddCssClass("fortune-shop-sub")
	gtkx.Append(shop, &sub.Widget)
	p.offer = *gtk.NewBox(gtk.OrientationHorizontalValue, 14)
	p.offer.SetHalign(gtk.AlignCenterValue)
	shop.Append(&p.offer.Widget)
	p.offer.Unref()
	p.onLeaveShop = func(gtk.Button) { p.act(p.g.LeaveShop) }
	leave := button(i18n.T("Leave the shop"), &p.onLeaveShop)
	leave.SetHalign(gtk.AlignCenterValue)
	gtkx.Append(shop, &leave.Widget)
	p.shop = shop
	shop.SetVisible(false)
	ov.AddOverlay(&shop.Widget)

	p.onCard = func(b gtk.Button) {
		var i, t int
		if _, err := fmt.Sscanf(b.GetName(), "buy:%d", &i); err == nil {
			p.act(func() bool { return p.g.BuyCard(i) })
		} else if _, err := fmt.Sscanf(b.GetName(), "play:%d:%d", &i, &t); err == nil {
			p.act(func() bool {
				if !p.g.Play(i, t) {
					return false
				}
				// It leaves the hand at once, for the board.
				p.handShown = slices.Delete(slices.Clone(p.handShown), i, i+1)
				born := map[int]time.Time{}
				for k, at := range p.handBorn {
					if k > i {
						born[k-1] = at
					} else if k < i {
						born[k] = at
					}
				}
				p.handBorn = born
				return true
			})
		}
	}
}

// refreshHand lays out the cards: at a shop, what it offers; then the
// player's hand as the board has shown it. Only when what it shows has
// changed, so a menu open on it stays open; a card new to the hand pops in.
func (p *fortune) refreshHand() {
	g := p.g
	if !p.busy() {
		was := p.handShown
		p.handShown = slices.Clone(g.Players[0].Cards)
		// The cards come into the hand: those not in it before.
		left := slices.Clone(was)
		born := map[int]time.Time{}
		for i, c := range p.handShown {
			if k := slices.Index(left, c); k >= 0 {
				left = slices.Delete(left, k, k+1)
				if t, ok := p.handBorn[slices.Index(was, c)]; ok {
					born[i] = t
				}
			} else if p.handFor != "" {
				born[i] = time.Now()
			}
		}
		p.handBorn = born
	}
	mine := g.Turn == 0 && !p.busy() && g.Phase != games.PhaseOver
	sq := p.focus()
	key := fmt.Sprint(mine, g.Phase, g.Turn, g.Offer, p.handShown, g.Players, sq, g.Owner, g.Houses)
	if key == p.handFor {
		return
	}
	p.handFor = key
	gtkx.Clear(&p.hand)
	gtkx.Clear(&p.offer)
	p.handCards = nil
	tip := func(c games.CardKind, more string) string {
		t := fmt.Sprintf("<b>%s</b>\n%s", html.EscapeString(i18n.T(games.Cards[c].Name)), html.EscapeString(i18n.T(games.Cards[c].Text)))
		if more != "" {
			t += "\n<i>" + html.EscapeString(more) + "</i>"
		}
		return t
	}
	shopping := g.Phase == games.PhaseShop && g.Turn == 0
	p.shop.SetVisible(shopping)
	if shopping {
		for i, c := range g.Offer {
			price := games.Cards[c].Price
			b := gtk.NewButton()
			b.AddCssClass("fortune-card-button")
			b.SetName(fmt.Sprintf("buy:%d", i))
			b.SetChild(&p.cardFace(c, price, 1.35, time.Time{}).area.Widget)
			b.SetTooltipMarkup(tip(c, i18n.Tf("Buy it for $%d", price)))
			b.SetSensitive(mine && g.CanBuyCard(i))
			b.ConnectClicked(&p.onCard)
			gtkx.Append(&p.offer, &b.Widget)
		}
	}
	cards := p.handShown
	p.handBar.SetVisible(len(cards) > 0)
	if mine && !shopping {
		p.handCaption.SetText(i18n.Tf("Your cards · %d of %d · click one to play it", len(cards), games.HandMax))
	} else {
		p.handCaption.SetText(i18n.Tf("Your cards · %d of %d", len(cards), games.HandMax))
	}
	for i, c := range cards {
		face := &p.cardFace(c, 0, 1, p.handBorn[i]).area.Widget
		var w *gtk.Widget
		switch games.Cards[c].Target {
		case games.TargetNone, games.TargetSquare:
			t, more := 0, ""
			if games.Cards[c].Target == games.TargetSquare {
				t, more = sq, i18n.Tf("Plays on the square in focus: %s.", g.SquareName(sq))
			}
			b := gtk.NewButton()
			b.SetName(fmt.Sprintf("play:%d:%d", i, t))
			b.SetChild(face)
			b.SetTooltipMarkup(tip(c, more))
			b.SetSensitive(mine && g.CanPlay(i, t))
			b.ConnectClicked(&p.onCard)
			w = &b.Widget
		default: // a player or steps: a menu of them
			var targets []int
			if mine {
				targets = g.Targets(i)
			}
			mb := gtk.NewMenuButton()
			mb.SetChild(face)
			mb.SetTooltipMarkup(tip(c, ""))
			mb.SetSensitive(len(targets) > 0)
			pop := gtk.NewPopover()
			box := gtk.NewGrid()
			box.SetRowSpacing(4)
			box.SetColumnSpacing(4)
			ask := i18n.T("On whom?")
			if games.Cards[c].Target == games.TargetSteps {
				ask = i18n.T("How many squares?")
			}
			head := gtk.NewLabel(ask)
			head.AddCssClass("fortune-ask")
			head.SetXalign(0)
			box.Attach(&head.Widget, 0, 0, 6, 1)
			head.Unref()
			for n, t := range targets {
				label := fmt.Sprint(t)
				if games.Cards[c].Target == games.TargetPlayer {
					label = g.Players[t].Name
				}
				b := button(label, &p.onCard)
				b.SetName(fmt.Sprintf("play:%d:%d", i, t))
				if games.Cards[c].Target == games.TargetSteps {
					box.Attach(&b.Widget, int32(n%6), int32(1+n/6), 1, 1)
				} else {
					box.Attach(&b.Widget, 0, int32(1+n), 6, 1)
				}
				b.Unref()
			}
			pop.SetChild(&box.Widget)
			box.Unref()
			mb.SetPopover(pop)
			pop.Unref()
			w = &mb.Widget
		}
		w.AddCssClass("fortune-card-button")
		gtkx.Append(&p.hand, w)
	}
}

// cardFace is a card drawn k times its size on a canvas of its own; born
// lately, it pops in.
func (p *fortune) cardFace(c games.CardKind, price int, k float64, born time.Time) *canvas {
	cv := newCanvas(cardW+4, cardH+4, func(pn pen) {
		pn.Translate(2, 2)
		if a := time.Since(born).Seconds(); !born.IsZero() && a < popTime {
			f := a / popTime
			k := 0.6 + 0.4*easeOutBack(f)
			pn.Glow(cardW/2, cardH/2, cardW*0.9, washiGold, 0.5*(1-f))
			pn.Translate(cardW/2*(1-k), cardH/2*(1-k))
			pn.Scale(k, k)
		}
		paintCard(pn, c, false, price)
	})
	cv.area.SetSizeRequest(int32((cardW+4)*k), int32((cardH+4)*k))
	cv.area.SetHexpand(false)
	cv.area.SetVexpand(false)
	cv.area.SetFocusable(false)
	if !born.IsZero() && time.Since(born).Seconds() < popTime {
		cv.animate(func(float64) bool { return time.Since(born).Seconds() < popTime })
	}
	p.handCards = append(p.handCards, cv)
	return cv
}

// easeOutBack overshoots a little and settles, for f from 0 to 1.
func easeOutBack(f float64) float64 {
	const c = 1.7
	f--
	return 1 + (c+1)*f*f*f + c*f*f
}
