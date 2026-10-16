package gamesui

import (
	"fmt"
	"html"
	"image/color"
	"math"
	"math/bits"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/makiuchi-d/gozxing"

	"github.com/lyricat/inua/internal/assistant/answer"
	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/games/fortuneai"
	"github.com/lyricat/inua/internal/games/fortunenet"
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
	people   int        // how many play at this screen, taking turns
	pages    *gtk.Stack // "setup", "lobby" for a game on the network, then "play"

	// A game on the local network (fortune_host.go): hostOn as set up; the
	// game's host from the lobby on; the lobby's link, QR code, players
	// and buttons; the code in the header while the game is on.
	hostOn                              bool
	net                                 *fortunenet.Host
	lobbyURL, lobbyMore                 *gtk.Label
	lobbyWait, lobbyNote, code          *gtk.Label
	lobbyList                           gtk.Box
	lobbyStart                          *gtk.Button
	qrCanvas                            *canvas
	qr                                  *gozxing.BitMatrix
	onKick, onLobbyStart, onLobbyCancel func(gtk.Button)
	onHostRule                          func(gtk.Switch, bool) bool

	// The people who play at this screen, a bit a player (games.Seat), and
	// the one of them the board is shown for: whose turn it is, or was last
	// while others play; -1 for none.
	here uint8
	me   int

	// The board shows events one after another: queue waits, cur plays.
	queue   []games.FortuneEvent
	cur     *games.FortuneEvent
	t, wait float64
	shown   games.FortuneView
	dice    [2]int
	diceAt  games.Vec // where the dice lie, on the board
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
	underScale  float64
	canvasScale float64

	fx   []moneyFX // money won and lost, flying off the pawns
	idle bool      // the board shows the game as it is

	// Its sounds (fortune_sound.go), muted with M; the turn last rung in
	// and whether the end has played, so each sounds once.
	snd      *synth
	muted    bool
	rungTurn string
	endRung  bool

	round, pot                      *gtk.Label
	players, logBox, cardButtons    gtk.Box
	rows                            []*gtk.Box
	names, cashes                   []*gtk.Label
	primary, secondary, build, sell *gtk.Button

	// The square in focus (fortune_panel.go): its sign, name, kind, owner,
	// rents, laid out again for squareFor; the build button's icon, labels.
	sign                               *canvas
	cardTitle, squareKind, squareOwner *gtk.Label
	rents                              gtk.Box
	squareFor                          string
	buildIcon                          *gtk.Image
	buildLabel, sellLabel              *gtk.Label
	// The cards over the board: the hand along its foot, under a caption;
	// the shop over its middle, with its offer. The hand as shown
	// (handShown), laid out again only when handFor changes; when each card
	// came into it, and the canvases they are drawn on.
	hand, offer   gtk.Box
	handBar, shop *gtk.Box
	handCaption   *gtk.Label
	onLeaveShop   func(gtk.Button)
	handFor       string
	handOf        int // the player whose hand it is
	handShown     []games.CardKind
	handBorn      map[int]time.Time
	handCards     []*canvas

	// The setup: who plays (roster), the map chosen large (previewCv,
	// drawn once into preview for previewCity at previewAt), its name and
	// buildings, a card for each city, the choices, Start.
	seats        [4]int // each seat's choice: seatHot, a computer player, seatAI, seatOpen
	seatDrops    [4]*gtk.DropDown
	seatNames    [4]*gtk.Label
	seatRows     [4]*gtk.Box
	seatSubs     [4]*gtk.Label
	fillingSeats bool
	open         int // the seats open, on the network
	onSeat       func(uintptr, uintptr, uintptr)

	// The user's AI (fortune_ai.go): whether its program started, why not,
	// and once it did, the program and the driver of its players; the
	// profile each seat asks, picked in profileDrops, and whether each
	// profile asked answers.
	aiState      aiState
	aiWhy        string
	aiClient     *answer.Client
	ai           *fortuneai.Driver
	seatProfile  [4]string
	profileDrops [4]*gtk.DropDown
	checks       map[string]*aiCheck
	aiProfiles   []string // the AI players', in their order
	onProfile    func(uintptr, uintptr, uintptr)

	// The game menu (fortune_menu.go), in the header: its Restart item, and
	// the page the compendium goes back to.
	menu                *gtk.MenuButton
	restartItem         *gtk.Button
	backTo              string
	onMenu, onBack      func(gtk.Button)
	onMenuOpen          func(uintptr, uintptr, uintptr)
	previewCv, piecesCv *canvas
	preview             *image
	previewCity         int
	previewAt           float64
	mapName, mapSub     *gtk.Label
	cities              []*canvas
	cards               map[int]*image // each city's card, drawn once at the screen's scale
	cardAt              float64
	picks               map[string][]*gtk.ToggleButton
	start               *gtk.Button
	onPick              func(gtk.ToggleButton)
	onStart             func(gtk.Button)
	onBuildRule         func(gtk.Switch, bool) bool

	onPrimary, onSecondary, onBuild, onSell func(gtk.Button)
	onCard                                  func(gtk.Button) // "buy:offer" or "play:card:target"
	onClick                                 func(gtk.GestureClick, int32, float64, float64)
	onMotion                                func(gtk.EventControllerMotion, float64, float64)
	onLeave                                 func(gtk.EventControllerMotion)
}

