package vt

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newTerm is a terminal from the library scripts/libghostty.sh built into
// bin/, or skips the test without it.
func newTerm(t *testing.T, cols, rows int) *Terminal {
	t.Helper()
	if os.Getenv("INUA_VT_LIB") == "" {
		if p, err := filepath.Abs(filepath.Join("..", "..", "..", "bin", Library)); err == nil {
			os.Setenv("INUA_VT_LIB", p)
		}
	}
	term, err := New(cols, rows)
	if errors.Is(err, ErrMissing) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(term.Close)
	return term
}

func TestText(t *testing.T) {
	term := newTerm(t, 20, 5)
	term.Write([]byte("hello\r\n\x1b[1mbold\x1b[0m world\r\nplaceholder\x1b[3;1H\x1b[2Koverwritten"))
	got, err := term.Text(false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "hello\nbold world\noverwritten"; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
	if x, y := term.Cursor(); x != 11 || y != 2 {
		t.Errorf("Cursor = %d,%d, want 11,2", x, y)
	}
}

// Wide characters take two cells; a long line wraps and unwraps.
func TestWideAndWrap(t *testing.T) {
	term := newTerm(t, 10, 4)
	term.Write([]byte("日本語\r\n0123456789abc"))
	wrapped, _ := term.Text(false)
	unwrapped, _ := term.Text(true)
	if wrapped != "日本語\n0123456789\nabc" || unwrapped != "日本語\n0123456789abc" {
		t.Errorf("wrapped %q, unwrapped %q", wrapped, unwrapped)
	}
	if x, _ := term.Cursor(); x != 3 {
		t.Errorf("cursor at %d after abc", x)
	}
}

// What the shell and the agents send for the title and the folder.
func TestTitleAndPwd(t *testing.T) {
	term := newTerm(t, 40, 5)
	if term.Title() != "" || term.Pwd() != "" {
		t.Fatal("title or pwd before any was set")
	}
	term.Write([]byte("\x1b]2;✳ Move the PTY reader\x07\x1b]7;file://osx/home/lyric/Codework/inua\x1b\\"))
	if got := term.Title(); got != "✳ Move the PTY reader" {
		t.Errorf("Title = %q", got)
	}
	if got := term.Pwd(); !strings.HasSuffix(got, "/home/lyric/Codework/inua") {
		t.Errorf("Pwd = %q", got)
	}
}

func TestResize(t *testing.T) {
	term := newTerm(t, 10, 3)
	term.Write([]byte("0123456789abcdef"))
	if err := term.Resize(20, 3); err != nil {
		t.Fatal(err)
	}
	got, _ := term.Text(false)
	if got != "0123456789abcdef" {
		t.Errorf("after widening, Text = %q (reflow)", got)
	}
}

// text is a frame's rows as plain text, by row.
func text(f Frame) map[int]string {
	out := map[int]string{}
	for _, l := range f.Lines {
		s := ""
		for _, r := range l.Runs {
			s += r.Text
		}
		out[l.Y] = s
	}
	return out
}

func TestFrame(t *testing.T) {
	term := newTerm(t, 20, 4)
	term.Write([]byte("hi \x1b[1;32mworld\x1b[0m!\r\n日本\r\n"))
	f, err := term.Frame(true)
	if err != nil {
		t.Fatal(err)
	}
	if f.Cols != 20 || f.Rows != 4 || len(f.Lines) != 4 || !f.Cursor.Visible || f.Cursor.X != 0 || f.Cursor.Y != 2 {
		t.Fatalf("frame %+v", f)
	}
	runs := f.Lines[0].Runs
	if len(runs) != 3 || runs[0].Text != "hi " || runs[1].Text != "world" || runs[1].Attrs&Bold == 0 || runs[1].FG == DefaultColor || runs[1].X != 3 || runs[2].Text != "!" || runs[2].FG != DefaultColor {
		t.Errorf("row 0 runs %+v", runs)
	}
	wide := f.Lines[1].Runs
	if len(wide) != 1 || wide[0].Text != "日本" || wide[0].Cells != 4 {
		t.Errorf("row 1 runs %+v", wide)
	}
	if len(f.Lines[3].Runs) != 0 {
		t.Errorf("blank row has runs %+v", f.Lines[3].Runs)
	}
}

// Only what changed comes back; nothing when nothing did.
func TestFrameDirty(t *testing.T) {
	term := newTerm(t, 20, 4)
	term.Write([]byte("one\r\ntwo"))
	term.Frame(false)
	if f, _ := term.Frame(false); f.Dirty != Clean || len(f.Lines) != 0 {
		t.Errorf("second frame: %+v", f)
	}
	term.Write([]byte("\x1b[1;1Hzero"))
	f, _ := term.Frame(false)
	if got := text(f); got[0] != "zero" || len(f.Lines) > 2 {
		t.Errorf("after writing row 0: %v (%d rows)", got, len(f.Lines))
	}
}

// After scrolling, the frame is the visible screen, not the history.
func TestFrameScrolled(t *testing.T) {
	term := newTerm(t, 20, 5)
	for i := 1; i <= 20; i++ {
		term.Write([]byte(strconv.Itoa(i) + "\r\n"))
	}
	term.Write([]byte("$ "))
	f, _ := term.Frame(true)
	got := text(f)
	if got[0] != "17" || got[3] != "20" || got[4] != "$" || f.Cursor.Y != 4 || f.Cursor.X != 2 {
		t.Errorf("visible rows %v, cursor %+v", got, f.Cursor)
	}
}

// A buffer too small is grown, and the frame is still whole.
func TestFrameGrows(t *testing.T) {
	term := newTerm(t, 200, 60)
	line := strings.Repeat("\x1b[31mr\x1b[32mg", 100) + "\r\n"
	for i := 0; i < 60; i++ {
		term.Write([]byte(line))
	}
	term.buf = make([]byte, 128)
	f, err := term.Frame(true)
	if err != nil || len(f.Lines) != 60 || len(f.Lines[0].Runs) != 200 {
		t.Errorf("frame: %v, %d rows", err, len(f.Lines))
	}
}

// The engine knows when a program reads a password.
func TestFramePassword(t *testing.T) {
	term := newTerm(t, 20, 4)
	f, _ := term.Frame(true)
	if f.Password {
		t.Error("password input at start")
	}
}

// XKB keycodes, as GDK reports them on a US keyboard.
const (
	xkbA     = 38
	xkbC     = 54
	xkbX     = 53
	xkbEnter = 36
	xkbUp    = 111
)

// press is k pressed.
func press(k KeyEvent) KeyEvent {
	k.Action = Press
	return k
}

func key(t *testing.T, term *Terminal, k KeyEvent) string {
	t.Helper()
	b, err := term.Key(k)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestKeys(t *testing.T) {
	term := newTerm(t, 80, 24)
	for _, tc := range []struct {
		name string
		k    KeyEvent
		want string
	}{
		{"a", press(KeyEvent{Keycode: xkbA, Text: "a", Unshifted: 'a'}), "a"},
		{"Ctrl+C", press(KeyEvent{Keycode: xkbC, Mods: ModCtrl, Text: "c", Unshifted: 'c'}), "\x03"},
		{"Alt+x", press(KeyEvent{Keycode: xkbX, Mods: ModAlt, Text: "x", Unshifted: 'x'}), "\x1bx"},
		{"Enter", press(KeyEvent{Keycode: xkbEnter}), "\r"},
		{"Up", press(KeyEvent{Keycode: xkbUp}), "\x1b[A"},
		{"a released", KeyEvent{Keycode: xkbA, Action: Release, Text: "a", Unshifted: 'a'}, ""},
	} {
		if got := key(t, term, tc.k); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The keys follow the modes the program sets.
func TestKeyModes(t *testing.T) {
	term := newTerm(t, 80, 24)
	term.Write([]byte("\x1b[?1h")) // application cursor keys (vim, less)
	if got := key(t, term, press(KeyEvent{Keycode: xkbUp})); got != "\x1bOA" {
		t.Errorf("Up in application mode: %q", got)
	}
	// Without the kitty protocol, Shift+Enter is xterm's sequence, as
	// Ghostty sends it.
	if got := key(t, term, press(KeyEvent{Keycode: xkbEnter, Mods: ModShift})); got != "\x1b[27;2;13~" {
		t.Errorf("Shift+Enter, legacy: %q", got)
	}
	term.Write([]byte("\x1b[>1u")) // the kitty keyboard protocol, as agents ask
	if got := key(t, term, press(KeyEvent{Keycode: xkbEnter, Mods: ModShift})); got != "\x1b[13;2u" {
		t.Errorf("Shift+Enter, kitty: %q", got)
	}
	// Releases only for a program that asks for them.
	if got := key(t, term, KeyEvent{Keycode: xkbA, Action: Release, Text: "a", Unshifted: 'a'}); got != "" {
		t.Errorf("a released, without report events: %q", got)
	}
	term.Write([]byte("\x1b[>3u")) // disambiguate and report events
	if got := key(t, term, KeyEvent{Keycode: xkbA, Action: Release, Text: "a", Unshifted: 'a'}); got != "\x1b[97;1:3u" {
		t.Errorf("a released, with report events: %q", got)
	}
	term.Write([]byte("\x1b[<u\x1b[<u")) // and back, both pushes popped
	if got := key(t, term, press(KeyEvent{Keycode: xkbEnter, Mods: ModShift})); got != "\x1b[27;2;13~" {
		t.Errorf("Shift+Enter after the program left the protocol: %q", got)
	}
}

// A pointer event at cell x, y of a 10 by 20 pixel cell grid: a drag
// in the cell's right half, which Ghostty counts as reaching the cell.
func ptr(kind PointerKind, x, y int, at time.Duration) Pointer {
	px := x*10 + 5
	if kind == PointerDrag {
		px = x*10 + 8
	}
	return Pointer{Kind: kind, X: x, Y: y, PX: float64(px), PY: float64(y*20 + 10),
		Time: at, Repeat: 400 * time.Millisecond, RepeatPx: 5, CellW: 10, Height: 4 * 20}
}

func selected(t *testing.T, term *Terminal) string {
	t.Helper()
	s, err := term.SelectionText()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSelect(t *testing.T) {
	term := newTerm(t, 20, 4)
	term.Write([]byte("hello world\r\nsecond line"))
	term.Select(ptr(PointerPress, 0, 0, time.Second))
	if ok, _ := term.Select(ptr(PointerDrag, 4, 0, time.Second)); !ok {
		t.Fatal("dragging made no selection")
	}
	term.Select(ptr(PointerRelease, 4, 0, time.Second))
	if got := selected(t, term); got != "hello" {
		t.Errorf("dragged %q", got)
	}
	// The frame marks the cells.
	f, _ := term.Frame(true)
	if r := f.Lines[0].Runs[0]; r.Attrs&Selected == 0 || r.Text != "hello" {
		t.Errorf("first run %+v", r)
	}
	// A plain click clears it.
	term.Select(ptr(PointerPress, 8, 1, 3*time.Second))
	term.Select(ptr(PointerRelease, 8, 1, 3*time.Second))
	if got := selected(t, term); got != "" {
		t.Errorf("after a click: %q", got)
	}
}

func TestSelectClicks(t *testing.T) {
	term := newTerm(t, 20, 4)
	term.Write([]byte("hello world\r\nsecond line"))
	at := 10 * time.Second
	click := func(x, y int) {
		term.Select(ptr(PointerPress, x, y, at))
		term.Select(ptr(PointerRelease, x, y, at))
		at += 100 * time.Millisecond
	}
	click(7, 0)
	click(7, 0)
	if got := selected(t, term); got != "world" {
		t.Errorf("double click: %q", got)
	}
	click(7, 0)
	if got := selected(t, term); got != "hello world" {
		t.Errorf("triple click: %q", got)
	}
	// Clicks too far apart in time are single.
	at += time.Second
	click(2, 1)
	at += time.Second
	click(2, 1)
	if got := selected(t, term); got != "" {
		t.Errorf("slow clicks selected %q", got)
	}
}

// A line the terminal wrapped is copied as one.
func TestSelectWrapped(t *testing.T) {
	term := newTerm(t, 10, 4)
	term.Write([]byte("0123456789abcdef"))
	term.Select(ptr(PointerPress, 0, 0, time.Second))
	term.Select(ptr(PointerDrag, 5, 1, time.Second))
	if got := selected(t, term); got != "0123456789abcdef" {
		t.Errorf("wrapped selection %q", got)
	}
	term.ClearSelection()
	if got := selected(t, term); got != "" {
		t.Errorf("after clear %q", got)
	}
}

// Named keys go by their keysym, so a remapped key sends what it now is:
// with caps:escape, Caps Lock's keycode makes Escape.
func TestKeysym(t *testing.T) {
	term := newTerm(t, 80, 24)
	const xkbCaps, xkbEscape = 66, 9
	if got := key(t, term, press(KeyEvent{Keyval: 0xff1b, Keycode: xkbCaps})); got != "\x1b" {
		t.Errorf("Escape on the Caps Lock key: %q", got)
	}
	if got := key(t, term, press(KeyEvent{Keyval: 0xff0d, Keycode: xkbEscape})); got != "\r" {
		t.Errorf("Return on the Escape key's code: %q", got)
	}
	if got := key(t, term, press(KeyEvent{Keyval: 0xffbf})); got != "\x1bOQ" {
		t.Errorf("F2 by keysym alone: %q", got)
	}
}

func TestPaste(t *testing.T) {
	term := newTerm(t, 40, 5)
	paste := func(text string, force bool) (string, error) {
		t.Helper()
		b, err := term.Paste(text, false, force)
		return string(b), err
	}
	if got, err := paste("ls -l", false); err != nil || got != "ls -l" {
		t.Errorf("plain: %q, %v", got, err)
	}
	// A line break would be Enter: refused, then sent as Enter once agreed.
	if _, err := paste("rm -rf build\n", false); !errors.Is(err, ErrUnsafe) {
		t.Errorf("newline unbracketed: %v, want ErrUnsafe", err)
	}
	if got, err := paste("a\nb", true); err != nil || got != "a\rb" {
		t.Errorf("forced: %q, %v", got, err)
	}
	// Control bytes become spaces.
	if got, err := paste("a\x1b[31mb", false); err != nil || got != "a [31mb" {
		t.Errorf("escape: %q, %v", got, err)
	}
	if got, err := paste("", false); err != nil || got != "" {
		t.Errorf("empty: %q, %v", got, err)
	}

	// Bracketed paste: lines are safe, the bracket's end is not.
	term.Write([]byte("\x1b[?2004h"))
	if got, err := paste("a\nb", false); err != nil || got != "\x1b[200~a\nb\x1b[201~" {
		t.Errorf("bracketed: %q, %v", got, err)
	}
	if _, err := paste("a\x1b[201~b\n", false); !errors.Is(err, ErrUnsafe) {
		t.Errorf("bracket end: %v, want ErrUnsafe", err)
	}
	// Agreed, the bracket's end loses its escape and cannot end the paste.
	if got, err := paste("a\x1b[201~b", true); err != nil || got != "\x1b[200~a [201~b\x1b[201~" {
		t.Errorf("bracket end, forced: %q, %v", got, err)
	}
	term.Write([]byte("\x1b[?2004l"))
	if _, err := paste("a\nb", false); !errors.Is(err, ErrUnsafe) {
		t.Errorf("after 2004l: %v, want ErrUnsafe", err)
	}
}

func TestAnswers(t *testing.T) {
	term := newTerm(t, 80, 24)
	ask := func(q string) string {
		t.Helper()
		term.Write([]byte(q))
		return string(term.Answers())
	}
	if got := ask("\x1b[c"); got != "" {
		t.Errorf("answered before Answer: %q", got)
	}
	term.Answer()
	for _, c := range []struct{ q, want string }{
		{"\x1b[c", "\x1b[?62;22;52c"},
		{"\x1b[>c", "\x1b[>1;10;0c"},
		{"\x1b[5n", "\x1b[0n"},
		{"ab\x1b[6n", "\x1b[1;3R"},
		{"\x1b[?2004$p", "\x1b[?2004;2$y"},
		{"\x1b[18t", "\x1b[8;24;80t"},
		{"\x1b[?996n", ""}, // no colours yet
		{"plain text", ""},
	} {
		if got := ask(c.q); got != c.want {
			t.Errorf("%q: answered %q, want %q", c.q, got, c.want)
		}
	}
	if got := ask("\x1b[>q"); !strings.Contains(got, "inua-term") {
		t.Errorf("XTVERSION: %q", got)
	}
	term.SetColors(0xe0e0e0, 0x0b1012, 0x7fb4ca)
	if got := ask("\x1b[?996n"); got != "\x1b[?997;1n" {
		t.Errorf("scheme: %q, want dark", got)
	}
	if got := ask("\x1b]11;?\x1b\\"); !strings.Contains(got, "rgb:0b0b/1010/1212") {
		t.Errorf("OSC 11: %q", got)
	}
	term.SetCellSize(11, 24)
	if got := ask("\x1b[16t"); got != "\x1b[6;24;11t" {
		t.Errorf("cell size: %q", got)
	}
	// Two queries in one write: both answers, in order.
	if got := ask("\x1b[5n\x1b[c"); got != "\x1b[0n\x1b[?62;22;52c" {
		t.Errorf("two: %q", got)
	}
	// A paste in between keeps the answers apart.
	term.Write([]byte("\x1b[5n"))
	if b, err := term.Paste("x", false, false); err != nil || string(b) != "x" {
		t.Errorf("paste while answering: %q, %v", b, err)
	}
	if got := string(term.Answers()); got != "\x1b[0n" {
		t.Errorf("answers after paste: %q", got)
	}
}

func TestClipboardWrite(t *testing.T) {
	term := newTerm(t, 80, 24)
	term.Answer()
	if _, _, ok := term.Clipboard(); ok {
		t.Fatal("a write before any")
	}
	// OSC 52: base64 of "copied from ssh".
	term.Write([]byte("\x1b]52;c;Y29waWVkIGZyb20gc3No\x1b\\"))
	if text, primary, ok := term.Clipboard(); !ok || primary || text != "copied from ssh" {
		t.Errorf("clipboard: %q %v %v", text, primary, ok)
	}
	if _, _, ok := term.Clipboard(); ok {
		t.Error("taken twice")
	}
	term.Write([]byte("\x1b]52;p;cHJpbWFyeQ==\x07"))
	if text, primary, ok := term.Clipboard(); !ok || !primary || text != "primary" {
		t.Errorf("primary: %q %v %v", text, primary, ok)
	}
	// Reading is not offered: no answer.
	term.Write([]byte("\x1b]52;c;?\x1b\\"))
	if a := term.Answers(); len(a) != 0 {
		t.Errorf("a read answered: %q", a)
	}
}

func TestScroll(t *testing.T) {
	term := newTerm(t, 20, 4)
	for i := 1; i <= 10; i++ {
		term.Write([]byte("line " + strconv.Itoa(i) + "\r\n"))
	}
	f, err := term.Frame(true)
	if err != nil {
		t.Fatal(err)
	}
	if s := f.Scroll; s.Total != 11 || s.Len != 4 || !s.AtBottom() {
		t.Fatalf("at the bottom: %+v", s)
	}
	term.Scroll(ScrollRows, -3)
	f, _ = term.Frame(false)
	if got := text(f)[0]; got != "line 5" || f.Scroll.Offset != 4 || f.Scroll.AtBottom() {
		t.Errorf("3 up: first row %q, %+v", got, f.Scroll)
	}
	if f.Cursor.Visible {
		t.Error("the cursor shows while scrolled away from it")
	}
	// Output keeps a scrolled viewport where it is.
	term.Write([]byte("line 11\r\n"))
	f, _ = term.Frame(true)
	if got := text(f)[0]; got != "line 5" {
		t.Errorf("after output: first row %q", got)
	}
	term.Scroll(ScrollTop, 0)
	f, _ = term.Frame(false)
	if got := text(f)[0]; got != "line 1" || f.Scroll.Offset != 0 {
		t.Errorf("top: %q %+v", got, f.Scroll)
	}
	term.Scroll(ScrollBottom, 0)
	f, _ = term.Frame(false)
	if !f.Scroll.AtBottom() || !f.Cursor.Visible {
		t.Errorf("bottom: %+v, cursor %v", f.Scroll, f.Cursor.Visible)
	}
	term.Write([]byte("\x1b[?1049h"))
	f, _ = term.Frame(false)
	if !f.Alt {
		t.Error("alternate screen not reported")
	}
}
