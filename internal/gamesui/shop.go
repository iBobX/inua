package gamesui

import (
	"fmt"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/gtkx"
)

// diceShop is Three Dice Shop: each morning, three dice to share between
// buying stock, selling it and mending the shop, for a week.
type diceShop struct {
	f                           *frame
	g                           *games.Shop
	cv                          *canvas
	day, cash, stock, condition *gtk.Label
	forecast                    *gtk.Label
	open                        *gtk.Button
	ledger                      gtk.Box
	picked                      int // the die picked up, -1 none
	onOpen                      func(gtk.Button)
}

const (
	shopW    = 660.0
	shopH    = 356.0
	dieSize  = 66.0
	cardW    = 196.0
	cardH    = 176.0
	cardTop  = 140.0
	cardGap  = (shopW - 3*cardW) / 4
	slotSize = 78.0
)

var jobNames = [3]string{"Stock", "Sales", "Repairs"}
var jobLines = [3]string{"buy crates at $2", "sell at $5 each", "mend the shop"}

func (s *diceShop) setup(f *frame) {
	s.f = f
	f.win.SetDefaultSize(720, 740)
	s.day = f.stat("DAY")
	s.cash = f.stat("CASH")
	s.stock = f.stat("STOCK")
	s.condition = f.stat("CONDITION")
	s.cv = newCanvas(shopW, shopH, s.paint)
	s.cv.top = true
	s.cv.area.SetVexpand(false)
	s.cv.area.SetContentHeight(int32(shopH))
	f.add(s.cv)
	s.cv.clicks(func(_ int, at games.Vec) { s.click(at) })

	foot := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	foot.AddCssClass("shop-foot")
	s.forecast = gtk.NewLabel("")
	s.forecast.AddCssClass("shop-forecast")
	s.forecast.SetWrap(true)
	gtkx.Append(foot, &s.forecast.Widget)
	s.onOpen = func(gtk.Button) { s.endDay() }
	s.open = button("Open the shop for the day", &s.onOpen)
	s.open.RemoveCssClass("outlined")
	s.open.AddCssClass("primary")
	s.open.SetHalign(gtk.AlignCenterValue)
	gtkx.Append(foot, &s.open.Widget)
	s.ledger = *gtk.NewBox(gtk.OrientationVerticalValue, 3)
	s.ledger.AddCssClass("shop-ledger")
	gtkx.Append(foot, &s.ledger.Widget)
	gtkx.Append(&f.body, &foot.Widget)

	f.keyDown = func(k uint32) bool {
		switch {
		case k >= '1' && k <= '3':
			s.pick(int(k - '1'))
		case k == 'z' || k == 'x' || k == 'c':
			s.assign(map[uint32]int{'z': 0, 'x': 1, 'c': 2}[k])
		case k == keyEnter || k == ' ':
			s.endDay()
		default:
			return false
		}
		return true
	}
}

func (s *diceShop) reset() {
	s.g = games.NewShop()
	s.picked = -1
	gtkx.Clear(&s.ledger)
	s.update()
}

func (s *diceShop) redraw() { s.cv.redraw() }

func (s *diceShop) pick(d int) {
	if s.g.Result != games.Playing {
		return
	}
	if s.picked == d {
		s.picked = -1
	} else {
		s.picked = d
	}
	s.update()
}

// assign puts the picked die on a job; with none picked, the job's die
// is picked up again.
func (s *diceShop) assign(job int) {
	if s.g.Result != games.Playing {
		return
	}
	if s.picked < 0 {
		if d := s.g.Jobs[job]; d >= 0 {
			s.picked = d
		}
		s.update()
		return
	}
	// A die already on the job swaps to where the picked one was.
	old := s.g.Jobs[job]
	from := -1
	for j, d := range s.g.Jobs {
		if d == s.picked {
			from = j
		}
	}
	s.g.Assign(job, s.picked)
	if old >= 0 && old != s.picked && from >= 0 {
		s.g.Assign(from, old)
	}
	s.picked = -1
	// The last free die goes to the last free job by itself.
	free, open := -1, -1
	n := 0
	for d := range 3 {
		used := false
		for _, x := range s.g.Jobs {
			used = used || x == d
		}
		if !used {
			free = d
			n++
		}
	}
	for j, x := range s.g.Jobs {
		if x < 0 {
			open = j
		}
	}
	if n == 1 && open >= 0 {
		s.g.Assign(open, free)
	}
	s.update()
}

