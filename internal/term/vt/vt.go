// Package vt is inua-term's terminal engine: a program's output in, the
// screen out. It is libghostty-vt at a pinned commit, linked with inua's
// shim into libinua-vt.so (libinua-vt.dylib on macOS;
// scripts/libghostty.sh), and loaded with purego on first use, so inua
// builds without cgo. Only the shim's calls are used; see shim/inua_vt.h.
//
// A Terminal is not safe for concurrent use; its owner serializes calls.
package vt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"codeberg.org/puregotk/purego"
)

// abi is the shim's INUA_VT_ABI this package is written for.
const abi = 18

var lib struct {
	once sync.Once
	err  error

	abi         func() int32
	new         func(cols, rows uint16) uintptr
	free        func(t uintptr)
	write       func(t uintptr, data *byte, n uintptr)
	resize      func(t uintptr, cols, rows uint16) int32
	text        func(t uintptr, unwrap int32, out **byte, n *uintptr) int32
	snapshot    func(t uintptr, out **byte, n *uintptr) int32
	freeBytes   func(p *byte, n uintptr)
	title       func(t uintptr, p **byte, n *uintptr) int32
	pwd         func(t uintptr, p **byte, n *uintptr) int32
	cursor      func(t uintptr, x, y *uint16) int32
	frame       func(t uintptr, all int32, buf *byte, cap uintptr, n *uintptr, info *frameInfo) int32
	key         func(t uintptr, keysym, xkb, mods, action uint32, utf8 *byte, n uintptr, unshifted, consumed uint32, out *byte, cap uintptr, outN *uintptr) int32
	sel         func(t uintptr, kind uint32, x, y uint16, px, py float64, timeNs, repeatNs uint64, repeatPx float64, rect, cellW, height uint32) int32
	selText     func(t uintptr, out **byte, n *uintptr) int32
	selClear    func(t uintptr)
	answer      func(t uintptr)
	answers     func(t uintptr, p **byte, n *uintptr)
	colors      func(t uintptr, fg, bg, cursor uint32)
	cellSize    func(t uintptr, w, h uint32)
	clipboard   func(t uintptr, p **byte, n *uintptr) int32
	prompts     func(t uintptr, p **byte, n *uintptr)
	input       func(t uintptr, out **byte, n *uintptr) int32
	tail        func(t uintptr, n uint16, out **byte, outN *uintptr) int32
	notes       func(t uintptr, p **byte, n *uintptr)
	defaults    func(t uintptr, scrollback, cursor, blink uint32)
	linkAt      func(t uintptr, x, y uint16, out **byte, n *uintptr, x0, x1 *uint16) int32
	palette     func(t uintptr, colors *uint32)
	scroll      func(t uintptr, kind uint32, n int32)
	search      func(t uintptr, needle *byte, n uintptr) int32
	searchMove  func(t uintptr, dir int32) int32
	searchCount func(t uintptr, total, current *uint32)
	mouse       func(t uintptr, action, button, mods uint32, x, y float64, w, h, cw, ch, pad, pressed uint32, out *byte, cap uintptr, n *uintptr) int32
	paste       func(t uintptr, data *byte, n uintptr, primary, force uint32, out **byte, outN *uintptr) int32
}

// ErrMissing is returned when the Library is not found.
var ErrMissing = errors.New("vt: " + Library + " not found (scripts/libghostty.sh builds it)")

// Paths are where the library is looked for, in order: $INUA_VT_LIB,
// beside the executable, ../lib/inua beside it (an installed prefix),
// then lib/inua beside it (a release's archive, unpacked and run where
// it is).
func Paths() []string {
	var paths []string
	if p := os.Getenv("INUA_VT_LIB"); p != "" {
		paths = append(paths, p)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		paths = append(paths,
			filepath.Join(dir, Library),
			filepath.Join(dir, "..", "lib", "inua", Library),
			filepath.Join(dir, "lib", "inua", Library))
	}
	return paths
}

