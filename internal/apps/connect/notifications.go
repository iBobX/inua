package connect

import (
	"context"
	"html"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/kdeconnect"
	"github.com/lyricat/inua/internal/notify"
)

// phoneNotes shows the devices' notifications here: closing one closes it
// there, its buttons are pressed there, and one that can be answered is
// answered from here.
type phoneNotes struct {
	s *Service

	mu   sync.Mutex
	live map[string]uint32 // the device's ID and its notification's, to this one's
}

func newPhoneNotes(s *Service) *phoneNotes { return &phoneNotes{s: s, live: map[string]uint32{}} }

// noteBody is a notification packet's body.
type noteBody struct {
	ID             string   `json:"id"`
	AppName        string   `json:"appName"`
	Ticker         string   `json:"ticker,omitempty"`
	Title          string   `json:"title,omitempty"`
	Text           string   `json:"text,omitempty"`
	IsClearable    bool     `json:"isClearable"`
	IsCancel       bool     `json:"isCancel,omitempty"`
	Silent         bool     `json:"silent,omitempty"`
	PayloadHash    string   `json:"payloadHash,omitempty"`
	RequestReplyID string   `json:"requestReplyId,omitempty"`
	Actions        []string `json:"actions,omitempty"`
}

// validHash is an icon's hash, safe as a file's name.
var validHash = regexp.MustCompile(`^[0-9a-fA-F]{8,128}$`)

func (pn *phoneNotes) received(d kdeconnect.Device, p kdeconnect.Packet) {
	var b noteBody
	if p.Decode(&b) != nil || b.ID == "" {
		return
	}
	key := d.ID + "\x00" + b.ID
	pn.mu.Lock()
	old := pn.live[key]
	if b.IsCancel {
		delete(pn.live, key)
	}
	pn.mu.Unlock()
	if b.IsCancel {
		pn.s.notes.Close(old)
		return
	}
	if b.Silent && old == 0 {
		return // a notification the device shows without a word: music playing, a download
	}
	go pn.show(d, p, b, key, old)
}

func (pn *phoneNotes) show(d kdeconnect.Device, p kdeconnect.Packet, b noteBody, key string, old uint32) {
	title, body := b.Title, b.Text
	if title == "" {
		title = b.AppName
	}
	if body == "" && b.Title == "" {
		body = b.Ticker
	}
	note := Note{
		App:     b.AppName,
		Icon:    DeviceIcon(d.Type),
		Title:   title,
		Body:    html.EscapeString(body),
		Replace: old,
		Image:   pn.icon(d, p, b),
	}
	for _, a := range b.Actions {
		note.Actions = append(note.Actions, Action{"a:" + a, a})
	}
	if b.RequestReplyID != "" {
		note.Actions = append(note.Actions, Action{"reply", i18n.T("Reply")})
	}
	note.Acted = func(k string) {
		switch {
		case k == "reply":
			replyWindow(d.ID, b.RequestReplyID, title)
		case len(k) > 2 && k[:2] == "a:":
			pn.s.core.Send(d.ID, kdeconnect.NewPacket(typeNoteAction, map[string]string{"key": b.ID, "action": k[2:]}))
		}
	}
	note.Closed = func(byUser bool) {
		pn.mu.Lock()
		delete(pn.live, key)
		pn.mu.Unlock()
		if byUser && b.IsClearable {
			pn.s.core.Send(d.ID, kdeconnect.NewPacket(typeNoteRequest, map[string]string{"cancel": b.ID}))
		}
	}
	id := pn.s.notes.Post(note)
	pn.mu.Lock()
	pn.live[key] = id
	pn.mu.Unlock()
}

// icon is the notification's icon, fetched once and kept by its hash.
func (pn *phoneNotes) icon(d kdeconnect.Device, p kdeconnect.Packet, b noteBody) string {
	if p.PayloadTransferInfo == nil || !validHash.MatchString(b.PayloadHash) || p.PayloadSize > 4<<20 {
		return ""
	}
	dir := iconDir()
	path := filepath.Join(dir, b.PayloadHash+".png")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	r, err := pn.s.core.Payload(pn.s.ctx, d.ID, p)
	if err != nil {
		return ""
	}
	defer r.Close()
	if os.MkdirAll(dir, 0700) != nil {
		return ""
	}
	f, err := os.CreateTemp(dir, ".icon-*")
	if err != nil {
		return ""
	}
	_, err = f.ReadFrom(r)
	f.Close()
	if err != nil || os.Rename(f.Name(), path) != nil {
		os.Remove(f.Name())
		return ""
	}
	return path
}

// replyWindow opens the window to answer a notification.
func replyWindow(device, replyID, title string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	if err := exec.Command(self, "--reply", replyID, "--device", device, "--title", title).Start(); err != nil {
		log.Printf("connect: the reply window: %v", err)
	}
}

// noteForwarder shows this computer's notifications on the devices that
// want them: it watches the bus for them, as a monitor.
type noteForwarder struct {
	s *Service

	mu   sync.Mutex
	next int
}

func newNoteForwarder(s *Service) *noteForwarder { return &noteForwarder{s: s} }

func (f *noteForwarder) run(ctx context.Context) {
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return
	}
	defer conn.Close()
	if conn.Auth(nil) != nil || conn.Hello() != nil {
		return
	}
	rule := "type='method_call',interface='" + notesIface + "',member='Notify'"
	if err := conn.BusObject().Call("org.freedesktop.DBus.Monitoring.BecomeMonitor", 0, []string{rule}, uint32(0)).Err; err != nil {
		log.Printf("connect: watching notifications: %v", err)
		return
	}
	msgs := make(chan *dbus.Message, 64)
	conn.Eavesdrop(msgs)
	go func() { <-ctx.Done(); conn.Close() }()
	mine := ""
	if names := f.s.bus.Names(); len(names) > 0 {
		mine = names[0]
	}
	for m := range msgs {
		if m.Type != dbus.TypeMethodCall || len(m.Body) < 8 {
			continue
		}
		if sender, _ := m.Headers[dbus.FieldSender].Value().(string); sender == mine {
			continue // the devices' own, shown here
		}
		app, _ := m.Body[0].(string)
		summary, _ := m.Body[3].(string)
		body, _ := m.Body[4].(string)
		hints, _ := m.Body[6].(map[string]dbus.Variant)
		if v, ok := hints["transient"]; ok {
			if t, _ := v.Value().(bool); t {
				continue
			}
		}
		f.forward(app, summary, notify.PlainText(notify.Markup(body)))
	}
}

func (f *noteForwarder) forward(app, title, text string) {
	f.mu.Lock()
	f.next++
	id := "inua-" + strconv.Itoa(f.next)
	f.mu.Unlock()
	ticker := title
	if text != "" {
		ticker += ": " + text
	}
	p := kdeconnect.NewPacket(typeNote, noteBody{ID: id, AppName: app, Title: title, Text: text, Ticker: ticker, IsClearable: true})
	for _, d := range f.s.core.Devices() {
		if d.Reachable && d.Pair == kdeconnect.Paired && f.s.Setting(d.ID, SetNotifications) && d.Takes(typeNote) {
			f.s.core.Send(d.ID, p)
		}
	}
}

// request is a device asking for this computer's notifications, or
// closing one: they are sent as they come, and none is kept.
func (f *noteForwarder) request(d kdeconnect.Device, p kdeconnect.Packet) {}

// iconDir keeps the devices' notifications' icons.
func iconDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = filepath.Join(files.Home(), ".cache")
	}
	return filepath.Join(dir, "inua", "connect", "icons")
}