type logLine struct {
	text      string
	good, bad bool
}

// The streets' colour groups, in the old pigments of washi prints, each
// unlike its neighbours: round the ring kogecha, wakatake, hanada, enji,
// sakura, kon, yamabuki, murasaki, matcha, sangoshu, seiji, nezumi, fuji,
// beni, ruri, shu; on the branch roads asagi, moegi, botan, aonibi,
// uguisu, suou, kaki, kikyo.
var groupColors = [24]color.NRGBA{
	{0x8A, 0x5A, 0x3A, 255}, {0x6A, 0x9E, 0x73, 255}, {0x6C, 0x8C, 0xAB, 255}, {0x9E, 0x3D, 0x3F, 255},
	{0xC2, 0x7D, 0x8B, 255}, {0x2E, 0x3A, 0x5C, 255}, {0xD4, 0x9B, 0x39, 255}, {0x7A, 0x4A, 0x7E, 255},
	{0x7A, 0x97, 0x58, 255}, {0xD4, 0x83, 0x6E, 255}, {0x2F, 0x6C, 0x68, 255}, {0x6E, 0x6A, 0x72, 255},
	{0x5C, 0x58, 0x8C, 255}, {0xC0, 0x48, 0x5A, 255}, {0x2D, 0x5D, 0x9F, 255}, {0xBF, 0x44, 0x30, 255},
	{0x4A, 0x9B, 0xA8, 255}, {0x8B, 0xA8, 0x45, 255}, {0xB5, 0x51, 0x7E, 255}, {0x4E, 0x5D, 0x6C, 255},
	{0x7D, 0x7A, 0x3A, 255}, {0x8E, 0x35, 0x4A, 255}, {0xD3, 0x79, 0x3D, 255}, {0x6A, 0x4C, 0x9C, 255},
}

// playerColor is a player's colour, the board's own: vermilion, indigo,
// ochre, wisteria.
func (p *fortune) playerColor(i int) color.NRGBA {
	return [...]color.NRGBA{{0xC2, 0x3B, 0x22, 255}, {0x2B, 0x4C, 0x7E, 255}, {0xC8, 0x8A, 0x1E, 255}, {0x7B, 0x5E, 0xA7, 255}}[i]
}

// theme: Small Fortune is a washi board whatever the shell's theme, its
// window the paper's too.
func (p *fortune) theme() string { return "washi" }