// Load opens the library once; every Terminal needs it.
func Load() error {
	lib.once.Do(func() {
		var h uintptr
		for _, p := range Paths() {
			if _, err := os.Stat(p); err != nil {
				continue
			}
			var err error
			if h, err = purego.Dlopen(p, purego.RTLD_NOW|purego.RTLD_LOCAL); err != nil {
				lib.err = fmt.Errorf("vt: %s: %w", p, err)
				return
			}
			break
		}
		if h == 0 {
			lib.err = ErrMissing
			return
		}
		purego.RegisterLibFunc(&lib.abi, h, "inua_vt_abi")
		if got := lib.abi(); got != abi {
			lib.err = fmt.Errorf("vt: %s has ABI %d, inua-term needs %d: rebuild it with scripts/libghostty.sh", Library, got, abi)
			return
		}
		purego.RegisterLibFunc(&lib.new, h, "inua_vt_new")
		purego.RegisterLibFunc(&lib.free, h, "inua_vt_free")
		purego.RegisterLibFunc(&lib.write, h, "inua_vt_write")
		purego.RegisterLibFunc(&lib.resize, h, "inua_vt_resize")
		purego.RegisterLibFunc(&lib.text, h, "inua_vt_text")
		purego.RegisterLibFunc(&lib.snapshot, h, "inua_vt_snapshot")
		purego.RegisterLibFunc(&lib.freeBytes, h, "inua_vt_free_bytes")
		purego.RegisterLibFunc(&lib.title, h, "inua_vt_title")
		purego.RegisterLibFunc(&lib.pwd, h, "inua_vt_pwd")
		purego.RegisterLibFunc(&lib.cursor, h, "inua_vt_cursor")
		purego.RegisterLibFunc(&lib.frame, h, "inua_vt_frame")
		purego.RegisterLibFunc(&lib.key, h, "inua_vt_key")
		purego.RegisterLibFunc(&lib.sel, h, "inua_vt_select")
		purego.RegisterLibFunc(&lib.selText, h, "inua_vt_selection_text")
		purego.RegisterLibFunc(&lib.selClear, h, "inua_vt_select_clear")
		purego.RegisterLibFunc(&lib.paste, h, "inua_vt_paste")
		purego.RegisterLibFunc(&lib.answer, h, "inua_vt_answer")
		purego.RegisterLibFunc(&lib.answers, h, "inua_vt_answers")
		purego.RegisterLibFunc(&lib.colors, h, "inua_vt_colors")
		purego.RegisterLibFunc(&lib.cellSize, h, "inua_vt_cell_size")
		purego.RegisterLibFunc(&lib.clipboard, h, "inua_vt_clipboard")
		purego.RegisterLibFunc(&lib.prompts, h, "inua_vt_prompts")
		purego.RegisterLibFunc(&lib.input, h, "inua_vt_input")
		purego.RegisterLibFunc(&lib.tail, h, "inua_vt_tail")
		purego.RegisterLibFunc(&lib.notes, h, "inua_vt_notifications")
		purego.RegisterLibFunc(&lib.defaults, h, "inua_vt_defaults")
		purego.RegisterLibFunc(&lib.linkAt, h, "inua_vt_link_at")
		purego.RegisterLibFunc(&lib.palette, h, "inua_vt_palette")
		purego.RegisterLibFunc(&lib.scroll, h, "inua_vt_scroll")
		purego.RegisterLibFunc(&lib.mouse, h, "inua_vt_mouse")
		purego.RegisterLibFunc(&lib.search, h, "inua_vt_search")
		purego.RegisterLibFunc(&lib.searchMove, h, "inua_vt_search_move")
		purego.RegisterLibFunc(&lib.searchCount, h, "inua_vt_search_count")
	})
	return lib.err
}

// Terminal is one terminal's state.
type Terminal struct {
	h   uintptr
	buf []byte // Frame's, kept between frames
}

// New is a terminal of cols × rows.
func New(cols, rows int) (*Terminal, error) {
	if err := Load(); err != nil {
		return nil, err
	}
	h := lib.new(uint16(cols), uint16(rows))
	if h == 0 {
		return nil, errors.New("vt: cannot make a terminal")
	}
	t := &Terminal{h: h}
	runtime.SetFinalizer(t, (*Terminal).Close)
	return t, nil
}

// Close frees the terminal; it may be called more than once.
func (t *Terminal) Close() {
	if t.h != 0 {
		lib.free(t.h)
		t.h = 0
	}
	runtime.SetFinalizer(t, nil)
}

