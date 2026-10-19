// Package disk is Disk Usage without GTK (docs/disk-usage.md): a folder's
// size on disk, measured to the file, and what the folders in it are for;
// the developer's and the agents' folders it knows (Rules), whether one can
// go (Decide), what is asked of the assistant about one, and deleting it.
package disk

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
)

// Fold is the size under which a folder's files are counted together, as
// one node, rather than kept one by one: a home holds millions of small
// files, and only the large ones are worth seeing apart.
const Fold = 1 << 20

// Node is a file or a folder, with the size of everything in it.
type Node struct {
	Name   string
	Size   int64 // bytes on disk: blocks, each hard link once
	Files  int64 // files in it; 1 for a file
	Mod    int64 // the newest change in it, Unix seconds
	Dir    bool
	Rest   bool // its parent's small files, counted together
	Denied bool // a folder that could not be read: its size is short
	Rule   *Rule
	Parent *Node
	Kids   []*Node // largest first

	path string // the root's, for Path
}

// Path is where the node is.
func (n *Node) Path() string {
	if n.Parent == nil {
		return n.path
	}
	if n.Rest {
		return n.Parent.Path()
	}
	return filepath.Join(n.Parent.Path(), n.Name)
}

// Root is the node the scan began at.
func (n *Node) Root() *Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}

// Find is the node at path under n, or nil.
func (n *Node) Find(path string) *Node {
	rel, err := filepath.Rel(n.Path(), filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil
	}
	if rel == "." {
		return n
	}
	at := n
	for _, name := range strings.Split(rel, "/") {
		i := slices.IndexFunc(at.Kids, func(k *Node) bool { return !k.Rest && k.Name == name })
		if i < 0 {
			return nil
		}
		at = at.Kids[i]
	}
	return at
}

// Remove takes n out of the tree, its size from every folder it was in.
func (n *Node) Remove() {
	p := n.Parent
	if p == nil {
		return
	}
	p.Kids = slices.DeleteFunc(p.Kids, func(k *Node) bool { return k == n })
	for a := p; a != nil; a = a.Parent {
		a.Size -= n.Size
		a.Files -= n.Files
	}
	for a := p; a != nil; a = a.Parent {
		sortKids(a)
	}
}

// Progress is how far a scan has come; its fields are read while it runs.
type Progress struct {
	Files, Bytes atomic.Int64
	at           atomic.Pointer[string]
	mu           sync.Mutex
	tops         []Top
}

// Top is a folder in the root measured to the end, while the scan goes on.
type Top struct {
	Name string
	Size int64
	Kind Kind
}

// Tops are the folders in the root measured so far, largest first.
func (p *Progress) Tops() []Top {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.tops)
}

