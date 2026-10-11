package notify

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
)

func TestMarkup(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"plain text", "plain text"},
		{"Tom & Jerry <3", "Tom &amp; Jerry &lt;3"},
		{"a &amp; b &#39; &lt;", "a &amp; b &#39; &lt;"},
		{"<b>bold</b> and <i>it</i>", "<b>bold</b> and <i>it</i>"},
		{"<B>caps</B>", "<b>caps</b>"},
		{"<b>never closed", "<b>never closed</b>"},
		{"stray </i> closer", "stray  closer"},
		{"<b><i>crossed</b></i>", "<b><i>crossed</i></b>"},
		{`<a href="https://x.org/?a=1&b=2">link</a>`, `<a href="https://x.org/?a=1&amp;b=2">link</a>`},
		{`<img src="x.png" alt="pic"/>after`, "after"},
		{"line<br/>next", "line\nnext"},
		{"<script>x</script>", "&lt;script&gt;x&lt;/script&gt;"},
		{"<body>", "&lt;body&gt;"},
		{"5 > 3", "5 &gt; 3"},
	} {
		if got := Markup(c.in); got != c.want {
			t.Errorf("Markup(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := PlainText(Markup("<b>Tom</b> &amp; <i>Jerry</i>")); got != "Tom & Jerry" {
		t.Errorf("PlainText: %q", got)
	}
}

func note(id uint32, app string) Notification {
	return Notification{ID: id, App: app, Summary: "s", Time: time.Now()}
}

func TestCenter(t *testing.T) {
	var c Center
	if !c.Post(note(1, "mail"), Policy{}) || !c.Post(note(2, "chat"), Policy{}) || !c.Post(note(3, "mail"), Policy{}) {
		t.Fatal("banners are shown outside Focus")
	}
	groups := c.Groups()
	if len(groups) != 2 || groups[0].Key != "mail" || len(groups[0].Items) != 2 || groups[0].Items[0].ID != 3 {
		t.Fatalf("groups: %+v", groups)
	}
	// A replacement moves to the top, without a second copy.
	r := note(2, "chat")
	r.Summary = "edited"
	c.Post(r, Policy{})
	if len(c.Items) != 3 || c.Items[0].ID != 2 || c.Items[0].Summary != "edited" {
		t.Fatalf("replace: %+v", c.Items)
	}
	// The desktop entry groups before the app name.
	e := note(4, "Mail")
	e.Entry = "mail"
	c.Post(e, Policy{})
	if g := c.Groups(); len(g) != 2 || g[0].Key != "mail" || len(g[0].Items) != 3 {
		t.Fatalf("entry key: %+v", g)
	}

	// Focus: quiet banners, except critical ones when allowed; the center
	// still collects.
	c.Focus = true
	crit := note(5, "alarm")
	crit.Urgency = Critical
	if c.Post(note(6, "chat"), Policy{CriticalInFocus: true}) {
		t.Error("banner in Focus")
	}
	if !c.Post(crit, Policy{CriticalInFocus: true}) || c.Post(crit, Policy{}) {
		t.Error("critical in Focus follows the policy")
	}
	if _, ok := c.Get(6); !ok {
		t.Error("Focus kept a notification out of the center")
	}

	// Transient: a banner, never kept.
	c.Focus = false
	tr := note(7, "vol")
	tr.Transient = true
	if !c.Post(tr, Policy{}) {
		t.Error("transient banner")
	}
	if _, ok := c.Get(7); ok {
		t.Error("transient notification kept")
	}

	if ids := c.ClearApp("chat"); len(ids) != 2 {
		t.Errorf("ClearApp: %v", ids)
	}
	if !c.Remove(1) || c.Remove(1) {
		t.Error("Remove")
	}
	if c.MaxID() != 5 {
		t.Errorf("MaxID %d", c.MaxID())
	}
	if ids := c.ClearAll(); len(ids) != 3 || len(c.Items) != 0 {
		t.Errorf("ClearAll: %v %v", ids, c.Items)
	}
}

func TestCenterKeepsTheNewest(t *testing.T) {
	var c Center
	for i := range Keep + 5 {
		c.Post(note(uint32(i+1), "x"), Policy{})
	}
	if len(c.Items) != Keep || c.Items[0].ID != Keep+5 {
		t.Errorf("kept %d, newest %d", len(c.Items), c.Items[0].ID)
	}
}

func TestUnreadAndSave(t *testing.T) {
	c := Center{Seen: time.Now().Add(-time.Minute)}
	old := note(1, "a")
	old.Time = time.Now().Add(-time.Hour)
	c.Post(old, Policy{})
	n := note(2, "b")
	n.Actions = []Action{{"reply", "Reply"}}
	n.Image = &Image{Width: 1, Height: 1, Stride: 3, Data: []byte{1, 2, 3}}
	c.Post(n, Policy{})
	c.Focus = true
	if c.Unread() != 1 {
		t.Errorf("unread %d", c.Unread())
	}
	path := filepath.Join(t.TempDir(), "state", "notifications.json")
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Focus || len(got.Items) != 2 || got.Items[0].Actions[0].Label != "Reply" || got.Items[0].Image != nil {
		t.Errorf("loaded %+v", got)
	}
	if empty, err := Load(filepath.Join(t.TempDir(), "none.json")); err != nil || len(empty.Items) != 0 {
		t.Errorf("missing file: %+v %v", empty, err)
	}
}

type recorder struct {
	notes  chan Notification
	closes chan uint32
}

func (r recorder) Notify(n Notification) { r.notes <- n }
func (r recorder) Close(id uint32)       { r.closes <- id }

func connect(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestServer(t *testing.T) {
	addr := dbustest.Start(t)
	ctx := t.Context()

	// Another daemon holds the name; the server waits, then takes over.
	other := connect(t, addr)
	if reply, err := other.RequestName(Name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("other daemon: %v %v", reply, err)
	}
	rec := recorder{make(chan Notification, 4), make(chan uint32, 4)}
	srv := NewServer(connect(t, addr), rec)
	srv.SetNextID(41)
	owned := make(chan bool, 4)
	go srv.Serve(ctx, func(on bool) { owned <- on })
	time.Sleep(200 * time.Millisecond)
	if srv.Owner() {
		t.Fatal("took the name from another daemon")
	}
	other.ReleaseName(Name)
	select {
	case on := <-owned:
		if !on {
			t.Fatal("owner callback: false")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("did not take over the name")
	}

	app := connect(t, addr)
	obj := app.Object(Name, Path)
	var caps []string
	if err := obj.Call(iface+".GetCapabilities", 0).Store(&caps); err != nil || len(caps) == 0 {
		t.Fatalf("capabilities: %v %v", caps, err)
	}
	img := struct {
		W, H, Stride   int32
		Alpha          bool
		Bits, Channels int32
		Data           []byte
	}{2, 1, 8, true, 8, 4, make([]byte, 8)}
	hints := map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(2)),
		"desktop-entry": dbus.MakeVariant("org.gnome.Nautilus.desktop"),
		"image-data":    dbus.MakeVariant(img),
	}
	var id uint32
	err := obj.Call(iface+".Notify", 0, "Files", uint32(0), "file:///usr/share/x.png", "Copied", "a & <b>b</b>",
		[]string{"default", "Open", "undo", "Undo"}, hints, int32(-1)).Store(&id)
	if err != nil || id != 41 {
		t.Fatalf("Notify: id %d, %v", id, err)
	}
	n := <-rec.notes
	if n.ID != 41 || n.Entry != "org.gnome.Nautilus" || n.Icon != "/usr/share/x.png" || n.Body != "a &amp; <b>b</b>" ||
		!n.Default || len(n.Actions) != 1 || n.Actions[0].Key != "undo" || n.Urgency != Critical ||
		n.Image == nil || n.Image.Width != 2 || !n.Image.Alpha || n.Sender == "" {
		t.Errorf("notification: %+v", n)
	}
	// replaces_id keeps the ID.
	if err := obj.Call(iface+".Notify", 0, "Files", id, "", "Copied 2", "", []string{}, map[string]dbus.Variant{}, int32(0)).Store(&id); err != nil || id != 41 {
		t.Errorf("replace: %d %v", id, err)
	}
	<-rec.notes
	if err := obj.Call(iface+".CloseNotification", 0, uint32(41)).Err; err != nil {
		t.Fatal(err)
	}
	if got := <-rec.closes; got != 41 {
		t.Errorf("close %d", got)
	}

	// Signals reach the app.
	if err := app.AddMatchSignal(dbus.WithMatchInterface(iface)); err != nil {
		t.Fatal(err)
	}
	sigs := make(chan *dbus.Signal, 4)
	app.Signal(sigs)
	srv.Invoke(41, "undo", "tok")
	srv.Closed(41, Dismissed)
	want := []string{iface + ".ActivationToken", iface + ".ActionInvoked", iface + ".NotificationClosed"}
	for _, name := range want {
		select {
		case s := <-sigs:
			if s.Name != name {
				t.Errorf("signal %s, want %s", s.Name, name)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no %s", name)
		}
	}
}

func TestImageSizes(t *testing.T) {
	variant := func(w, h, stride int32, alpha bool, bits, channels int32, data []byte) dbus.Variant {
		return dbus.MakeVariant([]any{w, h, stride, alpha, bits, channels, data})
	}
	ok := variant(2, 2, 8, true, 8, 4, make([]byte, 16))
	if img := readImage(ok); img == nil || len(img.Pixels()) != 16 {
		t.Fatalf("a 2×2 RGBA image: %+v", img)
	}
	for name, v := range map[string]dbus.Variant{
		// w*4 overflows int32 to a small number: once passed, then the
		// renderer sliced past the data.
		"overflowing width": variant(1<<30, 1, 4, true, 8, 4, make([]byte, 16)),
		"overflowing rows":  variant(4, 1<<30, 16, true, 8, 4, make([]byte, 64)),
		"short data":        variant(4, 4, 16, true, 8, 4, make([]byte, 63)),
		"short stride":      variant(4, 4, 15, true, 8, 4, make([]byte, 64)),
		"negative stride":   variant(4, 4, -16, true, 8, 4, make([]byte, 64)),
		"huge":              variant(MaxImageSide+1, 1, (MaxImageSide+1)*3, false, 8, 3, make([]byte, (MaxImageSide+1)*3)),
		"16-bit":            variant(2, 2, 8, true, 16, 4, make([]byte, 16)),
	} {
		if img := readImage(v); img != nil {
			t.Errorf("%s: kept %dx%d stride %d", name, img.Width, img.Height, img.Stride)
		}
	}
	// Pixels checks on its own too, for an Image made elsewhere.
	if (&Image{Width: 1 << 30, Height: 1, Stride: 4, Alpha: true, Data: make([]byte, 16)}).Pixels() != nil {
		t.Error("Pixels kept an image larger than its data")
	}
}
