package disk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
)

// Command is a rule's proper way to clean a folder: what it shows, and
// what inua runs for the user when it can.
type Command struct {
	Text string   // as the user would type it, the home as ~
	Args []string // what runs; the folder's path and the home in full
	Dir  string   // where it runs: the folder it cleans is in
	Root bool     // it runs as root, through pkexec, which asks for a password
	Not  string   // why inua cannot run it, in words; "" when it can
}

// LookPath finds a program; a test replaces it.
var LookPath = exec.LookPath

// CleanCommand is the clean command of rule r for the folder at path; a
// zero Command when the rule has none.
func CleanCommand(r *Rule, path, home string) Command {
	if r == nil || r.Clean == "" {
		return Command{}
	}
	c := Command{Text: strings.ReplaceAll(r.Clean, "PATH", Tilde(path, home)), Dir: filepath.Dir(path)}
	for _, f := range strings.Fields(r.Clean) {
		f = strings.ReplaceAll(f, "PATH", path)
		if rest, ok := strings.CutPrefix(f, "~/"); ok && home != "" {
			f = filepath.Join(home, rest)
		}
		c.Args = append(c.Args, f)
	}
	if c.Args[0] == "sudo" {
		c.Args, c.Root = append([]string{"pkexec"}, c.Args[1:]...), true
	}
	for _, f := range c.Args {
		if f == "NAME" || f == "MODEL" || strings.HasSuffix(f, "/NAME") {
			c.Not = i18n.T("Fill in the name it asks for, and run it in a terminal.")
			return c
		}
	}
	if r.Terminal {
		c.Not = i18n.T("It asks before it deletes: run it in a terminal.")
		return c
	}
	prog := c.Args[0]
	if c.Root {
		prog = c.Args[1]
	}
	if strings.Contains(prog, "/") {
		if _, err := os.Stat(filepath.Join(c.Dir, prog)); err != nil {
			c.Not = i18n.Tf("%s is not there.", prog)
		}
	} else if _, err := LookPath(prog); err != nil {
		c.Not = i18n.Tf("%s is not installed.", prog)
	} else if c.Root {
		if _, err := LookPath("pkexec"); err != nil {
			c.Not = i18n.T("It needs root, and pkexec is not installed: run it in a terminal with sudo.")
		}
	}
	return c
}

// outMax is how much of a command's output Run keeps: its end.
const outMax = 4000

// Run runs c in its folder and returns the end of what it printed. A
// failure's error says how it ended; the output says why.
func Run(ctx context.Context, c Command) (string, error) {
	if c.Not != "" || len(c.Args) == 0 {
		return "", errors.New(c.Not)
	}
	cmd := exec.CommandContext(ctx, c.Args[0], c.Args[1:]...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Stdin = nil // what asks a question gets no answer, and stops
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	b := bytes.TrimSpace(out.Bytes())
	if len(b) > outMax {
		b = append([]byte("…"), b[len(b)-outMax:]...)
	}
	return string(b), err
}

// Rescan scans n again, as after a command cleaned it; nil when it is no
// longer there. Replace puts what it found in n's place.
func Rescan(ctx context.Context, n *Node, home string) (*Node, error) {
	fresh, err := Scan(ctx, n.Path(), Options{Home: home}, nil)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return fresh, err
}

// Replace puts fresh, a new scan of n, in n's place; nil takes n out.
func (n *Node) Replace(fresh *Node) {
	if fresh == nil {
		n.Remove()
		return
	}
	ds, df := fresh.Size-n.Size, fresh.Files-n.Files
	n.Size, n.Files, n.Mod, n.Denied, n.Kids = fresh.Size, fresh.Files, fresh.Mod, fresh.Denied, fresh.Kids
	for _, k := range n.Kids {
		k.Parent = n
	}
	for a := n.Parent; a != nil; a = a.Parent {
		a.Size += ds
		a.Files += df
		sortKids(a)
	}
}
