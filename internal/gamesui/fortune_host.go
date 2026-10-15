package gamesui

import (
	"bytes"
	"fmt"
	"html"
	"image/color"
	"image/jpeg"
	"math/rand/v2"
	"os/user"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	"github.com/lyricat/inua/internal/games"
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
		Run:      gtkx.Post,
		Changed:  p.netChanged,
		Events:   p.netEvents,
		Print:    boardJPEG,
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
