package gamesui

import (
	"bytes"
	"fmt"
	"html"
	goimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"os/user"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/games/fortuneai"
	"github.com/lyricat/inua/internal/games/fortunenet"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// Small Fortune for the local network (fortunenet): set up with "others
// join from a browser", the game opens a lobby first, with the link and
// its QR code, the people at the screen and the visitors as they join;
// the host starts the game, which fortunenet then holds. The board shows
// it as it does a game of its own; the moves of the people here go to the
// game, then fortunenet hands what happened to the board and every
// browser alike.

// hostLobby opens the lobby: the game listening on the network, the page
// with its link, until the host starts the game or cancels.
func (p *fortune) hostLobby() {
	var local []string
	switch {
	case p.people == 1:
		local = []string{hostName()}
	case p.people > 1:
		for i := range p.people {
			local = append(local, i18n.Tf("Player %d", i+1))
		}
	}
	p.net = fortunenet.NewHost(fortunenet.Options{
		Settings: p.settings,
		Local:    local,
		Open:     p.open,
		Run:      gtkx.Post,
		Changed:  p.netChanged,
		Events:   p.netEvents,
		Print:    boardJPEG,
		Art:      artPicture,
		Pieces:   cityPieces,
		AI:       p.netAI(),
		Seed:     uint64(time.Now().UnixNano()),
	})
	p.lobbyNote.SetText("")
	if err := p.net.Serve(fortunenet.DefaultAddr); err != nil {
		p.lobbyNote.SetText(i18n.Tf("The game could not open to the network: %v", err))
	}
	urls := p.net.URLs()
	p.lobbyURL.SetText(strings.Join(urls[:min(1, len(urls))], ""))
	p.lobbyMore.SetText(strings.Join(urls[min(1, len(urls)):], "\n"))
	p.lobbyMore.SetVisible(len(urls) > 1)
	p.qr = nil
	if len(urls) > 0 {
		if m, err := qrcode.NewQRCodeWriter().EncodeWithoutHint(urls[0], gozxing.BarcodeFormat_QR_CODE, 0, 0); err == nil {
			p.qr = m
		}
	}
	p.qrCanvas.redraw()
	p.pages.SetVisibleChildName("lobby")
	p.lobbySeats()
	p.lobbyStart.GrabFocus()
	p.lobbyURL.SelectRegion(0, 0) // a selectable label shows all of itself selected at first
}

// hostName is the name the host plays under, for the visitors to see:
// their first name, or their login.
func hostName() string {
	if u, err := user.Current(); err == nil {
		if name := strings.Fields(strings.Split(u.Name, ",")[0]); len(name) > 0 {
			return name[0]
		}
		return u.Username
	}
	return i18n.T("Host")
}

// netChanged: the lobby or the game changed, a visitor came or went.
func (p *fortune) netChanged() {
	switch {
	case p.net == nil:
	case p.pages.GetVisibleChildName() == "lobby":
		p.lobbySeats()
	case p.g == p.net.Game:
		p.refresh()
	}
}

// netEvents shows what happened in the hosted game, as the board shows
// its own game's events.
func (p *fortune) netEvents(events []games.FortuneEvent) {
	if p.net == nil || p.g != p.net.Game {
		return
	}
	for _, e := range events {
		p.queue = append(p.queue, e.For(p.me))
	}
	p.idle = false
	p.play()
}

// lobbySeats lists the players so far, a visitor with a button to send
// them away.
func (p *fortune) lobbySeats() {
	gtkx.Clear(&p.lobbyList)
	for i, s := range p.net.Seats {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("fortune-player")
		col := p.playerColor(min(i, 3))
		name := gtk.NewLabel("")
		name.SetMarkup(fmt.Sprintf(`<span foreground="#%02x%02x%02x">●</span>  %s`, col.R, col.G, col.B, html.EscapeString(s.Name)))
		name.SetXalign(0)
		name.SetHexpand(true)
		gtkx.Append(row, &name.Widget)
		kind := gtk.NewLabel(map[fortunenet.SeatKind]string{fortunenet.Local: i18n.T("at this screen"), fortunenet.Visitor: i18n.T("in a browser")}[s.Kind])
		kind.AddCssClass("fortune-city-sub")
		gtkx.Append(row, &kind.Widget)
		if s.Kind == fortunenet.Visitor {
			b := button(i18n.T("Send away"), &p.onKick)
			b.SetName(fmt.Sprint(i))
			gtkx.Append(row, &b.Widget)
		}
		gtkx.Append(&p.lobbyList, &row.Widget)
	}
	waiting := i18n.T("Waiting for visitors: open the link on a phone or a computer on the same network.")
	if room := p.net.Room(); room > 0 {
		waiting += " " + i18n.Tf("Room for %d more", room)
	}
	p.lobbyWait.SetText(waiting)
	p.lobbyStart.SetSensitive(p.net.CanStart())
}

// hostStart starts the hosted game with the lobby as it is.
func (p *fortune) hostStart() {
	if p.net == nil || !p.net.Start() {
		return
	}
	var here uint8
	for i, s := range p.net.Seats {
		if s.Kind == fortunenet.Local {
			here |= games.Seat(i)
		}
	}
	p.newGame(p.net.Game, here)
	p.code.SetText(p.net.Code)
	p.code.GetParent().SetVisible(true)
}

// hostClose closes the hosted game, if any: every browser is sent away.
func (p *fortune) hostClose() {
	if p.net != nil {
		p.net.Close()
		p.net = nil
	}
	p.code.GetParent().SetVisible(false)
}

// setupLobby is the lobby's page: the link and its QR code beside the
// players, Start and Cancel.
func (p *fortune) setupLobby() *gtk.Box {
	page := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	page.AddCssClass("fortune-setup")
	page.SetHalign(gtk.AlignCenterValue)
	page.SetValign(gtk.AlignCenterValue)
	head := gtk.NewLabel(i18n.T("Others join from a browser"))
	head.AddCssClass("fortune-setup-title")
	head.SetXalign(0)
	gtkx.Append(page, &head.Widget)

	row := gtk.NewBox(gtk.OrientationHorizontalValue, 28)
	p.qrCanvas = newCanvas(qrSide, qrSide, p.paintQR)
	p.qrCanvas.area.SetSizeRequest(qrSide, qrSide)
	p.qrCanvas.area.SetFocusable(false)
	p.qrCanvas.area.SetValign(gtk.AlignStartValue)
	gtkx.Append(row, &p.qrCanvas.area.Widget)
	side := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	side.SetSizeRequest(420, -1)
	p.lobbyURL = gtk.NewLabel("")
	p.lobbyURL.AddCssClass("fortune-card-title")
	p.lobbyURL.SetSelectable(true)
	p.lobbyURL.SetXalign(0)
	gtkx.Append(side, &p.lobbyURL.Widget)
	p.lobbyMore = gtk.NewLabel("")
	p.lobbyMore.AddCssClass("fortune-city-sub")
	p.lobbyMore.SetSelectable(true)
	p.lobbyMore.SetXalign(0)
	gtkx.Append(side, &p.lobbyMore.Widget)
	p.lobbyWait = gtk.NewLabel("")
	p.lobbyWait.AddCssClass("fortune-city-sub")
	p.lobbyWait.SetWrap(true)
	p.lobbyWait.SetXalign(0)
	gtkx.Append(side, &p.lobbyWait.Widget)
	p.lobbyList = *gtk.NewBox(gtk.OrientationVerticalValue, 0)
	gtkx.Append(side, &p.lobbyList.Widget)
	p.lobbyNote = gtk.NewLabel("")
	p.lobbyNote.AddCssClass("fortune-city-sub")
	p.lobbyNote.SetWrap(true)
	p.lobbyNote.SetXalign(0)
	gtkx.Append(side, &p.lobbyNote.Widget)
	gtkx.Append(row, &side.Widget)
	gtkx.Append(page, &row.Widget)

	p.onKick = func(b gtk.Button) {
		var seat int
		fmt.Sscan(b.GetName(), &seat)
		if p.net != nil {
			p.net.Kick(seat)
		}
	}
	p.onLobbyStart = func(gtk.Button) { p.hostStart() }
	p.onLobbyCancel = func(gtk.Button) {
		p.hostClose()
		p.reset()
	}
	acts := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	p.lobbyStart = button(i18n.T("Start the game"), &p.onLobbyStart)
	p.lobbyStart.RemoveCssClass("outlined")
	p.lobbyStart.AddCssClass("primary")
	p.lobbyStart.AddCssClass("fortune-start")
	gtkx.Append(acts, &p.lobbyStart.Widget)
	cancel := button(i18n.T("Cancel"), &p.onLobbyCancel)
	cancel.AddCssClass("fortune-start")
	gtkx.Append(acts, &cancel.Widget)
	gtkx.Append(page, &acts.Widget)
	return page
}

const qrSide = 220

// paintQR draws the link's QR code, dark modules on white with its quiet
// zone.
func (p *fortune) paintQR(pn pen) {
	pn.Color(white)
	pn.Rect(0, 0, qrSide, qrSide)
	pn.Fill()
	m := p.qr
	if m == nil {
		return
	}
	n := float64(m.GetWidth())
	k := qrSide / n
	pn.Color(color.NRGBA{0x1F, 0x1B, 0x17, 255})
	for y := range m.GetHeight() {
		for x := range m.GetWidth() {
			if m.Get(x, y) {
				pn.Rect(float64(x)*k, float64(y)*k, k+0.3, k+0.3) // a little over, no seams
			}
		}
	}
	pn.Fill()
}

// artScale is how many pixels a unit of the cards and signs is drawn at,
// for the browsers' screens.
const artScale = 3

// artPicture is one of the pictures fortunenet serves, drawn as the
// board draws it: a card, its face (with its price tag) or its back, a
// little over its size for its shadow; a square's sign; a house or hotel
// in the city's style; a piece of cityPieces.
func artPicture(city int, name string) *fortunenet.Picture {
	var w, h float64
	var draw func(pn pen)
	var c, sq, along, owner int
	art := cityArts[city]
	switch {
	case scan2(name, "house-%d-%d", &along, &owner) && along < 2 && owner < 4:
		col := (&fortune{}).playerColor(owner)
		return standing(700, 700, func(pn pen) { art.house(pn, 700, 700, 11, along == 1, col) })
	case scan2(name, "hotel-%d-%d", &along, &owner) && along < 2 && owner < 4:
		col := (&fortune{}).playerColor(owner)
		su, sv := 38.0, 18.0
		if along == 0 {
			su, sv = 18, 38
		}
		return standing(700, 700, func(pn pen) { art.hotel(pn, 700, 700, su, sv, col) })
	case scan(name, "piece-%d", &c):
		pieces := piecesOf(city)
		if c >= len(pieces) {
			return nil
		}
		return standing(pieces[c].u, pieces[c].v, pieces[c].draw)
	case name == "card-back":
		w, h, draw = cardW+4, cardH+4, func(pn pen) { pn.Translate(2, 2); paintCard(pn, 0, true, 0) }
	case scan(name, "card-%d-price", &c) && c < len(games.Cards):
		k := games.CardKind(c)
		w, h, draw = cardW+4, cardH+4, func(pn pen) { pn.Translate(2, 2); paintCard(pn, k, false, games.Cards[k].Price) }
	case scan(name, "card-%d", &c) && c < len(games.Cards):
		k := games.CardKind(c)
		w, h, draw = cardW+4, cardH+4, func(pn pen) { pn.Translate(2, 2); paintCard(pn, k, false, 0) }
	case scan(name, "sign-%d", &sq) && sq < len(games.Board):
		w, h, draw = 40, 40, func(pn pen) { paintSquareSign(pn, city, sq) }
	default:
		return nil
	}
	im := newImage(int(w*artScale), int(h*artScale))
	defer im.Free()
	im.Draw(func(pn pen) {
		pn.Scale(artScale, artScale)
		draw(pn)
	})
	var b bytes.Buffer
	if png.Encode(&b, unpremultiply(im.RGBA())) != nil {
		return nil
	}
	return &fortunenet.Picture{PNG: b.Bytes()}
}

// piece is something standing on a city's board: where, and how drawn
// (there, as the board draws it).
type piece struct {
	u, v float64
	draw func(pn pen)
}

// piecesOf are what stands on a city's board, as paintUnder has them:
// its landmarks, its trees, the fountain.
func piecesOf(city int) []piece {
	art := cityArts[city]
	var out []piece
	for _, l := range landmarksOf(city) {
		out = append(out, piece{l.at.X, l.at.Y, func(pn pen) {
			c := project(l.at.X, l.at.Y, 0)
			pn.Translate(c.X, c.Y)
			pn.Scale(landmarkK, landmarkK)
			pn.Translate(-c.X, -c.Y)
			l.draw(pn, l.at.X, l.at.Y)
		}})
	}
	for _, t := range art.trees {
		u, v, s := t[0]*games.MapK, t[1]*games.MapK, t[2]*treeK
		out = append(out, piece{u, v, func(pn pen) { art.tree(pn, u, v, s, rand.New(rand.NewPCG(uint64(t[0]), uint64(t[1])))) }})
	}
	fc := games.TrackOf(city).Tiles[games.FountainAt].At(0, 24)
	out = append(out, piece{fc.X, fc.Y, func(pn pen) { fountainPiece(pn, fc.X, fc.Y, art.spout) }})
	return out
}

// cityPieces names a city's pieces for fortunenet, where they stand.
func cityPieces(city int) []fortunenet.Piece {
	var out []fortunenet.Piece
	for i, p := range piecesOf(city) {
		out = append(out, fortunenet.Piece{Name: fmt.Sprintf("piece-%d", i), X: p.u, Y: p.v})
	}
	return out
}

// standing is a piece standing at board point u, v, drawn there by draw
// as the board draws it, cropped to what it covers: with its size and
// foot in board units, for fortunenet.Picture.
func standing(u, v float64, draw func(pn pen)) *fortunenet.Picture {
	const left, up, w, h = 300.0, 640.0, 600.0, 760.0 // canvas units round the foot
	const k = 3                                       // pixels to a canvas unit
	foot := project(u, v, 0)
	im := newImage(int(w*k), int(h*k))
	defer im.Free()
	im.Draw(func(pn pen) {
		pn.Scale(k, k)
		pn.Translate(left-foot.X, up-foot.Y)
		draw(pn)
	})
	full := unpremultiply(im.RGBA())
	box := goimage.Rectangle{Min: full.Rect.Max, Max: full.Rect.Min}
	for y := full.Rect.Min.Y; y < full.Rect.Max.Y; y++ {
		for x := full.Rect.Min.X; x < full.Rect.Max.X; x++ {
			if full.Pix[full.PixOffset(x, y)+3] > 6 {
				box.Min.X, box.Min.Y = min(box.Min.X, x), min(box.Min.Y, y)
				box.Max.X, box.Max.Y = max(box.Max.X, x+1), max(box.Max.Y, y+1)
			}
		}
	}
	if box.Empty() {
		return nil
	}
	var b bytes.Buffer
	if png.Encode(&b, full.SubImage(box)) != nil {
		return nil
	}
	unit := k * boardK // pixels to a board unit, across
	return &fortunenet.Picture{
		PNG: b.Bytes(),
		W:   float64(box.Dx()) / unit, H: float64(box.Dy()) / unit,
		FootX: (left*k - float64(box.Min.X)) / unit,
		Dv:    (float64(box.Max.Y) - up*k) / (k * boardK * elevSin),
	}
}

// scan2 is scan with two numbers.
func scan2(name, format string, a, b *int) bool {
	_, err := fmt.Sscanf(name, format, a, b)
	return err == nil && fmt.Sprintf(format, *a, *b) == name && *a >= 0 && *b >= 0
}

// scan reports whether name is format with its one number, in n, and
// nothing else.
func scan(name, format string, n *int) bool {
	_, err := fmt.Sscanf(name, format, n)
	return err == nil && fmt.Sprintf(format, *n) == name && *n >= 0
}

// unpremultiply turns cairo's premultiplied pixels into straight ones,
// for PNG.
func unpremultiply(im *goimage.RGBA) *goimage.NRGBA {
	out := goimage.NewNRGBA(im.Rect)
	for i := 0; i < len(im.Pix); i += 4 {
		a := im.Pix[i+3]
		out.Pix[i+3] = a
		if a == 0 {
			continue
		}
		for k := range 3 {
			out.Pix[i+k] = uint8(min(255, int(im.Pix[i+k])*255/int(a)))
		}
	}
	return out
}

// boardJPEG is a city's board print, as the board shows it, for the
// visitors' board.
func boardJPEG(city int) []byte {
	const n = 2048
	im := newImage(n, n)
	defer im.Free()
	im.Draw(func(q pen) {
		q.Scale(n/fBoard, n/fBoard)
		paintPrint(q, city)
	})
	r := rand.New(rand.NewPCG(11, 3))
	im.Speckle(r, n*n/9, washiInk, 0.12)
	im.Speckle(r, n*n/14, white, 0.35)
	var b bytes.Buffer
	if jpeg.Encode(&b, im.RGBA(), &jpeg.Options{Quality: 86}) != nil {
		return nil
	}
	return b.Bytes()
}

// netAI is the driver of the AI players for the host, when any play.
func (p *fortune) netAI() *fortuneai.Driver {
	if len(p.settings.AIs) == 0 || p.ai == nil {
		return nil
	}
	p.ai.SetProfiles(p.aiProfiles)
	return p.ai
}