func (p *fortune) setup(f *frame) {
	p.f, p.settings, p.people, p.selected, p.hover = f, games.DefaultFortune, 1, -1, -1
	p.seats = [4]int{seatHot, 1, 2, seatOpen} // you, Mika and Ren
	p.snd = newSynth()
	f.win.SetDefaultSize(1440, 900)
	f.status.SetVisible(false) // the board and the panel say it all
	f.hint.SetVisible(false)
	p.round = f.stat(i18n.T("ROUND"))
	p.pot = f.stat(i18n.T("FOUNTAIN"))
	p.code = f.stat(i18n.T("JOIN CODE"))
	p.code.GetParent().SetVisible(false)

	p.pages = gtk.NewStack()
	p.pages.SetTransitionType(gtk.StackTransitionTypeCrossfadeValue)
	p.pages.SetTransitionDuration(220)
	p.pages.SetVexpand(true)
	p.pages.AddNamed(&p.setupPage().Widget, "setup")
	p.pages.AddNamed(&p.setupLobby().Widget, "lobby")
	p.pages.AddNamed(&p.setupCompendium().Widget, "compendium")
	p.setupMenu(f)

	row := gtk.NewBox(gtk.OrientationHorizontalValue, 18)
	p.cv = newCanvas(1000, 760, p.paint)
	p.cv.fill = true
	board := gtk.NewOverlay()
	board.SetChild(p.setupCamera())
	p.setupHand(board)
	gtkx.Append(row, &board.Widget)
	// The panel: players, the square in focus with what can be done, what
	// happened; the square takes what the players and the last few lines
	// leave.
	panel := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	panel.AddCssClass("fortune-panel")
	panel.SetSizeRequest(320, -1)
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
	players := section(i18n.T("PLAYERS"))
	p.players = *gtk.NewBox(gtk.OrientationVerticalValue, 0)
	gtkx.Append(players, &p.players.Widget)

	square := section(i18n.T("SQUARE"))
	square.SetVexpand(true)
	p.setupSquare(square)
	acts := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	acts.AddCssClass("fortune-acts")
	p.onPrimary = func(gtk.Button) { p.main() }
	p.onSecondary = func(gtk.Button) { p.other() }
	p.primary = button(i18n.T("Roll the dice"), &p.onPrimary)
	p.primary.RemoveCssClass("outlined")
	p.primary.AddCssClass("primary")
	p.primary.AddCssClass("fortune-main")
	p.primary.SetHexpand(true)
	p.secondary = button("", &p.onSecondary) // the quieter choice beside it: don't buy, pay bail
	p.secondary.RemoveCssClass("outlined")
	p.secondary.AddCssClass("fortune-main")
	p.secondary.AddCssClass("fortune-quiet")
	gtkx.Append(acts, &p.primary.Widget)
	gtkx.Append(acts, &p.secondary.Widget)
	gtkx.Append(square, &acts.Widget)

	happened := section(i18n.T("WHAT HAPPENED"))
	p.logBox = *gtk.NewBox(gtk.OrientationVerticalValue, 4)
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	sw.SetSizeRequest(-1, 120)
	sw.SetChild(&p.logBox.Widget)
	p.logBox.Unref()
	gtkx.Append(happened, &sw.Widget)

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
		if sq := squareAtScreen(p.city(), p.cam.seen(p.cv.at(x, y))); sq != p.hover {
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

// setupPage is the page before a game, in two columns, as a strategy
// game's new game is: on the left the players, who sits where (people at
// this screen, computer rivals, seats open to the network), with the
// choices that make them; on the right the map, the city chosen large
// with its buildings, the four to pick from, and the game's rules; Start
// at the foot.
func (p *fortune) setupPage() *gtk.Box {
	page := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	page.AddCssClass("fortune-setup")
	page.SetHalign(gtk.AlignCenterValue)
	page.SetValign(gtk.AlignCenterValue)
	head := gtk.NewLabel(i18n.T("New game"))
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
			p.showMap()
		case "rounds":
			p.settings.Rounds = games.FortuneRounds[i]
		case "cash":
			p.settings.Cash = games.FortuneCash[i]
		case "dice":
			p.settings.Dice = i + 1
		}
		p.start.SetLabel(p.startLabel())
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
	column := func(kicker string) *gtk.Box {
		col := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		col.AddCssClass("fortune-setup-col")
		k := gtk.NewLabel(kicker)
		k.AddCssClass("kicker")
		k.AddCssClass("fortune-kicker")
		k.SetXalign(0)
		gtkx.Append(col, &k.Widget)
		return col
	}
	options := func(col *gtk.Box) func(label string, control *gtk.Widget) {
		grid := gtk.NewGrid()
		grid.AddCssClass("fortune-options")
		grid.SetRowSpacing(10)
		grid.SetColumnSpacing(18)
		gtkx.Append(col, &grid.Widget)
		n := int32(0)
		return func(label string, control *gtk.Widget) {
			l := gtk.NewLabel(label)
			l.AddCssClass("fortune-option")
			l.SetXalign(0)
			grid.Attach(&l.Widget, 0, n, 1, 1)
			grid.Attach(control, 1, n, 1, 1)
			n++
		}
	}
	segments := func(kind string, names []string, on int) *gtk.Widget {
		seg := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		seg.AddCssClass("segmented")
		seg.AddCssClass("linked")
		for i, name := range names {
			lb := gtk.NewLabel(name)
			gtkx.Append(seg, &pick(kind, i, i == on, &lb.Widget).Widget)
		}
		return &seg.Widget
	}
	toggle := func(on bool, handler *func(gtk.Switch, bool) bool) *gtk.Widget {
		sw := gtk.NewSwitch()
		sw.SetActive(on)
		sw.SetHalign(gtk.AlignStartValue)
		sw.SetValign(gtk.AlignCenterValue)
		sw.ConnectStateSet(handler)
		return &sw.Widget
	}

	cols := gtk.NewBox(gtk.OrientationHorizontalValue, 24)

	// The players: a seat to a row, each a person at this screen, a
	// computer player, or open; then the network.
	left := column(i18n.T("PLAYERS"))
	left.SetSizeRequest(400, -1)
	roster := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	roster.AddCssClass("fortune-roster")
	choices := []string{i18n.T("Hot seat")}
	for _, r := range games.Rivals {
		choices = append(choices, i18n.T(r.Name))
	}
	choices = append(choices, i18n.T("AI"), i18n.T("Open seat"))
	p.onSeat = func(ptr, _, _ uintptr) {
		for i, d := range p.seatDrops {
			if d != nil && d.GoPointer() == ptr && !p.fillingSeats {
				p.setSeat(i, int(d.GetSelected()))
			}
		}
	}
	for i := range p.seats {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 14)
		row.AddCssClass("fortune-player")
		d := gtk.NewDropDownFromStrings(choices)
		d.AddCssClass("fortune-seat")
		d.SetSelected(uint32(p.seats[i]))
		d.SetValign(gtk.AlignCenterValue)
		gtkx.ConnectNotify(d, "selected", &p.onSeat)
		p.seatDrops[i] = d
		// The seat's choice, and under it, for the AI, its profile.
		drops := gtk.NewBox(gtk.OrientationVerticalValue, 6)
		drops.SetValign(gtk.AlignCenterValue)
		gtkx.Append(drops, &d.Widget)
		p.profileDrops[i] = p.profileDrop()
		gtkx.Append(drops, &p.profileDrops[i].Widget)
		gtkx.Append(row, &drops.Widget)
		who := gtk.NewBox(gtk.OrientationVerticalValue, 1)
		who.SetHexpand(true)
		who.SetValign(gtk.AlignCenterValue)
		p.seatNames[i] = gtk.NewLabel("")
		p.seatNames[i].SetXalign(0)
		gtkx.Append(who, &p.seatNames[i].Widget)
		p.seatSubs[i] = gtk.NewLabel("")
		p.seatSubs[i].AddCssClass("fortune-city-sub")
		p.seatSubs[i].SetXalign(0)
		p.seatSubs[i].SetEllipsize(3) // end
		p.seatSubs[i].SetWrap(true)   // why the AI cannot play: two lines
		p.seatSubs[i].SetLines(2)
		p.seatSubs[i].SetMaxWidthChars(28)
		gtkx.Append(who, &p.seatSubs[i].Widget)
		gtkx.Append(row, &who.Widget)
		p.seatRows[i] = row
		gtkx.Append(roster, &row.Widget)
	}
	gtkx.Append(left, &roster.Widget)
	spacer := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	spacer.SetVexpand(true)
	gtkx.Append(left, &spacer.Widget)
	players := options(left)
	p.onHostRule = func(_ gtk.Switch, on bool) bool {
		p.hostOn = on
		p.fitSeats()
		return false
	}
	players(i18n.T("On the network"), toggle(p.hostOn, &p.onHostRule))
	gtkx.Append(cols, &left.Widget)

	// The map: the city chosen, large, with its buildings; the four; the
	// rules.
	right := column(i18n.T("MAP"))
	p.previewCv = newCanvas(cityCardW*previewK, cityCardH*previewK, p.paintPreview)
	p.previewCv.area.SetSizeRequest(int32(cityCardW*previewK), int32(cityCardH*previewK))
	p.previewCv.area.SetFocusable(false)
	p.cities = append(p.cities, p.previewCv)
	gtkx.Append(right, &p.previewCv.area.Widget)
	info := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	info.AddCssClass("fortune-map-info")
	names := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	names.SetHexpand(true)
	names.SetValign(gtk.AlignCenterValue)
	p.mapName = gtk.NewLabel("")
	p.mapName.AddCssClass("fortune-city-name")
	p.mapName.SetXalign(0)
	gtkx.Append(names, &p.mapName.Widget)
	p.mapSub = gtk.NewLabel("")
	p.mapSub.AddCssClass("fortune-city-sub")
	p.mapSub.SetXalign(0)
	gtkx.Append(names, &p.mapSub.Widget)
	gtkx.Append(info, &names.Widget)
	p.piecesCv = newCanvas(cityCardW, cityPiecesH, func(pn pen) { paintCityPieces(pn, p.settings.City) })
	p.piecesCv.area.SetSizeRequest(int32(cityCardW), int32(cityPiecesH))
	p.piecesCv.area.SetFocusable(false)
	p.cities = append(p.cities, p.piecesCv)
	gtkx.Append(info, &p.piecesCv.area.Widget)
	gtkx.Append(right, &info.Widget)

	cities := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	cities.SetHomogeneous(true)
	for i, c := range games.FortuneCities {
		card := gtk.NewBox(gtk.OrientationVerticalValue, 4)
		cv := newCanvas(cityCardW/2, cityCardH/2, func(pn pen) {
			pn.Scale(0.5, 0.5)
			p.paintCard(pn, i)
		})
		cv.area.SetSizeRequest(int32(cityCardW/2), int32(cityCardH/2))
		cv.area.SetFocusable(false)
		p.cities = append(p.cities, cv)
		gtkx.Append(card, &cv.area.Widget)
		name := gtk.NewLabel(i18n.T(c.Name))
		name.AddCssClass("fortune-city-pick")
		gtkx.Append(card, &name.Widget)
		b := pick("city", i, i == p.settings.City, &card.Widget)
		b.AddCssClass("fortune-city")
		gtkx.Append(cities, &b.Widget)
	}
	gtkx.Append(right, &cities.Widget)

	var rounds, cash []string
	for _, r := range games.FortuneRounds {
		rounds = append(rounds, fmt.Sprint(r))
	}
	for _, c := range games.FortuneCash {
		cash = append(cash, "$"+thousands(c))
	}
	rules := options(right)
	rules(i18n.T("Rounds"), segments("rounds", rounds, slices.Index(games.FortuneRounds, p.settings.Rounds)))
	rules(i18n.T("Starting money"), segments("cash", cash, slices.Index(games.FortuneCash, p.settings.Cash)))
	rules(i18n.T("Dice"), segments("dice", []string{i18n.T("One die"), i18n.T("Two dice")}, max(1, p.settings.Dice)-1+boolInt(p.settings.Dice == 0)))
	p.onBuildRule = func(_ gtk.Switch, on bool) bool {
		p.settings.BuildOnBuy = on
		return false
	}
	rules(i18n.T("Build the day you buy"), toggle(p.settings.BuildOnBuy, &p.onBuildRule))
	gtkx.Append(cols, &right.Widget)
	gtkx.Append(page, &cols.Widget)

	p.onStart = func(gtk.Button) { p.begin() }
	p.start = button(p.startLabel(), &p.onStart)
	p.start.RemoveCssClass("outlined")
	p.start.AddCssClass("primary")
	p.start.AddCssClass("fortune-start")
	p.start.SetHalign(gtk.AlignEndValue)
	gtkx.Append(page, &p.start.Widget)
	p.fitSeats()
	p.showMap()
	return page
}

