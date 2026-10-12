package gamesui

import (
	"fmt"
	"math"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/games"
)

// lastTile is Last Tile: a floor of 5 × 5 tiles that fall away behind
// the player, ten maps to escape with every coin.
type lastTile struct {
	f                  *frame
	g                  *games.LastTile
	cv                 *canvas
	level              int
	maps               *choices
	moves, coins, best *gtk.Label
	from               games.Point // where the player slides from
	slide              float64     // 0 to 1
	falling            map[int]float64
}

const (
	tileSize = 82.0
	tileGap  = 8.0
	tileEdge = 14.0
	tileFull = 5*tileSize + 4*tileGap + 2*tileEdge
)

func (l *lastTile) setup(f *frame) {
	l.f = f
	f.win.SetDefaultSize(560, 780)
	l.moves = f.stat("MOVES")
	l.coins = f.stat("COINS")
	l.best = f.stat("BEST")
	l.cv = newCanvas(tileFull, tileFull+10, l.paint)
	f.add(l.cv)
	var names []string
	for i := range 10 {
		names = append(names, fmt.Sprint(i+1))
	}
	l.maps = f.choices(names, func(i int) {
		l.level = i
		l.reset()
	})
	l.cv.clicks(func(_ int, at games.Vec) {
		x := int(math.Floor((at.X - tileEdge) / (tileSize + tileGap)))
		y := int(math.Floor((at.Y - tileEdge) / (tileSize + tileGap)))
		if l.g.Result != games.Playing {
			l.next()
			return
		}
		l.move(games.Point{X: x, Y: y})
	})
	f.keyDown = l.key
}

func (l *lastTile) reset() {
	l.g = games.NewLastTile(l.level)
	l.from, l.slide = l.g.Player, 1
	l.falling = map[int]float64{}
	l.maps.set(l.level)
	l.update()
}

func (l *lastTile) redraw() { l.cv.redraw() }

func (l *lastTile) next() {
	l.level = (l.level + 1) % 10
	l.reset()
}

func (l *lastTile) key(k uint32) bool {
	if l.g.Result != games.Playing && (k == ' ' || k == keyEnter) {
		l.next()
		return true
	}
	d, ok := map[uint32]games.Point{
		'w': {Y: -1}, keyUp: {Y: -1}, 'a': {X: -1}, keyLeft: {X: -1},
		's': {Y: 1}, keyDown: {Y: 1}, 'd': {X: 1}, keyRight: {X: 1},
	}[k]
	if ok {
		l.move(l.g.Player.Add(d))
	}
	return ok
}

func (l *lastTile) move(p games.Point) {
	before := l.g.Player
	steps := l.g.Steps
	l.g.Move(p)
	if l.g.Steps == steps {
		return
	}
	l.from, l.slide = before, 0
	l.falling[before.Y*5+before.X] = 0
	if l.g.Result == games.Won {
		key := fmt.Sprintf("last-tile-%d", l.level+1)
		if l.g.Coins+1 > loadBest(key) {
			saveBest(key, l.g.Coins+1)
		}
	}
	l.update()
	l.cv.animate(func(dt float64) bool {
		l.slide = min(1, l.slide+dt/0.14)
		more := l.slide < 1
		for i, t := range l.falling {
			if t += dt; t >= 0.45 {
				delete(l.falling, i)
			} else {
				l.falling[i] = t
				more = true
			}
		}
		return more
	})
}

func (l *lastTile) update() {
	g := l.g
	l.moves.SetText(fmt.Sprintf("%d / 10", g.Steps))
	l.coins.SetText(fmt.Sprintf("%d / 4", g.Coins))
	if b := loadBest(fmt.Sprintf("last-tile-%d", l.level+1)); b > 0 {
		l.best.SetText(strings.Repeat("◆", b-1) + strings.Repeat("◇", 5-b))
	} else {
		l.best.SetText("—")
	}
	switch g.Result {
	case games.Won:
		if g.Coins == 4 {
			l.f.say(fmt.Sprintf("Out with all four coins in %d moves. Space for the next map.", g.Steps), "good")
		} else {
			l.f.say(fmt.Sprintf("Out, with %d of 4 coins. N to try again for all of them, Space for the next map.", g.Coins), "good")
		}
	case games.Lost:
		if g.Steps >= 10 {
			l.f.say("Ten moves, and still on the floor. N to try again.", "bad")
		} else {
			l.f.say("No tile left to step on. N to try again.", "bad")
		}
	default:
		l.f.say(fmt.Sprintf("Map %d: every tile you leave falls. Reach the exit within ten moves, picking up the coins.", l.level+1), "")
	}
	l.cv.redraw()
}