// Write feeds the program's output.
func (t *Terminal) Write(p []byte) (int, error) {
	if len(p) > 0 {
		lib.write(t.h, &p[0], uintptr(len(p)))
		runtime.KeepAlive(p)
	}
	return len(p), nil
}

// Search looks for needle in the screen and the scrollback (ASCII
// letters in any case); "" ends the search. Frames mark the matches in
// view (Match, and MatchCurrent for the one selected).
func (t *Terminal) Search(needle string) error {
	var p *byte
	if needle != "" {
		b := []byte(needle)
		p = &b[0]
		defer runtime.KeepAlive(b)
	}
	return result("search", lib.search(t.h, p, uintptr(len(needle))))
}

// SearchMove selects the next match, toward older lines (dir 1), or the
// previous (-1), and scrolls to it; false when there is none.
func (t *Terminal) SearchMove(dir int) bool { return lib.searchMove(t.h, int32(dir)) == 1 }

// SearchCount is how many matches there are, and which one is selected,
// from 1 (0 for none).
func (t *Terminal) SearchCount() (total, current int) {
	var n, i uint32
	lib.searchCount(t.h, &n, &i)
	return int(n), int(i)
}

// ScrollKind is where Scroll moves the viewport.
type ScrollKind uint32

const (
	ScrollTop     ScrollKind = iota // the scrollback's first row
	ScrollBottom                    // where the program writes
	ScrollRows                      // by n rows, up negative
	ScrollPrompts                   // by n of the shell's prompts (OSC 133)
)

// Scroll moves the viewport through the scrollback; the next frame shows
// the rows there. Output keeps it in place unless it is at the bottom.
func (t *Terminal) Scroll(kind ScrollKind, n int) { lib.scroll(t.h, uint32(kind), int32(n)) }

// Answer makes the terminal answer the program's queries (device
// attributes, cursor position, modes, colours, sizes); Answers takes what
// it would send back. One engine per program answers: the host's.
func (t *Terminal) Answer() { lib.answer(t.h) }

// Answers is what the terminal answered to the queries in what was
// written since the last call, for the program; nil for nothing.
func (t *Terminal) Answers() []byte {
	var p *byte
	var n uintptr
	lib.answers(t.h, &p, &n)
	if p == nil || n == 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(p, n)...)
}

// Clipboard is the text the program last put on a clipboard (OSC 52 and
// the like) in what was written since the last call, for a terminal that
// answers; ok is false for none, primary says it was the primary
// selection.
func (t *Terminal) Clipboard() (text string, primary, ok bool) {
	var p *byte
	var n uintptr
	at := lib.clipboard(t.h, &p, &n)
	if at == 0 {
		return "", false, false
	}
	if p != nil && n > 0 {
		text = string(unsafe.Slice(p, n))
	}
	return text, at == 2, true
}

// PromptKind is a step of a command the shell reports (OSC 133).
type PromptKind int

const (
	PromptStart  PromptKind = 1 // the shell draws its prompt
	InputStart   PromptKind = 2 // the user can type
	CommandStart PromptKind = 3 // a command runs
	CommandEnd   PromptKind = 4 // and ended
)

// Prompt is a prompt mark: for a CommandStart its command line, for a
// CommandEnd its exit code when the shell sent one.
type Prompt struct {
	Kind    PromptKind
	Command string
	Exit    int
	HasExit bool
}

// Prompts are the shell's prompt marks in what was written since the
// last call, for a terminal that answers.
func (t *Terminal) Prompts() []Prompt {
	var p *byte
	var n uintptr
	lib.prompts(t.h, &p, &n)
	if p == nil || n == 0 {
		return nil
	}
	b := unsafe.Slice(p, n)
	var out []Prompt
	for len(b) >= 8 {
		l := int(binary.LittleEndian.Uint16(b[6:]))
		if len(b) < 8+l {
			break
		}
		out = append(out, Prompt{
			Kind: PromptKind(b[0]), HasExit: b[1] != 0,
			Exit:    int(int32(binary.LittleEndian.Uint32(b[2:]))),
			Command: string(b[8 : 8+l]),
		})
		b = b[8+l:]
	}
	return out
}