// showMap shows the city chosen in the map column: large, its name, its
// buildings.
func (p *fortune) showMap() {
	c := games.FortuneCities[p.settings.City]
	name := i18n.T(c.Name)
	if c.Local != c.Name {
		name = c.Local + "  " + name
	}
	p.mapName.SetText(name)
	art := cityArts[p.settings.City]
	p.mapSub.SetText(i18n.Tf("%s and %s", i18n.T(art.houseWord), i18n.T(art.hotelWord)))
	p.previewCv.redraw()
	p.piecesCv.redraw()
}

// The seats' choices: a person at this screen (a hot seat), one of the
// computer players (games.Rivals, by 1 + its index), the user's AI, an
// open seat.
const (
	seatHot  = 0
	seatAI   = 4
	seatOpen = 5
)

// setSeat makes seat i choice c; a computer player another seat has
// moves that seat to the next one free, or opens it.
func (p *fortune) setSeat(i, c int) {
	p.seats[i] = c
	if c == seatAI {
		p.checkAI()
	}
	if c > seatHot && c <= len(games.Rivals) {
		for j := range p.seats {
			if j != i && p.seats[j] == c {
				p.seats[j] = seatOpen
				for k := 1; k <= len(games.Rivals); k++ {
					if !slices.Contains(p.seats[:], k) {
						p.seats[j] = k
						break
					}
				}
			}
		}
	}
	p.fitSeats()
}