func tileAt(p games.Point) (float64, float64) {
	return tileEdge + float64(p.X)*(tileSize+tileGap), tileEdge + float64(p.Y)*(tileSize+tileGap)
}

func (l *lastTile) paint(pn pen) {
	c, g := l.f.col, l.g
	pn.Color(mix(c.app, c.surface, 0.5))
	pn.RoundRect(0, 0, tileFull, tileFull+10, 10)
	pn.Fill()
	face, side := mix(c.surface2, c.t0, 0.07), mix(c.surface2, c.app, 0.55)
	canGo := func(p games.Point) bool {
		if g.Result != games.Playing || !p.Inside(5, 5) || !g.Tiles[p.Y*5+p.X] {
			return false
		}
		d := p.Add(games.Point{X: -g.Player.X, Y: -g.Player.Y})
		return d.X*d.X+d.Y*d.Y == 1
	}
	for i := range g.Tiles {
		p := games.Point{X: i % 5, Y: i / 5}
		x, y := tileAt(p)
		// The hole under every tile.
		pn.Color(mix(c.app, c.surface, 0.15))
		pn.RoundRect(x, y+4, tileSize, tileSize, 8)
		pn.Fill()
		standing := g.Tiles[i]
		t, falling := l.falling[i]
		if !standing && !falling {
			continue
		}
		pn.Save()
		if falling {
			s := 1 - t/0.45*0.6
			pn.Translate(x+tileSize/2, y+tileSize/2+t*30)
			pn.Scale(s, s)
			pn.Translate(-tileSize/2, -tileSize/2)
		} else {
			pn.Translate(x, y)
		}
		top := face
		if p == g.Exit {
			top = mix(c.surface2, c.ok, 0.35)
		}
		pn.Color(side)
		pn.RoundRect(0, 6, tileSize, tileSize-2, 8)
		pn.Fill()
		pn.Color(top)
		pn.RoundRect(0, 0, tileSize, tileSize-4, 8)
		pn.Fill()
		if falling {
			pn.Alpha(c.app, t/0.45)
			pn.RoundRect(0, 0, tileSize, tileSize, 8)
			pn.Fill()
		}
		cx, cy := tileSize/2, tileSize/2-2
		if p == g.Exit {
			pn.Width(3)
			pn.Color(c.ok)
			pn.RoundRect(cx-14, cy-20, 28, 38, 3)
			pn.Stroke()
			pn.Circle(cx+7, cy+1, 2.5)
			pn.Fill()
			pn.Text("EXIT", "Geist Mono, IBM Plex Mono Bold 8", cx, cy+30, 0.5, 0.5, c.ok)
		}
		if g.Gold[i] && !falling {
			pn.Glow(cx, cy, 26, c.warn, 0.35)
			pn.Color(c.warn)
			pn.Circle(cx, cy, 13)
			pn.Fill()
			pn.Width(2)
			pn.Color(mix(c.warn, c.app, 0.35))
			pn.Circle(cx, cy, 8.5)
			pn.Stroke()
		}
		if canGo(p) {
			pn.Alpha(c.accent, 0.55)
			pn.Circle(cx, cy+26, 3)
			pn.Fill()
		}
		pn.Restore()
	}
	// The player, sliding from tile to tile.
	fx, fy := tileAt(l.from)
	tx, ty := tileAt(g.Player)
	s := l.slide * l.slide * (3 - 2*l.slide)
	px, py := fx+(tx-fx)*s+tileSize/2, fy+(ty-fy)*s+tileSize/2-2
	py -= math.Sin(s*math.Pi) * 12
	pn.Alpha(c.app, 0.4)
	pn.Circle(px+2, py+5, 18)
	pn.Fill()
	pn.Color(c.t0)
	pn.Circle(px, py, 19)
	pn.Fill()
	pn.Color(c.accent)
	pn.Circle(px, py, 15)
	pn.Fill()

	if g.Result != games.Playing && l.slide >= 1 {
		switch {
		case g.Result == games.Won && g.Coins == 4:
			l.f.veil(pn, tileFull, tileFull+10, "Out, with every coin", "Space for the next map", c.ok)
		case g.Result == games.Won:
			l.f.veil(pn, tileFull, tileFull+10, fmt.Sprintf("Out, with %d of 4 coins", g.Coins), "N to try again · Space for the next map", c.ok)
		default:
			l.f.veil(pn, tileFull, tileFull+10, "Stranded", "N to try again · Space for the next map", c.danger)
		}
	}
}