func (p *Progress) measured(n *Node) {
	t := Top{Name: n.Name, Size: n.Size}
	if n.Rule != nil {
		t.Kind = n.Rule.Kind
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	i, _ := slices.BinarySearchFunc(p.tops, t, func(a, b Top) int { return cmp.Compare(b.Size, a.Size) })
	p.tops = slices.Insert(p.tops, i, t)
}

// At is a folder the scan is reading.
func (p *Progress) At() string {
	if s := p.at.Load(); s != nil {
		return *s
	}
	return ""
}

// Options shape a scan.
type Options struct {
	// Home is the user's home, for the rules that name a place in it; ""
	// for the process's.
	Home string
	// AllDevices crosses into other filesystems mounted below the root;
	// by default a scan stays on the root's, as du -x does.
	AllDevices bool
}

// Scan measures root and everything under it. It stops when ctx ends,
// returning ctx's error.
func Scan(ctx context.Context, root string, o Options, p *Progress) (*Node, error) {
	root = filepath.Clean(root)
	if o.Home == "" {
		o.Home, _ = os.UserHomeDir()
	}
	if p == nil {
		p = &Progress{}
	}
	fi, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	st, _ := fi.Sys().(*syscall.Stat_t)
	s := &scan{ctx: ctx, o: o, p: p, links: map[[2]uint64]bool{}, sem: make(chan struct{}, 4*runtime.NumCPU())}
	if st != nil {
		s.dev = st.Dev
	}
	n := &Node{Name: filepath.Base(root), path: root, Dir: fi.IsDir(), Mod: fi.ModTime().Unix()}
	if !n.Dir {
		n.Size, n.Files = blocks(fi), 1
		return n, nil
	}
	if _, err := os.ReadDir(root); err != nil {
		return nil, err
	}
	n.Rule = Match(root, o.Home)
	s.dir(n, root)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return n, nil
}

type scan struct {
	ctx   context.Context
	o     Options
	p     *Progress
	dev   uint64
	sem   chan struct{}
	mu    sync.Mutex
	links map[[2]uint64]bool // files with more than one link, seen
}

// dir reads the folder n at path and everything under it, then sums it.
func (s *scan) dir(n *Node, path string) {
	if s.ctx.Err() != nil {
		return
	}
	s.p.at.Store(&path)
	entries, err := os.ReadDir(path)
	n.Denied = err != nil
	var rest *Node
	var wg sync.WaitGroup
	beside := make(map[string]bool, len(entries))
	for _, e := range entries {
		beside[e.Name()] = true
	}
	if n.Rule == nil && beside["CACHEDIR.TAG"] && !insideRule(n) {
		n.Rule = tagged(n.Name)
	}
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		st, _ := fi.Sys().(*syscall.Stat_t)
		if fi.IsDir() {
			if st != nil && !s.o.AllDevices && st.Dev != s.dev {
				continue // another filesystem mounted here
			}
			kpath := filepath.Join(path, e.Name())
			k := &Node{Name: e.Name(), Dir: true, Parent: n, Size: blocks(fi), Mod: fi.ModTime().Unix()}
			k.Rule = match(kpath, k.Name, s.o.Home, beside)
			n.Kids = append(n.Kids, k)
			select {
			case s.sem <- struct{}{}:
				wg.Add(1)
				go func() {
					defer func() { <-s.sem; wg.Done() }()
					s.dir(k, kpath)
					if n.Parent == nil {
						s.p.measured(k)
					}
				}()
			default:
				s.dir(k, kpath)
				if n.Parent == nil {
					s.p.measured(k)
				}
			}
			continue
		}
		size := blocks(fi)
		if st != nil && st.Nlink > 1 {
			key := [2]uint64{st.Dev, st.Ino}
			s.mu.Lock()
			seen := s.links[key]
			s.links[key] = true
			s.mu.Unlock()
			if seen {
				size = 0
			}
		}
		s.p.Files.Add(1)
		s.p.Bytes.Add(size)
		mod := fi.ModTime().Unix()
		if size < Fold {
			if rest == nil {
				rest = &Node{Rest: true, Parent: n}
			}
			rest.Size += size
			rest.Files++
			rest.Mod = max(rest.Mod, mod)
			continue
		}
		n.Kids = append(n.Kids, &Node{Name: e.Name(), Size: size, Files: 1, Mod: mod, Parent: n})
	}
	wg.Wait()
	if rest != nil {
		n.Kids = append(n.Kids, rest)
	}
	for _, k := range n.Kids {
		n.Size += k.Size
		n.Files += k.Files
		n.Mod = max(n.Mod, k.Mod)
	}
	sortKids(n)
}

// insideRule reports whether a folder n is in has a rule: what is in it is
// that folder's.
func insideRule(n *Node) bool {
	for a := n.Parent; a != nil; a = a.Parent {
		if a.Rule != nil && !a.Rule.Group {
			return true
		}
	}
	return false
}

func sortKids(n *Node) {
	slices.SortStableFunc(n.Kids, func(a, b *Node) int {
		switch {
		case a.Rest != b.Rest: // the small files last
			if a.Rest {
				return 1
			}
			return -1
		case a.Size > b.Size:
			return -1
		case a.Size < b.Size:
			return 1
		}
		return 0
	})
}

// blocks is what a file takes on disk.
func blocks(fi os.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return fi.Size()
}

// Found are the folders under n a rule knows, of kind k (KindNone for
// every kind), largest first: each the outermost one, so none is counted
// twice; a group (an agent's own folder) gives its known parts instead,
// and itself when it has none.
func Found(n *Node, k Kind) []*Node {
	var out []*Node
	var walk func(n *Node) bool
	walk = func(n *Node) bool {
		if r := n.Rule; r != nil && (k == KindNone || r.Kind == k) && !r.Group {
			out = append(out, n)
			return true
		}
		if r := n.Rule; r != nil && r.Group && (k == KindNone || r.Kind == k) {
			before := len(out)
			for _, c := range n.Kids {
				walk(c)
			}
			if len(out) == before {
				out = append(out, n)
			}
			return true
		}
		if n.Rule != nil && !n.Rule.Group {
			return false // another kind's: what is in it is that kind's
		}
		any := false
		for _, c := range n.Kids {
			if c.Dir && walk(c) {
				any = true
			}
		}
		return any
	}
	walk(n)
	slices.SortStableFunc(out, func(a, b *Node) int {
		switch {
		case a.Size > b.Size:
			return -1
		case a.Size < b.Size:
			return 1
		}
		return 0
	})
	return out
}

// Total is the size of nodes, and of those a rule says are safe to delete.
func Total(nodes []*Node) (all, safe int64) {
	for _, n := range nodes {
		all += n.Size
		if n.Rule != nil && n.Rule.Verdict == Safe {
			safe += n.Size
		}
	}
	return all, safe
}