// fitSeats takes the game's players from the seats: the people, the
// computer players by name, the seats open on the network; names each
// row and dots it in the colour its player will have (people first, then
// visitors, then the computer players); and lets Start start a game that
// can be played.
func (p *fortune) fitSeats() {
	if p.start == nil {
		return
	}
	p.fillingSeats = true
	defer func() { p.fillingSeats = false }()
	p.people, p.open = 0, 0
	var rivals []string
	ais := 0
	for _, c := range p.seats {
		switch c {
		case seatHot:
			p.people++
		case seatOpen:
			p.open++
		case seatAI:
			ais++
		default:
			rivals = append(rivals, games.Rivals[c-1].Name)
		}
	}
	p.settings.Rivals, p.settings.RivalNames = len(rivals), rivals
	p.settings.AIs, p.aiProfiles = p.aiPlayers()
	aiReady := true
	open := 0
	if p.hostOn {
		open = p.open
	}
	hot, visitor, ai, cpu := 0, 0, 0, 0
	for i, c := range p.seats {
		p.seatDrops[i].SetSelected(uint32(c))
		var name, sub, about string
		var player int
		switch c {
		case seatHot:
			player = hot
			hot++
			sub = i18n.T("at this screen")
			switch {
			case p.people > 1:
				name = i18n.Tf("Player %d", hot)
			case p.hostOn:
				name = hostName()
			default:
				name = i18n.T("You")
			}
		case seatOpen:
			player = p.people + visitor
			visitor++
			name = i18n.T("Open seat")
			sub = i18n.T("empty: no one plays here")
			if p.hostOn {
				sub = i18n.T("for a visitor in a browser")
			}
		case seatAI:
			player = p.people + open + ai
			name = p.settings.AIs[ai]
			ai++
			var ready bool
			sub, about, ready = p.aiSeat(i)
			aiReady = aiReady && ready
		default:
			player = p.people + open + ais + cpu
			cpu++
			r := games.Rivals[c-1]
			name, sub, about = i18n.T(r.Name), i18n.T(r.Title), i18n.T(r.About)
		}
		p.fitProfileDrop(i)
		p.seatSubs[i].SetText(sub)
		p.seatRows[i].SetTooltipText(about)
		markup := html.EscapeString(name)
		if c != seatOpen || p.hostOn {
			col := p.playerColor(min(player, 3))
			markup = fmt.Sprintf(`<span foreground="#%02x%02x%02x">●</span>  %s`, col.R, col.G, col.B, markup)
		} else {
			markup = "<span alpha=\"1\">●</span>  " + markup // no one sits there
		}
		p.seatNames[i].SetMarkup(markup)
		if c == seatOpen && !p.hostOn {
			p.seatRows[i].AddCssClass("open")
		} else {
			p.seatRows[i].RemoveCssClass("open")
		}
	}
	can := p.people >= 1 && p.people+ais+len(rivals) >= 2
	if p.hostOn {
		can = p.people+open >= 1 && p.people+ais+len(rivals)+open >= 2
	}
	can = can && aiReady
	p.start.SetSensitive(can)
	p.start.SetLabel(p.startLabel())
}