// Input is what is typed at the shell's prompt and not yet run, "" when
// the cursor is not at a prompt (OSC 133 marks it).
func (t *Terminal) Input() string {
	var p *byte
	var n uintptr
	if lib.input(t.h, &p, &n) != 0 || p == nil {
		return ""
	}
	s := string(unsafe.Slice(p, n))
	lib.freeBytes(p, n)
	return s
}

// Link is a link on the screen: its URI and the cells it spans on its
// row of the viewport, X0 to X1.
type Link struct {
	URI    string
	X0, X1 int
}

// LinkAt is the link at a cell of the viewport: an OSC 8 hyperlink, else
// a URL in the text; ok is false for none.
func (t *Terminal) LinkAt(x, y int) (Link, bool) {
	var p *byte
	var n uintptr
	var x0, x1 uint16
	if lib.linkAt(t.h, uint16(x), uint16(y), &p, &n, &x0, &x1) != 1 || p == nil {
		return Link{}, false
	}
	l := Link{URI: string(unsafe.Slice(p, n)), X0: int(x0), X1: int(x1)}
	lib.freeBytes(p, n)
	return l, true
}

// SetDefaults sets the scrollback's length in lines and the cursor a
// program gets when it resets it (CursorBar, CursorBlock or
// CursorUnderline), blinking or not.
func (t *Terminal) SetDefaults(scrollback int, cursor CursorStyle, blink bool) {
	lib.defaults(t.h, uint32(max(scrollback, 0)), uint32(cursor), flag(blink))
}

// Notification is a desktop notification a program sent (OSC 9, 777).
type Notification struct{ Title, Body string }

// Notifications are the notifications the program sent since the last
// call, for a terminal that answers.
func (t *Terminal) Notifications() []Notification {
	var p *byte
	var n uintptr
	lib.notes(t.h, &p, &n)
	if p == nil || n == 0 {
		return nil
	}
	b := unsafe.Slice(p, n)
	var out []Notification
	str := func() (string, bool) {
		if len(b) < 2 {
			return "", false
		}
		l := int(binary.LittleEndian.Uint16(b))
		if len(b) < 2+l {
			return "", false
		}
		s := string(b[2 : 2+l])
		b = b[2+l:]
		return s, true
	}
	for len(b) > 0 {
		title, ok1 := str()
		body, ok2 := str()
		if !ok1 || !ok2 {
			break
		}
		out = append(out, Notification{title, body})
	}
	return out
}

// Tail is the last n rows of the screen, a line each: what a program
// shows at its bottom.
func (t *Terminal) Tail(n int) []string {
	var p *byte
	var l uintptr
	if lib.tail(t.h, uint16(n), &p, &l) != 0 || p == nil {
		return nil
	}
	s := string(unsafe.Slice(p, l))
	lib.freeBytes(p, l)
	return strings.Split(s, "\n")
}

// SetColors sets the default colours (0xRRGGBB) programs are told when
// they ask, and the scheme (dark or light) the background makes.
func (t *Terminal) SetColors(fg, bg, cursor uint32) { lib.colors(t.h, fg, bg, cursor) }

// SetPalette sets the palette's 16 colours (0xRRGGBB); the 240 others
// stay xterm's.
func (t *Terminal) SetPalette(colors [16]uint32) {
	lib.palette(t.h, &colors[0])
	runtime.KeepAlive(&colors)
}

// SetCellSize sets a cell's size in pixels, for size reports.
func (t *Terminal) SetCellSize(w, h int) { lib.cellSize(t.h, uint32(w), uint32(h)) }

// Resize changes the size in cells.
func (t *Terminal) Resize(cols, rows int) error {
	return result("resize", lib.resize(t.h, uint16(cols), uint16(rows)))
}

// Text is the screen as plain text, trailing blanks trimmed; unwrap joins
// soft-wrapped lines.
func (t *Terminal) Text(unwrap bool) (string, error) {
	var p *byte
	var n uintptr
	u := int32(0)
	if unwrap {
		u = 1
	}
	if err := result("text", lib.text(t.h, u, &p, &n)); err != nil {
		return "", err
	}
	if p == nil {
		return "", nil
	}
	s := string(unsafe.Slice(p, n))
	lib.freeBytes(p, n)
	return s, nil
}

