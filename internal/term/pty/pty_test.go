package pty

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// output reads everything the program writes until it closes the terminal.
func output(t *testing.T, p *PTY) string {
	t.Helper()
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(p)
		done <- b
	}()
	select {
	case b := <-done:
		return string(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
	case <-time.After(5 * time.Second):
		t.Fatal("the program did not finish")
		return ""
	}
}

func TestStart(t *testing.T) {
	dir := t.TempDir()
	p, err := Start([]string{"sh", "-c", "stty size; pwd; echo $TERM; tty; exit 3"}, dir, append(os.Environ(), "TERM=xterm-256color"), 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	got := output(t, p)
	code, err := p.Wait()
	if err != nil || code != 3 {
		t.Errorf("exit %d, %v", code, err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 4 || lines[0] != "30 100" || lines[1] != dir || lines[2] != "xterm-256color" || !strings.HasPrefix(lines[3], "/dev/pts/") {
		t.Errorf("output %q", got)
	}
	p.Close()
}

// The program sees the new size (and gets SIGWINCH).
func TestResize(t *testing.T) {
	p, err := Start([]string{"sh", "-c", "trap 'stty size; exit' WINCH; echo ready; while :; do sleep 0.05; done"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	buf := make([]byte, 64)
	n, _ := p.Read(buf)
	if !strings.Contains(string(buf[:n]), "ready") {
		t.Fatalf("got %q", buf[:n])
	}
	if err := p.Resize(132, 43); err != nil {
		t.Fatal(err)
	}
	if got := output(t, p); !strings.Contains(got, "43 132") {
		t.Errorf("after resize: %q", got)
	}
	p.Wait()
}

// The foreground group is the shell's, then the program it runs; echo is
// off while a program reads a password.
func TestForegroundAndEcho(t *testing.T) {
	p, err := Start([]string{"sh", "-c", "echo shell; read x; stty -echo; echo hidden; read y"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	buf := make([]byte, 64)
	p.Read(buf)
	if fg, err := p.Foreground(); err != nil || fg != p.Pid() {
		t.Errorf("foreground %d (%v), want the shell %d", fg, err, p.Pid())
	}
	if on, err := p.Echoing(); err != nil || !on {
		t.Errorf("echoing %v (%v), want on", on, err)
	}
	p.Write([]byte("go\n"))
	deadline := time.Now().Add(3 * time.Second)
	for {
		on, err := p.Echoing()
		if err != nil {
			t.Fatal(err)
		}
		if !on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("echo never went off")
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.Write([]byte("secret\n"))
	p.Wait()
}