// previewK is how much larger than its card the map column shows a city.
const previewK = 1.6

// paintPreview draws the city chosen, previewK times a card's size, from
// an image drawn once for it at the screen's scale.
func (p *fortune) paintPreview(pn pen) {
	scale := pn.DeviceScale()
	if p.preview == nil || p.previewCity != p.settings.City || math.Abs(scale-p.previewAt) > 0.01 {
		if p.preview != nil {
			p.preview.Free()
		}
		p.preview = newImage(int(math.Ceil(cityCardW*previewK*scale)), int(math.Ceil(cityCardH*previewK*scale)))
		p.preview.Draw(func(q pen) {
			q.Scale(previewK*scale, previewK*scale)
			q.Round()
			paintCityCard(q, p.settings.City)
		})
		p.previewCity, p.previewAt = p.settings.City, scale
	}
	pn.Blit(p.preview, 1/scale, false)
}

// startLabel is what Start says: where the game is, or that it opens to
// the network first.
func (p *fortune) startLabel() string {
	if p.hostOn {
		return i18n.T("Open the game to the network")
	}
	return i18n.Tf("Start in %s", i18n.T(games.FortuneCities[p.settings.City].Name))
}

// reset goes back to the setup, for a new game; a game on the network
// is over.
func (p *fortune) reset() {
	p.hostClose()
	if p.ai != nil {
		p.ai.Stop()
	}
	p.forgetFailures() // the AI may have been set up since
	if slices.Contains(p.seats[:], seatAI) {
		p.checkAI()
	}
	p.fitSeats()
	p.pages.SetVisibleChildName("setup")
	p.round.SetText("—")
	p.pot.SetText("—")
	p.start.GrabFocus()
}