// Snapshot is the terminal as the bytes that draw it again in a terminal
// of the same size: its modes, the scrollback and the screen, the
// scrolling region, the keyboard's modes and the cursor. A client
// attaching takes it in place of the program's output since it started.
func (t *Terminal) Snapshot() ([]byte, error) {
	var p *byte
	var n uintptr
	if err := result("snapshot", lib.snapshot(t.h, &p, &n)); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	b := append([]byte(nil), unsafe.Slice(p, n)...)
	lib.freeBytes(p, n)
	return b, nil
}

// Title is the title the program set (OSC 0 or 2), "" for none.
func (t *Terminal) Title() string { return t.borrowed(lib.title) }

// Pwd is the folder the shell reported (OSC 7), as it sent it: a
// file:// URL, "" for none.
func (t *Terminal) Pwd() string { return t.borrowed(lib.pwd) }

func (t *Terminal) borrowed(get func(uintptr, **byte, *uintptr) int32) string {
	var p *byte
	var n uintptr
	if get(t.h, &p, &n) != 0 || p == nil || n == 0 {
		return ""
	}
	return string(unsafe.Slice(p, n)) // a copy: the bytes are borrowed
}

// Cursor is the cursor's column and row, from 0.
func (t *Terminal) Cursor() (x, y int) {
	var cx, cy uint16
	if lib.cursor(t.h, &cx, &cy) != 0 {
		return 0, 0
	}
	return int(cx), int(cy)
}

// result turns a GhosttyResult into an error.
func result(op string, r int32) error {
	if r == 0 {
		return nil
	}
	return fmt.Errorf("vt: %s: ghostty result %d", op, r)
}

// frameInfo mirrors the shim's inua_vt_frame_info: 32-bit fields only.
type frameInfo struct {
	cols, rows, dirty               uint32
	cursorVisible, cursorX, cursorY uint32
	cursorStyle, cursorWide         uint32
	password, fg, bg, nrows         uint32
	mouse, alt                      uint32
	sbTotal, sbOffset, sbLen        uint32
	cursorBlink                     uint32
}

// Dirty says how much of the screen changed since the last frame.
type Dirty int

const (
	Clean Dirty = iota
	Partial
	Full
)

// CursorStyle is how the cursor is drawn.
type CursorStyle int

const (
	CursorBar CursorStyle = iota
	CursorBlock
	CursorUnderline
	CursorHollow
)

// Attributes of a run.
const (
	Bold = 1 << iota
	Italic
	Faint
	Inverse
	Strike
	Underline
	Invisible
	Overline
	Selected
	Match        // a search match
	MatchCurrent // the search match selected
)

// DefaultColor in a run's FG or BG means the terminal's own colour.
const DefaultColor = 0x01000000

// Frame is what to draw: the size, the cursor, the default colours and
// the rows that changed (or all of them).
type Frame struct {
	Cols, Rows int
	Dirty      Dirty
	Cursor     Cursor
	// Password is set while the program reads a password.
	Password bool
	FG, BG   uint32 // 0xRRGGBB
	// Mouse is set while the program tracks the mouse (vim, htop).
	Mouse bool
	// Alt is set while the alternate screen shows (vim, less, htop).
	Alt bool
	// Scroll is where the viewport is in the scrollback.
	Scroll Scrollbar
	Lines  []Line
}

// Scrollbar is the scrollable area in rows: Total of them, the viewport
// Len long from Offset. At the bottom, Offset+Len == Total.
type Scrollbar struct{ Total, Offset, Len int }

// AtBottom says the viewport shows where the program writes.
func (s Scrollbar) AtBottom() bool { return s.Offset+s.Len >= s.Total }

// Cursor is where the cursor is, in the viewport.
type Cursor struct {
	X, Y    int
	Visible bool
	Style   CursorStyle
	Wide    bool // on the second cell of a wide character
	Blink   bool // it should blink
}

// Line is one row of the viewport, as runs of text in one style.
type Line struct {
	Y    int
	Runs []Run
}

// Run is text of one style: from column X, Cells wide (a wide character
// counts two), in FG and BG (0xRRGGBB, or DefaultColor), with Attrs.
type Run struct {
	X, Cells int
	FG, BG   uint32
	Attrs    uint16
	Text     string
}

