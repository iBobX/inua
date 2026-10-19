// Package vt is inua-term's terminal engine: a program's output in, the
// screen out. It is libghostty-vt at a pinned commit, linked with inua's
// shim into libinua-vt.so (scripts/libghostty.sh) and loaded with purego
// on first use, so inua builds without cgo. Only the shim's calls are
// used; see shim/inua_vt.h.
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
	"sync"
	"unsafe"

	"codeberg.org/puregotk/purego"
)

// abi is the shim's INUA_VT_ABI this package is written for.
const abi = 2

// Library is the shim's file name.
const Library = "libinua-vt.so"

var lib struct {
	once sync.Once
	err  error

	abi       func() int32
	new       func(cols, rows uint16) uintptr
	free      func(t uintptr)
	write     func(t uintptr, data *byte, n uintptr)
	resize    func(t uintptr, cols, rows uint16) int32
	text      func(t uintptr, unwrap int32, out **byte, n *uintptr) int32
	freeBytes func(p *byte, n uintptr)
	title     func(t uintptr, p **byte, n *uintptr) int32
	pwd       func(t uintptr, p **byte, n *uintptr) int32
	cursor    func(t uintptr, x, y *uint16) int32
	frame     func(t uintptr, all int32, buf *byte, cap uintptr, n *uintptr, info *frameInfo) int32
}

// ErrMissing is returned when libinua-vt.so is not found.
var ErrMissing = errors.New("vt: " + Library + " not found (scripts/libghostty.sh builds it)")

// Paths are where the library is looked for, in order: $INUA_VT_LIB,
// beside the executable, then ../lib/inua beside it (an installed
// prefix).
func Paths() []string {
	var paths []string
	if p := os.Getenv("INUA_VT_LIB"); p != "" {
		paths = append(paths, p)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		paths = append(paths, filepath.Join(dir, Library), filepath.Join(dir, "..", "lib", "inua", Library))
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
		purego.RegisterLibFunc(&lib.freeBytes, h, "inua_vt_free_bytes")
		purego.RegisterLibFunc(&lib.title, h, "inua_vt_title")
		purego.RegisterLibFunc(&lib.pwd, h, "inua_vt_pwd")
		purego.RegisterLibFunc(&lib.cursor, h, "inua_vt_cursor")
		purego.RegisterLibFunc(&lib.frame, h, "inua_vt_frame")
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
	Lines    []Line
}

// Cursor is where the cursor is, in the viewport.
type Cursor struct {
	X, Y    int
	Visible bool
	Style   CursorStyle
	Wide    bool // on the second cell of a wide character
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
			Style: CursorStyle(info.cursorStyle), Wide: info.cursorWide != 0,
		},
		Password: info.password != 0, FG: info.fg, BG: info.bg,
		Lines: make([]Line, 0, info.nrows),
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
