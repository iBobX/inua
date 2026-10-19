package vt

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
