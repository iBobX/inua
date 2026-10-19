package shellint

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/term/pty"
	"github.com/lyricat/inua/internal/term/vt"
)

func TestWrap(t *testing.T) {
	argv, env := Wrap([]string{"/bin/bash"}, []string{"A=1"}, "/d")
	if strings.Join(argv, " ") != "/bin/bash --rcfile /d/bash/inua.bash" {
		t.Errorf("bash %v", argv)
	}
	_, env = Wrap([]string{"zsh"}, []string{"ZDOTDIR=/home/me/.config/zsh"}, "/d")
	if v, _ := lookup(env, "ZDOTDIR"); v != "/d/zsh" {
		t.Errorf("zsh ZDOTDIR %q", v)
	}
	if v, _ := lookup(env, "INUA_TERM_ZDOTDIR"); v != "/home/me/.config/zsh" {
		t.Errorf("zsh's own ZDOTDIR %q", v)
	}
	_, env = Wrap([]string{"fish"}, nil, "/d")
	if v, _ := lookup(env, "XDG_DATA_DIRS"); v != "/d:/usr/local/share:/usr/share" {
		t.Errorf("fish %q", v)
	}
	if argv, _ := Wrap([]string{"bash", "-c", "x"}, nil, "/d"); len(argv) != 3 {
		t.Errorf("bash with arguments %v", argv)
	}
}

func TestInstallKeepsFiles(t *testing.T) {
	dir := t.TempDir()
	if err := Install(dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "bash", "inua.bash")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(p, old, old)
	if err := Install(dir); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); !fi.ModTime().Equal(old) {
		t.Error("an unchanged script was written again")
	}
}

// shell runs a shell with the integration in a clean home on a PTY, the
// engine reading what it draws.
type shell struct {
	t    *testing.T
	p    *pty.PTY
	term *vt.Terminal
	out  chan []byte
	// marks are those read and not yet waited for: several can come in
	// one read.
	marks []vt.Prompt
}

func start(t *testing.T, name string) *shell {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skip(name + " is not installed")
	}
	if os.Getenv("INUA_VT_LIB") == "" {
		if p, err := filepath.Abs(filepath.Join("..", "..", "..", "bin", vt.Library)); err == nil {
			os.Setenv("INUA_VT_LIB", p)
		}
	}
	term, err := vt.New(80, 24)
	if errors.Is(err, vt.ErrMissing) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	term.Answer()
	t.Cleanup(term.Close)
	dir, home := t.TempDir(), t.TempDir()
	if err := Install(dir); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "TERM=xterm-256color", "PS1=$ "}
	argv, env := Wrap([]string{path}, env, dir)
	if name == "zsh" {
		os.WriteFile(filepath.Join(home, ".zshrc"), []byte("PS1='%% '\n"), 0o600)
	}
	if name == "bash" {
		os.WriteFile(filepath.Join(home, ".bashrc"), []byte("PS1='$ '\n"), 0o600)
	}
	p, err := pty.Start(argv, home, env, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close(); p.Wait() })
	s := &shell{t: t, p: p, term: term, out: make(chan []byte, 64)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				s.out <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				close(s.out)
				return
			}
		}
	}()
	return s
}

// until feeds the engine until it saw a mark of kind; it returns every
// mark seen.
func (s *shell) until(kind vt.PromptKind) []vt.Prompt {
	s.t.Helper()
	var seen []vt.Prompt
	deadline := time.After(10 * time.Second)
	for {
		for i, p := range s.marks {
			if p.Kind == kind {
				seen = append(seen, s.marks[:i+1]...)
				s.marks = s.marks[i+1:]
				return seen
			}
		}
		seen = append(seen, s.marks...)
		s.marks = nil
		select {
		case b, ok := <-s.out:
			if !ok {
				s.t.Fatalf("the shell ended; marks %+v", seen)
			}
			s.term.Write(b)
			s.marks = append(s.marks, s.term.Prompts()...)
		case <-deadline:
			s.t.Fatalf("no mark %d; marks %+v", kind, seen)
		}
	}
}

func testShell(t *testing.T, name string) {
	s := start(t, name)
	s.until(vt.InputStart)
	s.p.Write([]byte("echo one two"))
	deadline := time.Now().Add(5 * time.Second)
	for s.term.Input() != "echo one two" {
		select {
		case b := <-s.out:
			s.term.Write(b)
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("typed %q", s.term.Input())
		}
	}
	s.p.Write([]byte("\r"))
	marks := s.until(vt.CommandStart)
	if c := marks[len(marks)-1].Command; c != "echo one two" {
		t.Errorf("command %q", c)
	}
	s.p.Write([]byte("false\r"))
	s.until(vt.CommandStart)
	marks = s.until(vt.CommandEnd)
	if e := marks[len(marks)-1]; !e.HasExit || e.Exit != 1 {
		t.Errorf("false's end %+v", e)
	}
	s.until(vt.InputStart)
	if pwd := s.term.Pwd(); !strings.HasPrefix(pwd, "file://") {
		t.Errorf("folder %q", pwd)
	}
}

func TestBash(t *testing.T) { testShell(t, "bash") }
func TestZsh(t *testing.T)  { testShell(t, "zsh") }
func TestFish(t *testing.T) { testShell(t, "fish") }