func (s *diceShop) endDay() {
	if s.g.Result != games.Playing {
		s.reset()
		return
	}
	if !s.g.EndDay() {
		return
	}
	l := gtk.NewLabel(s.g.Message)
	l.SetXalign(0)
	s.ledger.Prepend(&l.Widget)
	l.Unref()
	s.picked = -1
	s.update()
}

func (s *diceShop) click(at games.Vec) {
	for d := range 3 {
		if x, y, ok := s.diePos(d); ok && at.X >= x && at.X < x+dieSize && at.Y >= y && at.Y < y+dieSize {
			s.pick(d)
			return
		}
	}
	for j := range 3 {
		x := cardGap + float64(j)*(cardW+cardGap)
		if at.X >= x && at.X < x+cardW && at.Y >= cardTop && at.Y < cardTop+cardH {
			s.assign(j)
			return
		}
	}
}

// diePos is where die d is drawn: in the row of the day's dice, or in
// its job's slot.
func (s *diceShop) diePos(d int) (float64, float64, bool) {
	for j, x := range s.g.Jobs {
		if x == d {
			cx := cardGap + float64(j)*(cardW+cardGap) + cardW/2
			return cx - dieSize/2, cardTop + 58, true
		}
	}
	return shopW/2 + float64(d-1)*110 - dieSize/2, 40, true
}

func (s *diceShop) update() {
	g := s.g
	s.day.SetText(fmt.Sprintf("%d / %d", g.Day, games.Days))
	s.cash.SetText(fmt.Sprintf("$%d", g.Cash))
	s.stock.SetText(fmt.Sprint(g.Stock))
	s.condition.SetText(fmt.Sprintf("%d / 9", g.Condition))
	d := g.Forecast()
	switch {
	case g.Result == games.Won:
		s.forecast.SetText(fmt.Sprintf("The week is done: $%d in the till, past the $%d goal.", g.Cash, games.ShopTarget))
		s.open.SetLabel("Another week")
		s.f.say("A good week. The shop stays open.", "good")
	case g.Result == games.Lost:
		s.forecast.SetText(fmt.Sprintf("The week is done: $%d in the till, short of the $%d goal.", g.Cash, games.ShopTarget))
		if g.Cash < 0 {
			s.forecast.SetText("The rent could not be paid: the shop has closed.")
		}
		s.open.SetLabel("Another week")
		s.f.say("Not this week. N, or the button, for another.", "bad")
	case g.Ready():
		s.forecast.SetText(fmt.Sprintf("Tonight: buy %d, sell %d, pay $%d rent  →  cash $%d, stock %d, condition %d",
			d.Bought, d.Sold, games.Rent, d.Cash, g.Stock+d.Bought-d.Sold, d.Condition))
		s.open.SetLabel("Open the shop for the day")
		s.f.say(fmt.Sprintf("Day %d of %d. Have $%d by the end of day %d.", g.Day, games.Days, games.ShopTarget, games.Days), "")
	default:
		s.forecast.SetText("Give each job a die: pick one up, then click a job.")
		s.open.SetLabel("Open the shop for the day")
		s.f.say(fmt.Sprintf("Day %d of %d. Have $%d by the end of day %d.", g.Day, games.Days, games.ShopTarget, games.Days), "")
	}
	s.open.SetSensitive(g.Ready() || g.Result != games.Playing)
	s.cv.redraw()
}