// Frame updates the terminal's frame and returns the rows to draw: every
// row when all is set or the whole screen changed, else the changed ones.
// They are clean afterwards.
func (t *Terminal) Frame(all bool) (Frame, error) {
	a := int32(0)
	if all {
		a = 1
	}
	var info frameInfo
	for {
		if len(t.buf) == 0 {
			t.buf = make([]byte, 64*1024)
		}
		var n uintptr
		r := lib.frame(t.h, a, &t.buf[0], uintptr(len(t.buf)), &n, &info)
		if r == -3 { // out of space: retry larger; nothing was marked clean
			t.buf = make([]byte, n)
			continue
		}
		if err := result("frame", r); err != nil {
			return Frame{}, err
		}
		return decodeFrame(&info, t.buf[:n])
	}
}

func decodeFrame(info *frameInfo, b []byte) (Frame, error) {
	f := Frame{
		Cols: int(info.cols), Rows: int(info.rows), Dirty: Dirty(info.dirty),
		Cursor: Cursor{
			X: int(info.cursorX), Y: int(info.cursorY), Visible: info.cursorVisible != 0,
			Style: CursorStyle(info.cursorStyle), Wide: info.cursorWide != 0, Blink: info.cursorBlink != 0,
		},
		Password: info.password != 0, FG: info.fg, BG: info.bg, Mouse: info.mouse != 0, Alt: info.alt != 0,
		Scroll: Scrollbar{Total: int(info.sbTotal), Offset: int(info.sbOffset), Len: int(info.sbLen)},
		Lines:  make([]Line, 0, info.nrows),
	}
	le := binary.LittleEndian
	short := errors.New("vt: frame: short buffer")
	for i := uint32(0); i < info.nrows; i++ {
		if len(b) < 4 {
			return f, short
		}
		line := Line{Y: int(le.Uint16(b))}
		runs := int(le.Uint16(b[2:]))
		b = b[4:]
		line.Runs = make([]Run, 0, runs)
		for j := 0; j < runs; j++ {
			if len(b) < 16 {
				return f, short
			}
			r := Run{
				X: int(le.Uint16(b)), Cells: int(le.Uint16(b[2:])),
				FG: le.Uint32(b[4:]), BG: le.Uint32(b[8:]), Attrs: le.Uint16(b[12:]),
			}
			n := int(le.Uint16(b[14:]))
			b = b[16:]
			if len(b) < n {
				return f, short
			}
			r.Text = string(b[:n])
			b = b[n:]
			line.Runs = append(line.Runs, r)
		}
		f.Lines = append(f.Lines, line)
	}
	return f, nil
}

// Modifiers of a key, as libghostty has them.
const (
	ModShift = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
	ModCaps
	ModNum
)

// KeyAction is what a key did.
type KeyAction uint32

const (
	Release KeyAction = iota
	Press
	Repeat
)

// KeyEvent is a key as the window saw it: its keysym after the layout
// (which names Enter, Escape, the arrows, the function and keypad keys,
// whatever key makes them), the XKB keycode GDK reports (for the other
// keys), the modifiers held, the text it made, its character without modifiers
// (0 for none) and the modifiers the layout used up making the text.
type KeyEvent struct {
	Keyval    uint32
	Keycode   uint32
	Mods      uint32
	Action    KeyAction
	Text      string
	Unshifted rune
	Consumed  uint32
}

// Key is what to send the program for a key, in the modes it set (cursor
// keys, keypad, the kitty keyboard protocol); nil for nothing.
func (t *Terminal) Key(k KeyEvent) ([]byte, error) {
	var out [128]byte
	var n uintptr
	var text *byte
	if k.Text != "" {
		b := []byte(k.Text)
		text = &b[0]
		defer runtime.KeepAlive(b)
	}
	r := lib.key(t.h, k.Keyval, k.Keycode, k.Mods, uint32(k.Action), text, uintptr(len(k.Text)), uint32(k.Unshifted), k.Consumed, &out[0], uintptr(len(out)), &n)
	if err := result("key", r); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	return append([]byte(nil), out[:n]...), nil
}

// MouseAction is what the mouse did, for a program tracking it.
type MouseAction uint32

const (
	MousePress MouseAction = iota
	MouseRelease
	MouseMotion
)