// begin starts a game as set up, or on the network its lobby.
func (p *fortune) begin() {
	if p.hostOn {
		p.hostLobby()
		return
	}
	s := p.settings
	s.Humans = nil // one person: "You"
	if p.people > 1 {
		s.Humans = make([]string, p.people) // "Player 1" …
	}
	var here uint8
	for i := range p.people {
		here |= games.Seat(i)
	}
	if p.ai != nil {
		p.ai.SetProfiles(p.aiProfiles)
	}
	p.newGame(games.NewFortune(s, uint64(time.Now().UnixNano())), here)
}

// newGame shows game g, played at this screen by the players in here.
func (p *fortune) newGame(g *games.Fortune, here uint8) {
	p.g, p.here, p.me = g, here, -1
	if here != 0 {
		p.me = bits.TrailingZeros8(here) // the first of them
	}
	p.queue, p.cur, p.t, p.wait, p.fx, p.idle = nil, nil, 0, 0, nil, true
	p.shown = p.g.View()
	p.lookAt(max(0, p.me))
	p.dice, p.diceAt = [2]int{}, p.diceBy(0)
	p.squareFor, p.handFor, p.handShown, p.handBorn, p.handOf = "", "", nil, nil, p.me
	p.rungTurn, p.endRung = "1 0", false
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
		name.SetWrap(true) // a long status: "away, the computer plays"
		name.SetMaxWidthChars(28)
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
	p.aiNext()
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
	if p.me < 0 {
		return p.shown.At[p.g.Turn]
	}
	return p.shown.At[p.me]
}

// mine reports whether the player whose turn it is plays at this screen.
func (p *fortune) mine() bool {
	return !p.g.Players[p.g.Turn].Computer() && p.here&games.Seat(p.g.Turn) != 0
}

// alone reports whether one person plays at this screen, "you".
func (p *fortune) alone() bool { return bits.OnesCount8(p.here) == 1 }

// take queues what happened since, as the person shown for sees it.
func (p *fortune) take() {
	for _, e := range p.g.Take() {
		p.queue = append(p.queue, e.For(p.me))
	}
}

// act does a move of the player's and plays out what follows.
func (p *fortune) act(do func() bool) {
	if p.busy() || !p.mine() || !do() {
		return
	}
	if p.net != nil {
		p.net.Flush() // its events come back through netEvents
		return
	}
	p.take()
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
	case games.PhaseShop:
		p.act(p.g.LeaveShop)
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
	if p.pages.GetVisibleChildName() == "lobby" {
		return false
	}
	if p.g == nil || p.pages.GetVisibleChildName() == "setup" {
		if k == ' ' || k == keyEnter {
			p.begin()
			return true
		}
		return false
	}
	if p.panKey(k) {
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
	case 'm':
		p.muted = !p.muted
		p.snd.setMuted(p.muted)
		p.cv.redraw()
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
			p.startSounds(p.cur)
			if p.cur.Kind == games.EvDice {
				p.diceAt = p.diceBy(p.cur.Player)
			}
			if p.cur.Kind == games.EvNote {
				p.log = append([]logLine{{p.cur.Text, p.cur.Good, p.cur.Bad}}, p.log...)
				if len(p.log) > 7 {
					p.log = p.log[:7]
				}
				p.refresh()
			}
		case p.g.Phase == games.PhaseCPU && p.net != nil:
			return true // the host plays, and hands the board what happened
		case p.g.Phase == games.PhaseCPU:
			if p.wait += dt; p.wait > 0.5 {
				p.wait = 0
				p.g.PlayCPU()
				p.take()
			}
			return true
		default:
			if !p.idle {
				if p.mine() && p.me != p.g.Turn { // the next person here takes the screen
					p.me, p.selected = p.g.Turn, -1
					p.lookAt(p.me)
				}
				old := p.shown
				p.shown, p.idle = p.g.View(), true
				p.moneyFrom(old, p.shown)
				p.soundsFrom(old, p.shown)
				p.refresh()
				p.ring()
				p.aiNext()
			}
			return len(p.fx) > 0 // until the money has flown
		}
	}
	p.cueSounds(dt)
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
	case games.EvCard:
		length = cardGotTime
		if e.Played {
			length = cardPlayTime
		}
		at, _ := p.token(e.Player)
		p.cam.follow(project(at.X, at.Y, 0), dt)
	case games.EvNote:
		length = 0.3
		if e.Player != p.me || !p.mine() {
			length = 0.75
		}
	}
	if p.t >= length {
		old := p.shown
		p.shown = e.View
		p.cur = nil
		p.moneyFrom(old, p.shown)
		p.soundsFrom(old, p.shown)
		p.refresh()
	}
	return true
}

