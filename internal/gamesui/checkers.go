package gamesui

import (
	"context"
	"errors"
	"fmt"
	"html"
	"image/color"
	"math"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	"github.com/lyricat/inua/internal/assistant/answer"
	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/games/checkersai"
	"github.com/lyricat/inua/internal/games/checkersnet"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// Chinese checkers: the setup (who plays each seat and in which team, the
// rule, the network with its link and QR code), then the star on a canvas
// beside a panel of the players and what happened. The game is a
// checkersnet.Host's, on the network or not: people here move through it,
// and the computer, the AI and the browsers play through it too.

// seatChoices are what a seat's drop-down offers, in its order.
const (
	choiceHere = iota
	choiceOpen
	choiceEasy
	choiceNormal
	choiceHard
	choiceAI
)

type checkers struct {
	f     *frame
	pages *gtk.Stack
	cv    *canvas
	host  *checkersnet.Host
	g     *games.Checkers // the game shown
	round *gtk.Label

	// The setup.
	counts              []*gtk.ToggleButton
	seatBox             gtk.Box
	rows                []seatRow
	filling             bool
	noParking, network  *gtk.Switch
	start               *gtk.Button
	why                 gtk.Label
	join                *gtk.Box // the link, its QR code and the code, on the network
	joinCode, linkLabel gtk.Label
	linkMore, joinNote  gtk.Label
	copyLink            *gtk.Button
	onCopy              func(gtk.Button)
	qrCanvas            *canvas
	qr                  interface {
		GetWidth() int
		GetHeight() int
		Get(x, y int) bool
	}

	// The panel.
	turn, code   gtk.Label
	players, log gtk.Box
	again, setUp *gtk.Button
	logLines     int

	// The board: the holes as shown (a move in flight is not in them yet),
	// the moves still to show, the marble moving, the one picked.
	shown  []int
	queue  []games.CheckersEvent
	moving *flight
	picked int
	reach  map[int][]int
	angle  float64 // the board turned so the screen's player sits at the foot
	hover  int

	ai        *answer.Client
	aiState   aiState
	aiWhy     string
	onCount   func(gtk.ToggleButton)
	onSeat    func(uintptr, uintptr, uintptr)
	onTeam    func(uintptr, uintptr, uintptr)
	onProfile func(uintptr, uintptr, uintptr)
	onKick    func(gtk.Button)
	onRule    func(gtk.Switch, bool) bool
	onNetwork func(gtk.Switch, bool) bool
	onStart   func(gtk.Button)
	onAgain   func(gtk.Button)
	onSetUp   func(gtk.Button)
}

// seatRow is a seat's row in the setup.
type seatRow struct {
	box             *gtk.Box
	dot, name, sub  *gtk.Label
	who, team, prof *gtk.DropDown
	kick            *gtk.Button
}

// flight is a marble moving along a path, a hole a step.
type flight struct {
	owner int
	path  []int
	t     float64 // in steps along the path
}

const (
	ckUnit   = 40.0 // the gap between holes, in canvas units
	ckSide   = 760.0
	ckMarble = 0.36 // a marble's radius, in gaps
	ckStep   = 0.17 // seconds a step or a hop takes
)

func (p *checkers) theme() string { return "washi" }

func (p *checkers) setup(f *frame) {
	p.f, p.picked, p.hover = f, -1, -1
	f.win.SetDefaultSize(1280, 860)
	f.status.SetVisible(false)
	p.round = f.stat(i18n.T("ROUND"))
	p.round.GetParent().SetVisible(false)

	p.pages = gtk.NewStack()
	p.pages.SetTransitionType(gtk.StackTransitionTypeCrossfadeValue)
	p.pages.SetTransitionDuration(220)
	p.pages.SetVexpand(true)
	p.pages.AddNamed(&p.setupPage().Widget, "setup")

	row := gtk.NewBox(gtk.OrientationHorizontalValue, 18)
	p.cv = newCanvas(ckSide, ckSide, p.paint)
	p.cv.clicks(p.click)
	p.cv.hovers(func(at games.Vec) {
		if h := p.holeAt(at); h != p.hover {
			p.hover = h
			p.cv.redraw()
		}
	})
	gtkx.Append(row, &p.cv.area.Widget)
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
	p.turn = *gtk.NewLabel("")
	p.turn.AddCssClass("checkers-turn")
	p.turn.SetXalign(0)
	p.turn.SetWrap(true)
	gtkx.Append(players, &p.turn.Widget)
	p.players = *gtk.NewBox(gtk.OrientationVerticalValue, 0)
	gtkx.Append(players, &p.players.Widget)
	acts := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	acts.AddCssClass("fortune-acts")
	p.onAgain = func(gtk.Button) {
		if p.host != nil && p.host.Restart() {
			p.show(p.host.Game)
		}
	}
	p.onSetUp = func(gtk.Button) { p.reset() }
	p.again = button(i18n.T("New game"), &p.onAgain)
	p.again.SetHexpand(true)
	p.setUp = button(i18n.T("Set up again"), &p.onSetUp)
	p.setUp.SetHexpand(true)
	gtkx.Append(acts, &p.again.Widget)
	gtkx.Append(acts, &p.setUp.Widget)
	gtkx.Append(players, &acts.Widget)
	p.code = *gtk.NewLabel("")
	p.code.AddCssClass("fortune-city-sub")
	p.code.SetSelectable(true)
	p.code.SetXalign(0)
	p.code.SetWrap(true)
	gtkx.Append(players, &p.code.Widget)

	happened := section(i18n.T("WHAT HAPPENED"))
	happened.SetVexpand(true)
	p.log = *gtk.NewBox(gtk.OrientationVerticalValue, 4)
	sw := gtk.NewScrolledWindow()
	sw.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	sw.SetVexpand(true)
	sw.SetChild(&p.log.Widget)
	gtkx.Append(happened, &sw.Widget)

	f.keyDown = func(k uint32) bool {
		if k == 0xff1b && p.picked >= 0 { // Escape
			p.pick(-1)
			return true
		}
		return false
	}
}

// setupPage is the page before a game: on the left how many play and who
// plays each seat, in which team; on the right the rule and the network,
// its link and QR code; Start at the foot.
func (p *checkers) setupPage() *gtk.Box {
	page := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	page.AddCssClass("fortune-setup")
	page.SetHalign(gtk.AlignCenterValue)
	page.SetValign(gtk.AlignCenterValue)
	head := gtk.NewLabel(i18n.T("New game"))
	head.AddCssClass("fortune-setup-title")
	head.SetXalign(0)
	gtkx.Append(page, &head.Widget)

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
	cols := gtk.NewBox(gtk.OrientationHorizontalValue, 24)

	left := column(i18n.T("PLAYERS"))
	left.SetSizeRequest(520, -1)
	seg := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	seg.AddCssClass("segmented")
	seg.AddCssClass("linked")
	seg.SetHalign(gtk.AlignStartValue)
	p.onCount = func(b gtk.ToggleButton) {
		var n int
		fmt.Sscan(b.GetName(), &n)
		if b.GetActive() && !p.filling && p.host != nil && !p.host.SetPlayers(n) {
			p.fill() // more people than that: back to what it is
		}
	}
	for _, n := range []int{2, 3, 4, 6} {
		b := gtk.NewToggleButtonWithLabel(i18n.Tf("%d players", n))
		b.SetName(fmt.Sprint(n))
		if len(p.counts) > 0 {
			b.SetGroup(p.counts[0])
		}
		b.ConnectToggled(&p.onCount)
		gtkx.Append(seg, &b.Widget)
		p.counts = append(p.counts, b)
	}
	gtkx.Append(left, &seg.Widget)
	p.seatBox = *gtk.NewBox(gtk.OrientationVerticalValue, 0)
	p.seatBox.AddCssClass("fortune-roster")
	gtkx.Append(left, &p.seatBox.Widget)
	gtkx.Append(cols, &left.Widget)

	p.onSeat = func(ptr, _, _ uintptr) {
		if p.filling || p.host == nil {
			return
		}
		for i, r := range p.rows {
			if r.who.GoPointer() == ptr {
				p.setSeat(i, int(r.who.GetSelected()))
			}
		}
	}
	p.onTeam = func(ptr, _, _ uintptr) {
		if p.filling || p.host == nil {
			return
		}
		for i, r := range p.rows {
			if r.team.GoPointer() == ptr {
				p.host.SetTeam(i, int(r.team.GetSelected()))
			}
		}
	}
	p.onProfile = func(ptr, _, _ uintptr) {
		if p.filling || p.host == nil || p.ai == nil {
			return
		}
		for i, r := range p.rows {
			if r.prof.GoPointer() == ptr {
				if k := int(r.prof.GetSelected()); k < len(p.ai.Profiles) {
					p.host.SetSeat(i, checkersnet.AI, 0, p.ai.Profiles[k].Name, "")
				}
			}
		}
	}
	p.onKick = func(b gtk.Button) {
		var seat int
		fmt.Sscan(b.GetName(), &seat)
		if p.host != nil {
			p.host.Kick(seat)
		}
	}

	right := column(i18n.T("THE GAME"))
	right.SetSizeRequest(380, -1)
	grid := gtk.NewGrid()
	grid.AddCssClass("fortune-options")
	grid.SetRowSpacing(10)
	grid.SetColumnSpacing(18)
	option := func(n int32, label string, sub string, sw **gtk.Switch, handler *func(gtk.Switch, bool) bool) {
		l := gtk.NewLabel(label)
		l.AddCssClass("fortune-option")
		l.SetXalign(0)
		l.SetTooltipText(sub)
		grid.Attach(&l.Widget, 0, n, 1, 1)
		*sw = gtk.NewSwitch()
		(*sw).SetHalign(gtk.AlignStartValue)
		(*sw).SetValign(gtk.AlignCenterValue)
		(*sw).ConnectStateSet(handler)
		grid.Attach(&(*sw).Widget, 1, n, 1, 1)
	}
	p.onRule = func(_ gtk.Switch, on bool) bool {
		if !p.filling && p.host != nil {
			p.host.SetNoParking(on)
		}
		return false
	}
	p.onNetwork = func(_ gtk.Switch, on bool) bool {
		if !p.filling {
			p.serve(on)
		}
		return false
	}
	option(0, i18n.T("Stay out of other points"), i18n.T("A marble may cross another player's point, but not stop in it."), &p.noParking, &p.onRule)
	option(1, i18n.T("On the network"), i18n.T("Others join from a browser on this network, with the link or the QR code."), &p.network, &p.onNetwork)
	gtkx.Append(right, &grid.Widget)
	gtkx.Append(right, &p.joinBlock().Widget)
	p.joinNote = *gtk.NewLabel("")
	p.joinNote.AddCssClass("fortune-city-sub")
	p.joinNote.SetWrap(true)
	p.joinNote.SetXalign(0)
	gtkx.Append(right, &p.joinNote.Widget)
	gtkx.Append(cols, &right.Widget)
	gtkx.Append(page, &cols.Widget)

	foot := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	foot.AddCssClass("checkers-foot")
	p.onStart = func(gtk.Button) { p.begin() }
	p.start = button(i18n.T("Start the game"), &p.onStart)
	p.start.RemoveCssClass("outlined")
	p.start.AddCssClass("primary")
	p.start.AddCssClass("fortune-start")
	gtkx.Append(foot, &p.start.Widget)
	p.why = *gtk.NewLabel("")
	p.why.AddCssClass("fortune-city-sub")
	p.why.SetWrap(true)
	p.why.SetXalign(0)
	gtkx.Append(foot, &p.why.Widget)
	gtkx.Append(page, &foot.Widget)
	return page
}

// reset goes back to the setup: the game ended for the seats to be set up
// again, those on the network still in the lobby.
func (p *checkers) reset() {
	if p.host == nil {
		p.host = checkersnet.NewHost(checkersnet.Options{
			Players: 2, Local: []string{hostName()}, NoParking: true,
			Run: gtkx.Post, Changed: p.changed, Events: p.events, Seed: uint64(time.Now().UnixNano()),
		})
		p.host.SetSeat(1, checkersnet.Computer, checkersai.Normal, "", "")
	}
	p.host.ToLobby()
	p.g, p.queue, p.moving, p.picked = nil, nil, nil, -1
	p.round.GetParent().SetVisible(false)
	p.pages.SetVisibleChildName("setup")
	p.fill()
}

// changed: the lobby or the game changed (a visitor came, a move was made).
func (p *checkers) changed() {
	switch {
	case p.host == nil:
	case p.host.Game == nil:
		if p.pages.GetVisibleChildName() == "play" { // a browser's lead went back to the lobby
			p.reset()
			return
		}
		p.fill()
	case p.g != p.host.Game:
		p.show(p.host.Game) // started from a browser, or again
	default:
		p.refresh()
	}
}

// events queues what happened, for the board to show.
func (p *checkers) events(events []games.CheckersEvent) {
	if p.host == nil || p.g != p.host.Game {
		return
	}
	p.queue = append(p.queue, events...)
	p.play()
}

// fill sets the setup from the host's lobby.
func (p *checkers) fill() {
	h := p.host
	p.filling = true
	defer func() { p.filling = false }()
	for _, b := range p.counts {
		var n int
		fmt.Sscan(b.GetName(), &n)
		b.SetActive(n == len(h.Seats))
	}
	p.noParking.SetActive(h.NoParking)
	p.network.SetActive(h.Port() != 0)
	gtkx.Clear(&p.seatBox)
	p.rows = nil
	who := []string{i18n.T("At this screen"), i18n.T("Open: someone in a browser"),
		i18n.Tf("Computer · %s", i18n.T("Easy")), i18n.Tf("Computer · %s", i18n.T("Normal")), i18n.Tf("Computer · %s", i18n.T("Hard")), i18n.T("AI")}
	var teams []string
	for t := range 6 {
		teams = append(teams, i18n.Tf("Team %s", string(rune('A'+t))))
	}
	corners := games.SeatCorners[len(h.Seats)]
	for i, s := range h.Seats {
		r := seatRow{box: gtk.NewBox(gtk.OrientationHorizontalValue, 12)}
		r.box.AddCssClass("fortune-player")
		r.dot = gtk.NewLabel("")
		r.dot.SetMarkup(fmt.Sprintf(`<span foreground="%s" size="x-large">●</span>`, checkersnet.MarbleColors[corners[i]]))
		gtkx.Append(r.box, &r.dot.Widget)
		text := gtk.NewBox(gtk.OrientationVerticalValue, 1)
		text.SetHexpand(true)
		text.SetValign(gtk.AlignCenterValue)
		r.name, r.sub = gtk.NewLabel(""), gtk.NewLabel("")
		r.name.SetXalign(0)
		r.sub.SetXalign(0)
		r.sub.AddCssClass("fortune-city-sub")
		text.SetSizeRequest(150, -1)
		gtkx.Append(text, &r.name.Widget)
		gtkx.Append(text, &r.sub.Widget)
		gtkx.Append(r.box, &text.Widget)
		if s.Kind == checkersnet.Visitor {
			r.name.SetText(s.Name)
			r.sub.SetText(i18n.T("in a browser"))
			if i == h.Lead() {
				r.sub.SetText(i18n.T("in a browser · sets up the game"))
			}
			r.kick = button(i18n.T("Send away"), &p.onKick)
			r.kick.SetName(fmt.Sprint(i))
			r.kick.SetValign(gtk.AlignCenterValue)
			gtkx.Append(r.box, &r.kick.Widget)
		} else {
			r.who = gtk.NewDropDownFromStrings(who)
			r.who.SetValign(gtk.AlignCenterValue)
			r.who.SetSelected(uint32(seatChoice(s)))
			gtkx.ConnectNotify(r.who, "selected", &p.onSeat)
			gtkx.Append(r.box, &r.who.Widget)
			r.name.SetText(seatTitle(s))
			r.sub.SetText(p.seatSub(s))
			if s.Kind == checkersnet.AI && p.ai != nil && len(p.ai.Profiles) > 1 {
				var names []string
				for _, pr := range p.ai.Profiles {
					names = append(names, pr.Name)
				}
				r.prof = gtk.NewDropDownFromStrings(names)
				r.prof.SetValign(gtk.AlignCenterValue)
				r.prof.SetSelected(uint32(max(0, slices.Index(names, s.Profile))))
				gtkx.ConnectNotify(r.prof, "selected", &p.onProfile)
				gtkx.Append(r.box, &r.prof.Widget)
			}
		}
		r.team = gtk.NewDropDownFromStrings(teams)
		r.team.SetValign(gtk.AlignCenterValue)
		r.team.SetSelected(uint32(s.Team))
		gtkx.ConnectNotify(r.team, "selected", &p.onTeam)
		gtkx.Append(r.box, &r.team.Widget)
		gtkx.Append(&p.seatBox, &r.box.Widget)
		p.rows = append(p.rows, r)
	}
	why := h.Why()
	if why == "" && !h.HostLeads() && h.Port() != 0 {
		why = i18n.T("No one plays at this screen: the first to join from a browser sets the game up, and may start it.")
	}
	p.why.SetText(why)
	p.start.SetSensitive(h.Why() == "")
	p.showLink()
}

// seatChoice is the drop-down's choice for a seat.
func seatChoice(s *checkersnet.Seat) int {
	switch s.Kind {
	case checkersnet.Local:
		return choiceHere
	case checkersnet.Computer:
		return choiceEasy + int(s.Level)
	case checkersnet.AI:
		return choiceAI
	}
	return choiceOpen
}

// seatTitle is a seat's name in the setup: a person's, the computer's by
// its level, the AI's profile.
func seatTitle(s *checkersnet.Seat) string {
	switch s.Kind {
	case checkersnet.Local:
		return s.Name
	case checkersnet.Computer:
		return checkersnet.LevelNames[s.Level]
	case checkersnet.AI:
		if s.Profile != "" {
			return s.Profile
		}
		return i18n.T("AI")
	}
	return i18n.T("Open seat")
}

// seatSub is the line under a seat's name: who it is, or why the AI
// cannot play.
func (p *checkers) seatSub(s *checkersnet.Seat) string {
	switch s.Kind {
	case checkersnet.Local:
		return i18n.T("at this screen")
	case checkersnet.Computer:
		return i18n.T("the computer")
	case checkersnet.AI:
		switch p.aiState {
		case aiChecking:
			return i18n.T("Asking the AI…")
		case aiFailed:
			return p.aiWhy
		}
		return i18n.T("the AI of Settings → AI")
	}
	if p.host.Port() == 0 {
		return i18n.T("the computer, unless the game is on the network")
	}
	return i18n.T("waiting for someone to join")
}

// setSeat makes seat i choice c.
func (p *checkers) setSeat(i, c int) {
	h := p.host
	switch c {
	case choiceHere:
		name := hostName()
		if n := slices.IndexFunc(h.Seats, func(s *checkersnet.Seat) bool { return s.Kind == checkersnet.Local }); n >= 0 && n != i {
			name = i18n.Tf("Player %d", i+1)
		}
		h.SetSeat(i, checkersnet.Local, 0, "", name)
	case choiceOpen:
		h.SetSeat(i, checkersnet.Open, 0, "", "")
	case choiceEasy, choiceNormal, choiceHard:
		h.SetSeat(i, checkersnet.Computer, checkersai.Level(c-choiceEasy), "", "")
	case choiceAI:
		h.SetSeat(i, checkersnet.Computer, checkersai.Normal, "", "") // until the AI answers
		p.checkAI(i)
	}
	p.fill()
}

// checkAI starts the user's AI (inua-assistant --answer), then gives seat
// its first profile; meanwhile, and if it cannot, the computer plays it.
func (p *checkers) checkAI(seat int) {
	if p.aiState == aiReady {
		p.host.SetSeat(seat, checkersnet.AI, 0, p.ai.Profiles[0].Name, "")
		return
	}
	if p.aiState == aiChecking {
		return
	}
	p.aiState = aiChecking
	p.fill()
	go func() {
		c, err := startAI()
		if err == nil && len(c.Profiles) == 0 {
			err = errors.New(i18n.T("no profile of Settings → AI makes decisions"))
		}
		gtkx.Post(func() {
			if err != nil {
				p.aiState, p.aiWhy = aiFailed, err.Error()
				p.fill()
				return
			}
			p.aiState, p.ai = aiReady, c
			var names []string
			for _, pr := range c.Profiles {
				names = append(names, pr.Name)
			}
			p.host.SetAI(func(ctx context.Context, profile, instructions, state string, options []string) (int, error) {
				return c.Choose(ctx, profile, instructions, state, options)
			}, names)
			if p.host.Game == nil {
				p.host.SetSeat(seat, checkersnet.AI, 0, names[0], "")
			}
			p.fill()
		})
	}()
}

// serve puts the lobby on the network, or takes it off.
func (p *checkers) serve(on bool) {
	h := p.host
	if on && h.Port() == 0 {
		if err := h.Serve(checkersnet.DefaultAddr); err != nil {
			p.why.SetText(err.Error())
			p.network.SetActive(false)
			return
		}
	}
	if !on {
		h.Unserve()
	}
	p.fill()
}

// joinBlock is how others join, while the game is on the network: the
// QR code, beside it the join code to type and the link to open or copy,
// and the computer's other addresses under them.
func (p *checkers) joinBlock() *gtk.Box {
	p.join = gtk.NewBox(gtk.OrientationHorizontalValue, 18)
	p.join.AddCssClass("checkers-join")
	p.join.SetVisible(false)
	p.qrCanvas = newCanvas(ckQR, ckQR, p.paintQR)
	p.qrCanvas.area.SetSizeRequest(ckQR, ckQR)
	p.qrCanvas.area.SetFocusable(false)
	p.qrCanvas.area.SetValign(gtk.AlignStartValue)
	p.qrCanvas.area.SetHexpand(false)
	p.qrCanvas.area.AddCssClass("checkers-qr")
	gtkx.Append(p.join, &p.qrCanvas.area.Widget)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	text.SetHexpand(true)
	k := gtk.NewLabel(i18n.T("JOIN CODE"))
	k.AddCssClass("kicker")
	k.SetXalign(0)
	gtkx.Append(text, &k.Widget)
	p.joinCode = *gtk.NewLabel("")
	p.joinCode.AddCssClass("checkers-code")
	p.joinCode.SetSelectable(true)
	p.joinCode.SetXalign(0)
	gtkx.Append(text, &p.joinCode.Widget)
	sub := gtk.NewLabel(i18n.T("Scan the code, or open the link in a browser on this network."))
	sub.AddCssClass("fortune-city-sub")
	sub.SetWrap(true)
	sub.SetXalign(0)
	gtkx.Append(text, &sub.Widget)
	link := func(l *gtk.Label, class string) {
		*l = *gtk.NewLabel("")
		l.AddCssClass(class)
		l.SetSelectable(true)
		l.SetXalign(0)
		l.SetWrap(true)
		l.SetWrapMode(pango.WrapCharValue) // a link has no spaces to break at
		gtkx.Append(text, &l.Widget)
	}
	link(&p.linkLabel, "checkers-link")
	p.onCopy = func(gtk.Button) {
		if urls := p.host.URLs(); len(urls) > 0 {
			clip := p.f.win.GetClipboard()
			clip.SetText(urls[0])
			clip.Unref()
			p.copyLink.SetLabel(i18n.T("Copied"))
			time.AfterFunc(1500*time.Millisecond, func() { gtkx.Post(func() { p.copyLink.SetLabel(i18n.T("Copy link")) }) })
		}
	}
	p.copyLink = button(i18n.T("Copy link"), &p.onCopy)
	p.copyLink.SetHalign(gtk.AlignStartValue)
	p.copyLink.AddCssClass("checkers-copy")
	gtkx.Append(text, &p.copyLink.Widget)
	link(&p.linkMore, "checkers-link-more")
	gtkx.Append(p.join, &text.Widget)
	return p.join
}

// ckQR is the QR code's side.
const ckQR = 132

// showLink shows how to join while the lobby is on the network.
func (p *checkers) showLink() {
	urls := []string{}
	if p.host.Port() != 0 {
		urls = p.host.URLs()
	}
	on := len(urls) > 0
	p.join.SetVisible(on)
	p.joinNote.SetText("")
	p.qr = nil
	if !on {
		if p.host.Port() != 0 {
			p.joinNote.SetText(i18n.T("This computer is on no network a browser could reach it from."))
		}
		return
	}
	if m, err := qrcode.NewQRCodeWriter().EncodeWithoutHint(urls[0], gozxing.BarcodeFormat_QR_CODE, 0, 0); err == nil {
		p.qr = m
	}
	p.qrCanvas.redraw()
	p.joinCode.SetText(p.host.Code)
	// No hyphen where a link breaks: it would read as part of it.
	plain := func(s string) string { return `<span insert_hyphens="false">` + html.EscapeString(s) + `</span>` }
	p.linkLabel.SetMarkup(plain(urls[0]))
	p.linkMore.SetMarkup(plain(strings.Join(urls[1:], "\n")))
	p.linkMore.SetVisible(len(urls) > 1)
}

func (p *checkers) paintQR(pn pen) {
	pn.Color(white)
	pn.Rect(0, 0, ckQR, ckQR)
	pn.Fill()
	m := p.qr
	if m == nil {
		return
	}
	k := ckQR / float64(m.GetWidth())
	pn.Color(color.NRGBA{0x1F, 0x1B, 0x17, 255})
	for y := range m.GetHeight() {
		for x := range m.GetWidth() {
			if m.Get(x, y) {
				pn.Rect(float64(x)*k, float64(y)*k, k+0.3, k+0.3)
			}
		}
	}
	pn.Fill()
}

// begin starts the game as set up.
func (p *checkers) begin() {
	if p.host.Start() {
		p.show(p.host.Game)
	} else {
		p.fill()
	}
}

// show shows game g: the board turned so the first person here sits at
// its foot (else the first player).
func (p *checkers) show(g *games.Checkers) {
	p.g, p.queue, p.moving, p.picked, p.reach = g, nil, nil, -1, nil
	p.shown = slices.Clone(g.Holes)
	me := slices.IndexFunc(p.host.Seats, func(s *checkersnet.Seat) bool { return s.Kind == checkersnet.Local })
	start := g.Players[max(me, 0)].Start
	p.angle = float64(3-start) * math.Pi / 3 // its point to the foot (corner 3)
	gtkx.Clear(&p.log)
	p.logLines = 0
	p.round.GetParent().SetVisible(true)
	p.pages.SetVisibleChildName("play")
	p.refresh()
	p.cv.redraw()
}

// mine reports whether the turn is a person's here, and nothing is moving.
func (p *checkers) mine() bool {
	g := p.g
	return g != nil && !g.Over && p.host.Seats[g.Turn].Kind == checkersnet.Local && len(p.queue) == 0 && p.moving == nil
}

// pick picks the marble in hole h (-1 for none), with the holes it may go.
func (p *checkers) pick(h int) {
	p.picked, p.reach = h, nil
	if h >= 0 {
		p.reach = p.g.Reach(h)
	}
	p.cv.redraw()
}

// click picks a marble of the turn's, or moves the one picked.
func (p *checkers) click(_ int, at games.Vec) {
	if !p.mine() {
		return
	}
	h := p.holeAt(at)
	switch {
	case h < 0:
		p.pick(-1)
	case p.picked >= 0 && p.reach[h] != nil:
		from := p.picked
		p.pick(-1)
		p.host.Act(p.g.Turn, from, h)
	case p.g.Holes[h] >= 0 && slices.Contains(p.g.Movers(), p.g.Holes[h]):
		p.pick(h)
	default:
		p.pick(-1)
	}
}

// at is where hole h is drawn on the canvas, the board turned.
func (p *checkers) at(h int) (float64, float64) {
	x, y := games.StarXY(h)
	s, c := math.Sin(p.angle), math.Cos(p.angle)
	return ckSide/2 + (x*c-y*s)*ckUnit, ckSide/2 + (x*s+y*c)*ckUnit
}

// holeAt is the hole at a point of the canvas, -1 for none.
func (p *checkers) holeAt(at games.Vec) int {
	best, bestD := -1, 0.45*ckUnit
	for h := range games.StarHoles {
		x, y := p.at(h)
		if d := math.Hypot(at.X-x, at.Y-y); d < bestD {
			best, bestD = h, d
		}
	}
	return best
}

// play shows the queued moves, one after another, then the game as it is.
func (p *checkers) play() {
	p.cv.animate(func(dt float64) bool {
		if p.g == nil { // back to the setup meanwhile
			p.queue, p.moving = nil, nil
			return false
		}
		if p.moving == nil {
			for len(p.queue) > 0 {
				e := p.queue[0]
				p.queue = p.queue[1:]
				p.note(e)
				if e.Kind == games.CkMove {
					p.moving = &flight{owner: e.Owner, path: e.Path}
					p.shown[e.Path[0]] = -1
					break
				}
			}
		}
		if p.moving == nil {
			p.shown = slices.Clone(p.g.Holes)
			p.refresh()
			return false
		}
		m := p.moving
		m.t += dt / ckStep
		if m.t >= float64(len(m.path)-1) {
			p.shown[m.path[len(m.path)-1]] = m.owner
			p.moving = nil
		}
		return true
	})
}

// note writes what happened in the log.
func (p *checkers) note(e games.CheckersEvent) {
	g := p.g
	name := func(i int) string { return g.Players[i].Name }
	var line string
	switch e.Kind {
	case games.CkMove:
		hops := len(e.Path) - 1
		switch {
		case e.Owner != e.Player:
			line = i18n.Tf("%s moved %s's marble", name(e.Player), name(e.Owner))
		case hops > 1 || !slices.Contains(neighbours(e.Path[0]), e.Path[1]):
			line = i18n.Tf("%s hopped %d times", name(e.Player), hops)
		default:
			return // a step: not worth a line
		}
	case games.CkPass:
		line = i18n.Tf("%s could not move", name(e.Player))
	case games.CkHome:
		line = i18n.Tf("%s is home", name(e.Player))
	case games.CkWin:
		line = p.winners(e.Players)
	case games.CkDraw:
		line = i18n.T("No one got home in time: a draw.")
	}
	l := gtk.NewLabel(line)
	l.AddCssClass("fortune-log")
	l.SetXalign(0)
	l.SetWrap(true)
	p.log.Prepend(&l.Widget)
	l.Unref()
	if p.logLines++; p.logLines > 60 {
		p.log.Remove(p.log.GetLastChild())
		p.logLines--
	}
}

// neighbours are the holes next to h.
func neighbours(h int) []int {
	var out []int
	x, y := games.StarXY(h)
	for o := range games.StarHoles {
		ox, oy := games.StarXY(o)
		if d := math.Hypot(x-ox, y-oy); d > 0.5 && d < 1.5 {
			out = append(out, o)
		}
	}
	return out
}

// winners says who won: a player, or a team's players.
func (p *checkers) winners(players []int) string {
	var names []string
	for _, i := range players {
		names = append(names, p.g.Players[i].Name)
	}
	if len(names) == 1 {
		return i18n.Tf("%s wins", names[0])
	}
	all := names[0]
	for _, n := range names[1:] {
		all = i18n.Tf("%s and %s", all, n)
	}
	return i18n.Tf("%s win", all)
}

// refresh sets the panel from the game.
func (p *checkers) refresh() {
	g := p.g
	if g == nil {
		return
	}
	p.round.SetText(fmt.Sprint(g.Round))
	people := 0
	for _, s := range p.host.Seats {
		if s.Kind == checkersnet.Local {
			people++
		}
	}
	turn := g.Players[g.Turn]
	switch {
	case g.Over && g.WinTeam >= 0:
		var team []int
		for i, pl := range g.Players {
			if pl.Team == g.WinTeam {
				team = append(team, i)
			}
		}
		p.turn.SetText(p.winners(team))
	case g.Over:
		p.turn.SetText(i18n.T("No one got home in time: a draw."))
	case p.host.Seats[g.Turn].Kind == checkersnet.Local:
		text := i18n.T("Your turn: pick one of your marbles.")
		if people > 1 {
			text = i18n.Tf("%s's turn", turn.Name) + " · " + text
		}
		if turn.Home {
			text = i18n.Tf("You are home: move %s's marbles.", g.Players[g.Movers()[0]].Name)
		}
		p.turn.SetText(text)
	default:
		p.turn.SetText(i18n.Tf("%s's turn", turn.Name))
	}
	gtkx.Clear(&p.players)
	for _, i := range g.Standings() {
		pl := g.Players[i]
		s := p.host.Seats[i]
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("fortune-player")
		if i == g.Turn && !g.Over {
			row.AddCssClass("checkers-now")
		}
		name := gtk.NewLabel("")
		name.SetMarkup(fmt.Sprintf(`<span foreground="%s">●</span>  %s  <span alpha="55%%">%s</span>`,
			checkersnet.MarbleColors[pl.Start], html.EscapeString(pl.Name), html.EscapeString(i18n.Tf("Team %s", string(rune('A'+pl.Team))))))
		name.SetXalign(0)
		name.SetHexpand(true)
		gtkx.Append(row, &name.Widget)
		state := i18n.Tf("%d to go", g.Left(i)-20)
		switch {
		case pl.Home:
			state = i18n.Tf("#%d home", pl.Place)
		case s.Away:
			state = i18n.T("away, the computer plays")
		}
		st := gtk.NewLabel(state)
		st.AddCssClass("fortune-city-sub")
		gtkx.Append(row, &st.Widget)
		gtkx.Append(&p.players, &row.Widget)
	}
	p.again.SetVisible(g.Over || p.host.HostLeads())
	if p.host.Port() != 0 {
		p.code.SetText(i18n.Tf("Join code %s", p.host.Code))
	} else {
		p.code.SetText("")
	}
	if p.picked >= 0 && !p.mine() {
		p.pick(-1)
	}
	p.cv.redraw()
}

func (p *checkers) redraw() {
	p.cv.redraw()
	p.qrCanvas.redraw()
}

// paint draws the star: a board of the paper, each player's point tinted
// in their colour, the holes, the marbles, the last move's way, and for
// the person whose turn it is here the marble picked and where it may go.
func (p *checkers) paint(pn pen) {
	col := p.f.col
	if p.g == nil {
		return
	}
	cx := ckSide / 2
	// The board: a hexagram a little larger than the holes.
	star := func(r float64) {
		for i := range 12 {
			rr := r
			if i%2 == 1 {
				rr = r * 0.58
			}
			a := p.angle + float64(i)*math.Pi/6 - math.Pi/2
			x, y := cx+rr*math.Cos(a), cx+rr*math.Sin(a)
			if i == 0 {
				pn.MoveTo(x, y)
			} else {
				pn.LineTo(x, y)
			}
		}
		pn.Close()
	}
	pn.Alpha(col.t0, 0.08)
	star(9.6*ckUnit + 6)
	pn.Fill()
	pn.Color(col.surface)
	star(9.6 * ckUnit)
	pn.FillKeep()
	pn.Alpha(col.line, 0.9)
	pn.Width(1.5)
	pn.Stroke()
	// Each point in its player's colour, faintly.
	for i, pl := range p.g.Players {
		c := hexColor(checkersnet.MarbleColors[pl.Start])
		for _, h := range games.CornerHoles(pl.Start) {
			x, y := p.at(h)
			pn.Alpha(c, 0.13)
			pn.Circle(x, y, 0.62*ckUnit)
			pn.Fill()
		}
		_ = i
	}
	// The last move's way.
	if len(p.g.Last) > 1 && p.moving == nil {
		pn.Alpha(col.t2, 0.55)
		pn.Width(2)
		pn.Dash(4, 5)
		for i, h := range p.g.Last {
			x, y := p.at(h)
			if i == 0 {
				pn.MoveTo(x, y)
			} else {
				pn.LineTo(x, y)
			}
		}
		pn.Stroke()
		pn.Dash()
	}
	// The holes.
	for h := range games.StarHoles {
		x, y := p.at(h)
		pn.Alpha(col.t0, 0.22)
		pn.Circle(x, y, 0.17*ckUnit)
		pn.Fill()
		pn.Alpha(white, 0.5)
		pn.Arc(x, y, 0.17*ckUnit, 0.2, math.Pi-0.2)
		pn.Width(1)
		pn.Stroke()
	}
	// Where the marble picked may go.
	mine := p.mine()
	for h := range p.reach {
		x, y := p.at(h)
		pn.Alpha(col.accent, 0.18)
		pn.Circle(x, y, 0.33*ckUnit)
		pn.Fill()
		pn.Alpha(col.accent, 0.8)
		pn.Width(2)
		pn.Circle(x, y, 0.33*ckUnit)
		pn.Stroke()
	}
	// The marbles.
	movers := []int{}
	if mine {
		movers = p.g.Movers()
	}
	for h, owner := range p.shown {
		if owner < 0 {
			continue
		}
		x, y := p.at(h)
		p.marble(pn, x, y, owner, 1)
		switch {
		case h == p.picked:
			pn.Color(col.t0)
			pn.Width(2.5)
			pn.Circle(x, y, (ckMarble+0.12)*ckUnit)
			pn.Stroke()
		case slices.Contains(movers, owner) && (h == p.hover || p.picked < 0):
			pn.Alpha(col.t0, 0.35)
			pn.Width(1.5)
			pn.Circle(x, y, (ckMarble+0.09)*ckUnit)
			pn.Stroke()
		}
	}
	if m := p.moving; m != nil {
		i := min(int(m.t), len(m.path)-2)
		f := m.t - float64(i)
		x0, y0 := p.at(m.path[i])
		x1, y1 := p.at(m.path[i+1])
		lift := 1 + 0.22*math.Sin(f*math.Pi) // a hop rises and falls
		pn.Alpha(col.t0, 0.15)
		pn.Circle(x0+(x1-x0)*f+3, y0+(y1-y0)*f+5, ckMarble*ckUnit)
		pn.Fill()
		p.marble(pn, x0+(x1-x0)*f, y0+(y1-y0)*f-8*math.Sin(f*math.Pi), m.owner, lift)
	}
}

// marble draws a player's marble at x, y, k times its size: its colour, a
// darker rim, a gleam.
func (p *checkers) marble(pn pen, x, y float64, owner int, k float64) {
	c := hexColor(checkersnet.MarbleColors[p.g.Players[owner].Start])
	r := ckMarble * ckUnit * k
	pn.Alpha(p.f.col.t0, 0.18)
	pn.Circle(x+1.5, y+2.5, r)
	pn.Fill()
	pn.Color(c)
	pn.Circle(x, y, r)
	pn.FillKeep()
	pn.Alpha(color.NRGBA{0, 0, 0, 255}, 0.28)
	pn.Width(1.2)
	pn.Stroke()
	pn.Glow(x-r*0.32, y-r*0.36, r*0.55, white, 0.6)
}