// Mouse buttons; 4 and 5 are the wheel up and down.
const (
	ButtonNone   = 0
	ButtonLeft   = 1
	ButtonRight  = 2
	ButtonMiddle = 3
	WheelUp      = 4
	WheelDown    = 5
)

// MouseEvent is the pointer for a program tracking the mouse: X, Y in
// pixels from the widget's corner, the widget Width × Height with Pad
// around its cells of CellW × CellH, Pressed while a button is held.
type MouseEvent struct {
	Action        MouseAction
	Button        int
	Mods          uint32
	X, Y          float64
	Width, Height int
	CellW, CellH  int
	Pad           int
	Pressed       bool
}

// Mouse is what to send a program tracking the mouse, in its mode and
// format; nil for nothing.
func (t *Terminal) Mouse(m MouseEvent) ([]byte, error) {
	var out [64]byte
	var n uintptr
	r := lib.mouse(t.h, uint32(m.Action), uint32(m.Button), m.Mods, m.X, m.Y, uint32(m.Width), uint32(m.Height),
		uint32(m.CellW), uint32(m.CellH), uint32(m.Pad), flag(m.Pressed), &out[0], uintptr(len(out)), &n)
	if err := result("mouse", r); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	return append([]byte(nil), out[:n]...), nil
}

// PointerKind is what the pointer did, for selecting.
type PointerKind uint32

const (
	PointerPress PointerKind = iota
	PointerRelease
	PointerDrag
)

// Pointer is a pointer event for selecting: the cell in the viewport, the
// position in pixels from the cells' corner, when (for telling double
// and triple clicks: no more than Repeat apart, RepeatPx away), whether
// the selection is a block, and the cells' width and the viewport's
// height in pixels.
type Pointer struct {
	Kind          PointerKind
	X, Y          int
	PX, PY        float64
	Time, Repeat  time.Duration
	RepeatPx      float64
	Rectangle     bool
	CellW, Height int
}

// Select applies a pointer event through Ghostty's gesture (drag for a
// range, double click a word, triple a line); the selection becomes the
// terminal's, marked in frames. It reports whether there is one.
func (t *Terminal) Select(p Pointer) (bool, error) {
	rect := uint32(0)
	if p.Rectangle {
		rect = 1
	}
	r := lib.sel(t.h, uint32(p.Kind), uint16(p.X), uint16(p.Y), p.PX, p.PY, uint64(p.Time), uint64(p.Repeat), p.RepeatPx, rect, uint32(p.CellW), uint32(p.Height))
	if r < 0 {
		return false, result("select", r)
	}
	return r == 1, nil
}

// SelectionText is the selected text, wrapped lines joined and trailing
// blanks trimmed; "" without a selection.
func (t *Terminal) SelectionText() (string, error) {
	var p *byte
	var n uintptr
	if err := result("selection", lib.selText(t.h, &p, &n)); err != nil {
		return "", err
	}
	if p == nil {
		return "", nil
	}
	s := string(unsafe.Slice(p, n))
	lib.freeBytes(p, n)
	return s, nil
}

// ClearSelection removes the selection.
func (t *Terminal) ClearSelection() { lib.selClear(t.h) }

// ErrUnsafe is Paste's answer for text that could run commands: a line
// break the program would take as Enter, or the end of a bracketed paste
// inside one. Paste it with force set once the user agreed.
var ErrUnsafe = errors.New("vt: the text could run commands")

// Paste is what to send the program for text the user pastes, in the
// modes it set: inside bracketed paste when it asked for it, control
// bytes made spaces; nil for nothing. primary says it came from the
// primary selection. Without force, text that could run commands is
// refused with ErrUnsafe.
func (t *Terminal) Paste(text string, primary, force bool) ([]byte, error) {
	if text == "" {
		return nil, nil
	}
	b := []byte(text)
	var p *byte
	var n uintptr
	r := lib.paste(t.h, &b[0], uintptr(len(b)), flag(primary), flag(force), &p, &n)
	runtime.KeepAlive(b)
	if r == -7 {
		return nil, ErrUnsafe
	}
	if err := result("paste", r); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	out := append([]byte(nil), unsafe.Slice(p, n)...)
	lib.freeBytes(p, n)
	return out, nil
}

func flag(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}