// ring sounds what the board, come to rest, now stands at: the game's
// end, won or lost, once; the player's turn come round again.
func (p *fortune) ring() {
	g := p.g
	switch {
	case g.Phase == games.PhaseOver && !p.endRung:
		p.endRung = true
		if p.here == 0 || p.here&games.Seat(g.Winner) != 0 {
			p.sound(sndWin)
		} else {
			p.sound(sndLose)
		}
	case p.mine() && g.Phase == games.PhaseRoll:
		if turn := fmt.Sprint(g.Round, g.Turn); turn != p.rungTurn {
			p.rungTurn = turn
			p.sound(sndYourTurn)
		}
	}
}

// diceBy is where the dice roll for player pi: inside the loop, beside the
// square their pawn stands on.
func (p *fortune) diceBy(pi int) games.Vec {
	t := games.TrackOf(p.city()).Tiles[p.shown.At[pi]]
	return t.At(0, -t.Across/2-40)
}

// raisable counts the streets the player may raise a level now.
func (p *fortune) raisable() int {
	n := 0
	if !p.mine() || p.busy() {
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
		if !pl.Out {
			if n := v.Hands[i]; n > 0 {
				status += i18n.Nf(" · %d card", " · %d cards", n, n)
			}
			if pl.RentFree {
				status += i18n.T(" · free rent")
			}
			if pl.Turtle > 0 {
				status += i18n.T(" · turtle")
			}
			if pl.Asleep {
				status += i18n.T(" · asleep")
			}
			if pl.Away {
				status += i18n.T(" · away, the computer plays")
			}
			if pl.AI {
				status += i18n.T(" · the AI plays")
			}
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

	p.refreshSquare()

	// What can be done.
	p.secondary.SetVisible(false)
	switch {
	case g.Phase == games.PhaseOver && !p.busy():
		p.primary.SetLabel(i18n.T("New game"))
		p.primary.SetSensitive(true)
	case p.busy() || !p.mine():
		p.primary.SetLabel(i18n.Tf("%s is playing…", g.Players[g.Turn].Name))
		if g.Players[g.Turn].AI && !p.busy() {
			p.primary.SetLabel(i18n.Tf("%s is thinking…", g.Players[g.Turn].Name))
		}
		if g.Turn == p.me {
			p.primary.SetLabel("…")
		}
		p.primary.SetSensitive(false)
	case g.Phase == games.PhaseRoll:
		p.primary.SetLabel(i18n.T("Roll the dice"))
		p.primary.SetSensitive(true)
		if g.Players[g.Turn].InJail {
			p.primary.SetLabel(map[bool]string{false: i18n.T("Roll for a double"), true: i18n.T("Roll for a six")}[g.OneDie])
			p.secondary.SetLabel(i18n.T("Pay $50 bail"))
			p.secondary.SetVisible(true)
			p.secondary.SetSensitive(g.Players[g.Turn].Cash >= games.Bail)
		}
	case g.Phase == games.PhaseBuy:
		at := g.Players[g.Turn].At
		p.primary.SetLabel(i18n.Tf("Buy it for $%d", games.Board[at].Price)) // short: the panel keeps its width
		p.primary.SetSensitive(true)
		p.secondary.SetLabel(i18n.T("Don't buy"))
		p.secondary.SetVisible(true)
		p.secondary.SetSensitive(true)
	case g.Phase == games.PhaseShop:
		p.primary.SetLabel(i18n.T("Leave the shop"))
		p.primary.SetSensitive(true)
	case g.Phase == games.PhaseEnd:
		p.primary.SetLabel(i18n.T("End turn"))
		p.primary.SetSensitive(true)
	}
	p.refreshHand()

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