func (s *diceShop) paint(pn pen) {
	c, g := s.f.col, s.g
	pn.Text("THE DAY'S DICE", "Geist Mono, IBM Plex Mono Bold 9", shopW/2, 14, 0.5, 0.5, c.t2)
	d := g.Forecast()
	effects := [3]string{
		fmt.Sprintf("buys %d · −$%d", d.Bought, d.Bought*games.StockCost),
		fmt.Sprintf("sells %d · +$%d", d.Sold, d.Sold*games.SalePrice),
		fmt.Sprintf("condition %d → %d", g.Condition, d.Condition),
	}
	for j := range 3 {
		x := cardGap + float64(j)*(cardW+cardGap)
		pn.Color(c.surface)
		pn.RoundRect(x, cardTop, cardW, cardH, 8)
		pn.Fill()
		pn.Width(1)
		pn.Color(c.line)
		if s.picked >= 0 && g.Result == games.Playing {
			pn.Color(mix(c.line, c.accent, 0.6))
		}
		pn.RoundRect(x+0.5, cardTop+0.5, cardW-1, cardH-1, 8)
		pn.Stroke()
		cx := x + cardW/2
		pn.Text(jobNames[j], "IBM Plex Sans Medium 14", cx, cardTop+22, 0.5, 0.5, c.t0)
		pn.Text(jobLines[j], "IBM Plex Sans 10", cx, cardTop+40, 0.5, 0.5, c.t2)
		pn.Width(1.5)
		pn.Dash(4, 4)
		pn.Color(mix(c.surface, c.t2, 0.5))
		pn.RoundRect(cx-slotSize/2, cardTop+52, slotSize, slotSize, 10)
		pn.Stroke()
		pn.Dash()
		if g.Jobs[j] >= 0 {
			pn.Text(effects[j], "Geist Mono, IBM Plex Mono 10", cx, cardTop+cardH-16, 0.5, 0.5, c.accent)
		} else if j == 1 {
			pn.Text(fmt.Sprintf("+%d for condition", g.Condition/3), "Geist Mono, IBM Plex Mono 9", cx, cardTop+cardH-16, 0.5, 0.5, c.t2)
		}
	}
	for i, v := range g.Dice {
		x, y, _ := s.diePos(i)
		lift := 0.0
		if i == s.picked {
			lift = 6
		}
		pn.Alpha(c.app, 0.4)
		pn.RoundRect(x+2, y+4, dieSize, dieSize, 12)
		pn.Fill()
		face := c.t0
		if g.Result != games.Playing {
			face = mix(c.t0, c.surface, 0.4)
		}
		pn.Color(face)
		pn.RoundRect(x, y-lift, dieSize, dieSize, 12)
		pn.Fill()
		if i == s.picked {
			pn.Width(3)
			pn.Color(c.accent)
			pn.RoundRect(x-3, y-lift-3, dieSize+6, dieSize+6, 14)
			pn.Stroke()
		}
		pn.Color(c.app)
		for _, p := range pips[v] {
			pn.Circle(x+dieSize/2+p[0]*18, y-lift+dieSize/2+p[1]*18, 5.5)
			pn.Fill()
		}
		pn.Text(fmt.Sprint(i+1), "Geist Mono, IBM Plex Mono 8", x+dieSize-8, y+dieSize+10, 0.5, 0.5, c.t2)
	}
	// The way to the goal.
	frac := max(0, min(1, float64(g.Cash)/games.ShopTarget))
	pn.Color(c.surface2)
	pn.RoundRect(cardGap, shopH-12, shopW-2*cardGap, 6, 3)
	pn.Fill()
	pn.Color(mix(c.accent, c.ok, frac))
	pn.RoundRect(cardGap, shopH-12, (shopW-2*cardGap)*frac, 6, 3)
	pn.Fill()
	pn.Text(fmt.Sprintf("$%d of $%d", g.Cash, games.ShopTarget), "Geist Mono, IBM Plex Mono 9", shopW-cardGap, shopH-24, 1, 0.5, c.t2)
	pn.Text("GOAL", "Geist Mono, IBM Plex Mono Bold 8", cardGap, shopH-24, 0, 0.5, c.t2)
}
