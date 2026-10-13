package gamesui

import (
	"fmt"
	"html"
	"image/color"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// fortune is Small Fortune: the board on a canvas, where the dice roll
// and the tokens walk, and a panel with the players, the square in
// focus, what can be done, and what happened.
type fortune struct {
	f        *frame
	g        *games.Fortune // nil while the game is being set up
	cv       *canvas
	cam      camera
	settings games.FortuneSettings
	pages    *gtk.Stack // "setup", then "play"

	// The board shows events one after another: queue waits, cur plays.
	queue   []games.FortuneEvent
	cur     *games.FortuneEvent
	t, wait float64
	shown   games.FortuneView
	dice    [2]int
	log     []logLine

	selected, hover int

	// The board's print, drawn once at printScale pixels to a unit, for
	// printCity.
	print      *image
	printScale float64
	printCity  int
	// The still layer: the board as it stands, for underFor, at underScale
	// pixels to a canvas unit; canvasScale is the canvas's own.
	under       *image
	underFor    string
	underState  string
	underScale  float64
	canvasScale float64
	zoomedAt    time.Time // when the camera's zoom last changed

	fx   []moneyFX // money won and lost, flying off the pawns
	idle bool      // the board shows the game as it is

	round, pot                      *gtk.Label
	players, logBox, cardButtons    gtk.Box
	rows                            []*gtk.Box
	names, cashes                   []*gtk.Label
	cardTitle, cardBody             *gtk.Label
	primary, secondary, build, sell *gtk.Button

	// The setup: a card for each city, the choices, Start.
	cities      []*canvas
	cards       map[int]*image // each city's card, drawn once at the screen's scale
	cardAt      float64
	picks       map[string][]*gtk.ToggleButton
	start       *gtk.Button
	onPick      func(gtk.ToggleButton)
	onStart     func(gtk.Button)
	onBuildRule func(gtk.Switch, bool) bool

	onPrimary, onSecondary, onBuild, onSell func(gtk.Button)
	onClick                                 func(gtk.GestureClick, int32, float64, float64)
	onMotion                                func(gtk.EventControllerMotion, float64, float64)
	onLeave                                 func(gtk.EventControllerMotion)
}

type logLine struct {
	text      string
	good, bad bool
}

// The streets' colour groups, in the old pigments of washi prints:
// kogecha, hanada, sakura, yamabuki, matcha, seiji, fuji, shu round the
// ring; asagi, botan, uguisu, kaki on the branch roads.
var groupColors = [12]color.NRGBA{
	{0x8A, 0x5A, 0x3A, 255}, {0x6C, 0x8C, 0xAB, 255}, {0xC2, 0x7D, 0x8B, 255}, {0xD4, 0x9B, 0x39, 255},
	{0x7A, 0x97, 0x58, 255}, {0x2F, 0x6C, 0x68, 255}, {0x5C, 0x58, 0x8C, 255}, {0xBF, 0x44, 0x30, 255},
	{0x4A, 0x9B, 0xA8, 255}, {0xB5, 0x51, 0x7E, 255}, {0x7D, 0x7A, 0x3A, 255}, {0xD3, 0x79, 0x3D, 255},
}

// playerColor is a player's colour, the board's own: vermilion, indigo,
// ochre, wisteria.
func (p *fortune) playerColor(i int) color.NRGBA {
	return [...]color.NRGBA{{0xC2, 0x3B, 0x22, 255}, {0x2B, 0x4C, 0x7E, 255}, {0xC8, 0x8A, 0x1E, 255}, {0x7B, 0x5E, 0xA7, 255}}[i]
}

func (p *fortune) setup(f *frame) {
	p.f, p.settings, p.selected, p.hover = f, games.DefaultFortune, -1, -1
	f.win.SetDefaultSize(1440, 900)
	f.status.SetVisible(false) // the board and the panel say it all
	f.hint.SetVisible(false)
	p.round = f.stat(i18n.T("ROUND"))
	p.pot = f.stat(i18n.T("FOUNTAIN"))

	p.pages = gtk.NewStack()
	p.pages.SetTransitionType(gtk.StackTransitionTypeCrossfadeValue)
	p.pages.SetTransitionDuration(220)
	p.pages.SetVexpand(true)
	p.pages.AddNamed(&p.setupPage().Widget, "setup")

	row := gtk.NewBox(gtk.OrientationHorizontalValue, 18)
	p.cv = newCanvas(boardW, boardH, p.paint)
	gtkx.Append(row, p.setupCamera())
	// The panel: players, the square in focus with what can be done, what
	// happened; a third of its height each.
	panel := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	panel.AddCssClass("fortune-panel")
	panel.SetHomogeneous(true)
	panel.SetSizeRequest(300, -1)
	panel.SetHexpand(false)
	gtkx.Append(row, &panel.Widget)
	p.pages.AddNamed(&row.Widget, "play")
	row.Unref()
	gtkx.Append(&f.body, &p.pages.Widget)

	section := func(name string) *gtk.Box {
		box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		box.AddCssClass("fortune-section")
		k := gtk.NewLabel(name)
		k.AddCssClass("kicker")
		k.AddCssClass("fortune-kicker")
		k.SetXalign(0)
		gtkx.Append(box, &k.Widget)
		gtkx.Append(panel, &box.Widget)
		return box
	}
	scroll := func(box *gtk.Box, child *gtk.Widget) {
		sw := gtk.NewScrolledWindow()
		sw.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
		sw.SetVexpand(true)
		sw.SetChild(child)
		gtkx.Append(box, &sw.Widget)
	}

	players := section(i18n.T("PLAYERS"))
	p.players = *gtk.NewBox(gtk.OrientationVerticalValue, 0)
	scroll(players, &p.players.Widget)
	p.players.Unref()

	square := section(i18n.T("SQUARE"))
	card := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	p.cardTitle = gtk.NewLabel("")
	p.cardTitle.AddCssClass("fortune-card-title")
	p.cardTitle.SetXalign(0)
	p.cardTitle.SetEllipsize(3) // end
	p.cardTitle.SetMaxWidthChars(28)
	gtkx.Append(card, &p.cardTitle.Widget)
	p.cardBody = gtk.NewLabel("")
	p.cardBody.AddCssClass("fortune-card")
	p.cardBody.SetXalign(0)
	p.cardBody.SetWrap(true)
	p.cardBody.SetMaxWidthChars(36)
	gtkx.Append(card, &p.cardBody.Widget)
	scroll(square, &card.Widget)
	card.Unref()
	// Building on the square, then the turn's moves.
	p.cardButtons = *gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	p.cardButtons.AddCssClass("fortune-buttons")
	p.cardButtons.SetHomogeneous(true)
	p.onBuild = func(gtk.Button) { p.act(func() bool { return p.g.Build(p.focus()) }) }
	p.onSell = func(gtk.Button) { p.act(func() bool { return p.g.Sell(p.focus()) }) }
	p.build = button(i18n.T("Build a house"), &p.onBuild)
	p.sell = button(i18n.T("Sell"), &p.onSell)
	gtkx.Append(&p.cardButtons, &p.build.Widget)
	gtkx.Append(&p.cardButtons, &p.sell.Widget)
	gtkx.Append(square, &p.cardButtons.Widget)
	acts := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	acts.AddCssClass("fortune-acts")
	acts.SetHomogeneous(true)
	p.onPrimary = func(gtk.Button) { p.main() }
	p.onSecondary = func(gtk.Button) { p.other() }
	p.primary = button(i18n.T("Roll the dice"), &p.onPrimary)
	p.primary.RemoveCssClass("outlined")
	p.primary.AddCssClass("primary")
	p.primary.AddCssClass("fortune-main")
	p.secondary = button("", &p.onSecondary)
	p.secondary.AddCssClass("fortune-main")
	gtkx.Append(acts, &p.primary.Widget)
	gtkx.Append(acts, &p.secondary.Widget)
	gtkx.Append(square, &acts.Widget)

	happened := section(i18n.T("WHAT HAPPENED"))
	p.logBox = *gtk.NewBox(gtk.OrientationVerticalValue, 4)
	scroll(happened, &p.logBox.Widget)
	p.logBox.Unref()

	f.keyDown = p.key
	p.onClick = func(_ gtk.GestureClick, _ int32, x, y float64) {
		if sq := squareAtScreen(p.city(), p.cam.seen(p.cv.at(x, y))); sq >= 0 && p.g != nil {
			p.selected = sq
			p.refresh()
		}
	}
	click := gtk.NewGestureClick()
	click.ConnectReleased(&p.onClick) // a drag pans instead
	p.cv.area.AddController(&click.EventController)
	p.onMotion = func(_ gtk.EventControllerMotion, x, y float64) {
		p.cam.pointer = p.cv.at(x, y)
		if sq := squareAtScreen(p.city(), p.cam.seen(p.cam.pointer)); sq != p.hover {
			p.hover = sq
			p.cv.redraw()
		}
	}
	p.onLeave = func(gtk.EventControllerMotion) {
		p.hover = -1
		p.cv.redraw()
	}
	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(&p.onMotion)
	motion.ConnectLeave(&p.onLeave)
	p.cv.area.AddController(&motion.EventController)
}

// setupPage is the page before a game: the cities, each a card with its
// board's middle, then the rounds, the starting money, the rivals, and
// Start.
func (p *fortune) setupPage() *gtk.Box {
	page := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	page.AddCssClass("fortune-setup")
	page.SetHalign(gtk.AlignCenterValue)
	page.SetValign(gtk.AlignCenterValue)
	head := gtk.NewLabel(i18n.T("Where do you play?"))
	head.AddCssClass("fortune-setup-title")
	head.SetXalign(0)
	gtkx.Append(page, &head.Widget)

	p.picks = map[string][]*gtk.ToggleButton{}
	p.onPick = func(b gtk.ToggleButton) {
		if !b.GetActive() {
			return
		}
		kind, n, _ := strings.Cut(b.GetName(), ":")
		i := int(n[0] - '0')
		switch kind {
		case "city":
			p.settings.City = i
		case "rounds":
			p.settings.Rounds = games.FortuneRounds[i]
		case "cash":
			p.settings.Cash = games.FortuneCash[i]
		case "rivals":
			p.settings.Rivals = i + 1
		case "dice":
			p.settings.Dice = i + 1
		}
		p.start.SetLabel(i18n.Tf("Start in %s", i18n.T(games.FortuneCities[p.settings.City].Name)))
	}
	pick := func(kind string, i int, active bool, child *gtk.Widget) *gtk.ToggleButton {
		b := gtk.NewToggleButton()
		b.SetName(fmt.Sprintf("%s:%d", kind, i))
		b.SetChild(child)
		if group := p.picks[kind]; len(group) > 0 {
			b.SetGroup(group[0])
		}
		b.SetActive(active)
		b.ConnectToggled(&p.onPick)
		p.picks[kind] = append(p.picks[kind], b)
		return b
	}

	cities := gtk.NewBox(gtk.OrientationHorizontalValue, 14)
	cities.SetHomogeneous(true)
	for i, c := range games.FortuneCities {
		card := gtk.NewBox(gtk.OrientationVerticalValue, 4)
		cv := newCanvas(cityCardW, cityCardH, func(pn pen) { p.paintCard(pn, i) })
		cv.area.SetSizeRequest(cityCardW, cityCardH)
		cv.area.SetFocusable(false)
		p.cities = append(p.cities, cv)
		gtkx.Append(card, &cv.area.Widget)
		// Its buildings: houses, a hotel, a tree.
		pieces := newCanvas(cityCardW, cityPiecesH, func(pn pen) { paintCityPieces(pn, i) })
		pieces.area.SetSizeRequest(cityCardW, cityPiecesH)
		pieces.area.SetFocusable(false)
		p.cities = append(p.cities, pieces)
		gtkx.Append(card, &pieces.area.Widget)
		name := gtk.NewLabel(i18n.T(c.Name))
		if c.Local != c.Name {
			name.SetText(c.Local + "  " + i18n.T(c.Name))
		}
		name.AddCssClass("fortune-city-name")
		name.SetXalign(0)
		gtkx.Append(card, &name.Widget)
		art := cityArts[i]
		sub := gtk.NewLabel(i18n.Tf("%s and %s", i18n.T(art.houseWord), i18n.T(art.hotelWord)))
		sub.AddCssClass("fortune-city-sub")
		sub.SetXalign(0)
		gtkx.Append(card, &sub.Widget)
		b := pick("city", i, i == p.settings.City, &card.Widget)
		b.AddCssClass("fortune-city")
		gtkx.Append(cities, &b.Widget)
	}
	gtkx.Append(page, &cities.Widget)

	opts := gtk.NewGrid()
	opts.AddCssClass("fortune-options")
	opts.SetRowSpacing(10)
	opts.SetColumnSpacing(18)
	row := func(n int, label string, kind string, names []string, on int) {
		l := gtk.NewLabel(label)
		l.AddCssClass("fortune-option")
		l.SetXalign(0)
		opts.Attach(&l.Widget, 0, int32(n), 1, 1)
		seg := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		seg.AddCssClass("segmented")
		seg.AddCssClass("linked")
		for i, name := range names {
			lb := gtk.NewLabel(name)
			gtkx.Append(seg, &pick(kind, i, i == on, &lb.Widget).Widget)
		}
		opts.Attach(&seg.Widget, 1, int32(n), 1, 1)
	}
	var rounds, cash []string
	for _, r := range games.FortuneRounds {
		rounds = append(rounds, fmt.Sprint(r))
	}
	for _, c := range games.FortuneCash {
		cash = append(cash, "$"+thousands(c))
	}
	row(0, i18n.T("Rounds"), "rounds", rounds, slices.Index(games.FortuneRounds, p.settings.Rounds))
	row(1, i18n.T("Starting money"), "cash", cash, slices.Index(games.FortuneCash, p.settings.Cash))
	row(2, i18n.T("Rivals"), "rivals", []string{i18n.T("Mika"), i18n.T("Mika, Ren"), i18n.T("Mika, Ren, Sol")}, p.settings.Rivals-1)
	row(3, i18n.T("Dice"), "dice", []string{i18n.T("One die"), i18n.T("Two dice")}, max(1, p.settings.Dice)-1+boolInt(p.settings.Dice == 0))
	l := gtk.NewLabel(i18n.T("Build the day you buy"))
	l.AddCssClass("fortune-option")
	l.SetXalign(0)
	opts.Attach(&l.Widget, 0, 4, 1, 1)
	build := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	sw := gtk.NewSwitch()
	sw.SetActive(p.settings.BuildOnBuy)
	sw.SetValign(gtk.AlignCenterValue)
	p.onBuildRule = func(_ gtk.Switch, on bool) bool {
		p.settings.BuildOnBuy = on
		return false
	}
	sw.ConnectStateSet(&p.onBuildRule)
	gtkx.Append(build, &sw.Widget)
	note := gtk.NewLabel(i18n.T("A street you buy may go up a level that same turn"))
	note.AddCssClass("fortune-city-sub")
	gtkx.Append(build, &note.Widget)
	opts.Attach(&build.Widget, 1, 4, 1, 1)
	gtkx.Append(page, &opts.Widget)

	p.onStart = func(gtk.Button) { p.begin() }
	p.start = button(i18n.Tf("Start in %s", i18n.T(games.FortuneCities[p.settings.City].Name)), &p.onStart)
	p.start.RemoveCssClass("outlined")
	p.start.AddCssClass("primary")
	p.start.AddCssClass("fortune-start")
	p.start.SetHalign(gtk.AlignStartValue)
	gtkx.Append(page, &p.start.Widget)
	return page
}

// reset goes back to the setup, for a new game.
func (p *fortune) reset() {
	p.pages.SetVisibleChildName("setup")
	p.round.SetText("—")
	p.pot.SetText("—")
	p.start.GrabFocus()
}

// begin starts a game as set up.
func (p *fortune) begin() {
	p.g = games.NewFortune(p.settings, uint64(time.Now().UnixNano()))
	p.queue, p.cur, p.t, p.wait, p.fx, p.idle = nil, nil, 0, 0, nil, true
	p.cam.zoom, p.cam.view = 1, boardMiddle
	p.shown = p.g.View()
	p.dice = [2]int{}
	p.log = nil
	p.selected = -1
	gtkx.Clear(&p.players)
	p.rows, p.names, p.cashes = nil, nil, nil
	for range p.g.Players {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("fortune-player")
		name := gtk.NewLabel("")
		name.SetHexpand(true)
		name.SetXalign(0)
		gtkx.Append(row, &name.Widget)
		cash := gtk.NewLabel("")
		cash.AddCssClass("fortune-cash")
		cash.SetValign(gtk.AlignStartValue)
		gtkx.Append(row, &cash.Widget)
		gtkx.Append(&p.players, &row.Widget)
		p.rows, p.names, p.cashes = append(p.rows, row), append(p.names, name), append(p.cashes, cash)
	}
	p.pages.SetVisibleChildName("play")
	p.refresh()
	p.cv.redraw()
	p.cv.area.GrabFocus()
}

func (p *fortune) redraw() {
	for _, c := range p.cities {
		c.redraw()
	}
	if p.g != nil {
		p.refresh()
		p.cv.redraw()
	}
}

// busy: the board is still showing what happened, or a computer player
// is about to play.
func (p *fortune) busy() bool {
	return p.cur != nil || len(p.queue) > 0 || p.g.Phase == games.PhaseCPU
}

// focus is the square the panel shows: the one picked, else the one
// the player stands on.
func (p *fortune) focus() int {
	if p.selected >= 0 {
		return p.selected
	}
	return p.shown.At[0]
}

// act does a move of the player's and plays out what follows.
func (p *fortune) act(do func() bool) {
	if p.busy() || !do() {
		return
	}
	p.queue = append(p.queue, p.g.Take()...)
	p.idle = false
	p.play()
}

func (p *fortune) main() {
	switch p.g.Phase {
	case games.PhaseRoll:
		p.selected = -1
		p.act(p.g.Roll)
	case games.PhaseBuy:
		p.act(p.g.Buy)
	case games.PhaseEnd:
		p.act(p.g.EndTurn)
	case games.PhaseOver:
		p.reset()
	}
}

func (p *fortune) other() {
	switch p.g.Phase {
	case games.PhaseRoll:
		p.act(p.g.PayBail)
	case games.PhaseBuy:
		p.act(p.g.Pass)
	}
}

func (p *fortune) key(k uint32) bool {
	if p.g == nil || p.pages.GetVisibleChildName() == "setup" {
		if k == ' ' || k == keyEnter {
			p.begin()
			return true
		}
		return false
	}
	if p.zoomKey(k) {
		return true
	}
	switch k {
	case ' ', keyEnter:
		p.main()
	case 'r':
		if p.g.Phase == games.PhaseRoll {
			p.main()
		}
	case 'b':
		if p.g.Phase == games.PhaseBuy {
			p.main()
		}
	case 'e':
		if p.g.Phase == games.PhaseEnd {
			p.main()
		}
	case 'x', 'j':
		p.other()
	case 'h':
		p.act(func() bool { return p.g.Build(p.focus()) })
	default:
		return false
	}
	return true
}

// How long each kind of event shows, in seconds.
const (
	hopTime  = 0.14
	jumpTime = 0.45
	diceTime = 0.55
)

func (p *fortune) play() {
	p.refresh()
	p.cv.animate(p.step)
}

func (p *fortune) step(dt float64) bool {
	if p.stepFX(dt) {
		p.refresh() // a player's cash stops flashing
	}
	if p.cur == nil {
		switch {
		case len(p.queue) > 0:
			p.cur, p.queue, p.t, p.idle = &p.queue[0], p.queue[1:], 0, false
			if p.cur.Kind == games.EvNote {
				p.log = append([]logLine{{p.cur.Text, p.cur.Good, p.cur.Bad}}, p.log...)
				if len(p.log) > 7 {
					p.log = p.log[:7]
				}
				p.refresh()
			}
		case p.g.Phase == games.PhaseCPU:
			if p.wait += dt; p.wait > 0.5 {
				p.wait = 0
				p.g.PlayCPU()
				p.queue = append(p.queue, p.g.Take()...)
			}
			return true
		default:
			if !p.idle {
				old := p.shown
				p.shown, p.idle = p.g.View(), true
				p.moneyFrom(old, p.shown)
				p.refresh()
			}
			return len(p.fx) > 0 // until the money has flown
		}
	}
	p.t += dt
	e := p.cur
	var length float64
	switch e.Kind {
	case games.EvDice:
		length = diceTime
		if p.t < diceTime-0.1 {
			p.dice = [2]int{rand.IntN(6) + 1, rand.IntN(6) + 1}
			if p.g.OneDie {
				p.dice[1] = 0
			}
		} else {
			p.dice = e.Dice
		}
	case games.EvWalk:
		length = hopTime*float64(e.Steps) + 0.08
		at, _ := p.token(e.Player)
		p.cam.follow(project(at.X, at.Y, 0), dt)
	case games.EvJump:
		length = jumpTime
		at, _ := p.token(e.Player)
		p.cam.follow(project(at.X, at.Y, 0), dt)
	case games.EvNote:
		length = 0.3
		if e.Player != 0 || p.g.Players[p.g.Turn].CPU {
			length = 0.75
		}
	}
	if p.t >= length {
		old := p.shown
		p.shown = e.View
		p.cur = nil
		p.moneyFrom(old, p.shown)
		p.refresh()
	}
	return true
}

// raisable counts the streets the player may raise a level now.
func (p *fortune) raisable() int {
	n := 0
	if p.g.Turn != 0 || p.busy() {
		return 0
	}
	for sq := range games.Board {
		if p.g.CanBuild(sq) {
			n++
		}
	}
	return n
}

// refresh sets the header and the panel from the board as shown.
func (p *fortune) refresh() {
	g, v := p.g, p.shown
	p.round.SetText(fmt.Sprintf("%d / %d", g.Round, g.Rounds))
	p.pot.SetText("$" + thousands(v.Pot))

	// Players.
	for i, pl := range g.Players {
		col := p.playerColor(i)
		status := i18n.Tf("worth $%s", thousands(v.Worth(i)))
		switch {
		case pl.Out:
			status = i18n.T("bankrupt")
		case pl.InJail:
			status += i18n.T(" · in jail")
		}
		mark := ""
		if i == g.Turn && g.Phase != games.PhaseOver {
			mark = "  ◂"
		}
		p.names[i].SetMarkup(fmt.Sprintf(`<span foreground="#%02x%02x%02x">●</span>  %s%s
<span size="smaller" foreground="#%02x%02x%02x">%s</span>`, col.R, col.G, col.B, html.EscapeString(pl.Name), mark,
			p.f.col.t2.R, p.f.col.t2.G, p.f.col.t2.B, status))
		p.cashes[i].SetText("$" + thousands(v.Cash[i]))
		for class, on := range map[string]bool{"gain": p.moneyTone(i) > 0, "loss": p.moneyTone(i) < 0} {
			if on {
				p.cashes[i].AddCssClass(class)
			} else {
				p.cashes[i].RemoveCssClass(class)
			}
		}
		if pl.Out {
			p.rows[i].AddCssClass("out")
		} else {
			p.rows[i].RemoveCssClass("out")
		}
	}

	// The square in focus.
	sq := p.focus()
	s := games.Board[sq]
	p.cardTitle.SetText(g.SquareName(sq))
	var b strings.Builder
	switch s.Kind {
	case games.Street:
		var group []string
		for _, o := range games.Group(s.Group) {
			if o != sq {
				group = append(group, g.SquareName(o))
			}
		}
		fmt.Fprintf(&b, i18n.T("Price $%d · each building $%d\nGroup: %s\n"), s.Price, games.HouseCost(sq), strings.Join(group, ", "))
		fmt.Fprintf(&b, i18n.T("Rent $%d (whole group $%d)\n1–4 houses: $%d, $%d, $%d, $%d\nHotel: $%d (+50%% with the group)"),
			games.RentAt(sq, 0, false), games.RentAt(sq, 0, true), games.RentAt(sq, 1, false), games.RentAt(sq, 2, false),
			games.RentAt(sq, 3, false), games.RentAt(sq, 4, false), games.RentAt(sq, games.Hotel, false))
		if o := v.Owner[sq]; o >= 0 {
			fmt.Fprintf(&b, i18n.T("\nOwned by %s"), g.Players[o].Name)
			switch h := v.Houses[sq]; {
			case h == games.Hotel:
				b.WriteString(i18n.T(", with a hotel"))
			case h > 0:
				b.WriteString(i18n.Nf(", %d house", ", %d houses", h, h))
			}
			fmt.Fprintf(&b, i18n.T(": rent now $%d"), rentOf(v, sq))
		} else {
			b.WriteString(i18n.T("\nFor sale"))
		}
	case games.Pier:
		fmt.Fprintf(&b, i18n.T("Price $%d. Rent $%d, $%d, $%d or $%d as its owner has 1 to 4 piers."), s.Price,
			games.PierRent(1), games.PierRent(2), games.PierRent(3), games.PierRent(4))
		if o := v.Owner[sq]; o >= 0 {
			fmt.Fprintf(&b, i18n.T("\nOwned by %s: rent now $%d"), g.Players[o].Name, rentOf(v, sq))
		} else {
			b.WriteString(i18n.T("\nFor sale"))
		}
	case games.Utility:
		fmt.Fprintf(&b, i18n.T("Price $%d. Rent is the dice times %d, or times %d with both utilities."), s.Price,
			games.UtilityTimes(1), games.UtilityTimes(2))
		if o := v.Owner[sq]; o >= 0 {
			fmt.Fprintf(&b, i18n.T("\nOwned by %s"), g.Players[o].Name)
		} else {
			b.WriteString(i18n.T("\nFor sale"))
		}
	case games.StartSquare:
		b.WriteString(i18n.T("Collect $200 every time you pass."))
	case games.JailSquare:
		fmt.Fprintf(&b, i18n.T("Only visiting, unless sent here. In jail, roll %s to leave, or pay $50 before rolling; after three tries you pay and go."),
			map[bool]string{false: i18n.T("a double"), true: i18n.T("a six")}[g.OneDie])
	case games.Fountain:
		fmt.Fprintf(&b, i18n.T("Taxes and fines go into the fountain; the next player to land here takes it all: $%d now."), v.Pot)
	case games.GoToJail:
		b.WriteString(i18n.T("Straight to jail, without passing Start."))
	case games.ChanceSquare:
		b.WriteString(i18n.T("Draw a card: money, a fine, or a trip."))
	case games.TaxSquare:
		fmt.Fprintf(&b, i18n.T("Pay $%d into the fountain."), s.Price)
	}
	p.cardBody.SetText(b.String())
	mine := s.Kind == games.Street && v.Owner[sq] == 0
	p.cardButtons.SetVisible(mine)
	idle := !p.busy() && g.Turn == 0
	p.build.SetSensitive(idle && g.CanBuild(sq))
	p.sell.SetSensitive(idle && g.CanSell(sq))
	switch h := v.Houses[sq]; {
	case h == games.Hotel:
		p.build.SetLabel(i18n.T("Hotel built"))
	case h == games.Hotel-1:
		p.build.SetLabel(i18n.Tf("Build a hotel ($%d)", games.HouseCost(sq)))
	default:
		p.build.SetLabel(i18n.Tf("Build a house ($%d)", games.HouseCost(sq)))
	}

	// What can be done.
	p.secondary.SetVisible(false)
	switch {
	case g.Phase == games.PhaseOver && !p.busy():
		p.primary.SetLabel(i18n.T("New game"))
		p.primary.SetSensitive(true)
	case p.busy() || g.Players[g.Turn].CPU:
		p.primary.SetLabel(i18n.Tf("%s is playing…", g.Players[g.Turn].Name))
		if g.Turn == 0 {
			p.primary.SetLabel("…")
		}
		p.primary.SetSensitive(false)
	case g.Phase == games.PhaseRoll:
		p.primary.SetLabel(i18n.T("Roll the dice"))
		p.primary.SetSensitive(true)
		if g.Players[0].InJail {
			p.primary.SetLabel(map[bool]string{false: i18n.T("Roll for a double"), true: i18n.T("Roll for a six")}[g.OneDie])
			p.secondary.SetLabel(i18n.T("Pay $50 bail"))
			p.secondary.SetVisible(true)
			p.secondary.SetSensitive(g.Players[0].Cash >= 50)
		}
	case g.Phase == games.PhaseBuy:
		at := g.Players[0].At
		p.primary.SetLabel(i18n.Tf("Buy it for $%d", games.Board[at].Price)) // short: the panel keeps its width
		p.primary.SetSensitive(true)
		p.secondary.SetLabel(i18n.T("Pass"))
		p.secondary.SetVisible(true)
		p.secondary.SetSensitive(true)
	case g.Phase == games.PhaseEnd:
		p.primary.SetLabel(i18n.T("End turn"))
		p.primary.SetSensitive(true)
	}

	// What happened.
	gtkx.Clear(&p.logBox)
	for n, l := range p.log {
		lb := gtk.NewLabel(l.text)
		lb.AddCssClass("fortune-log")
		if n == 0 {
			lb.AddCssClass("latest")
		}
		if l.good {
			lb.AddCssClass("good")
		}
		if l.bad {
			lb.AddCssClass("bad")
		}
		lb.SetXalign(0)
		lb.SetWrap(true)
		lb.SetMaxWidthChars(36)
		gtkx.Append(&p.logBox, &lb.Widget)
	}

	p.cv.redraw()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
