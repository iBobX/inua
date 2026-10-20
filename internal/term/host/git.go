package host

import (
	"os"
	"path/filepath"
	"strings"
)

// Git is the checkout a pane's folder is in, read from its files: no git
// runs. The zero Git is no checkout.
type Git struct {
	// Root is the checkout's top folder; Branch its branch, or a detached
	// HEAD's commit, short.
	Root   string `json:"root"`
	Branch string `json:"branch,omitempty"`
	// Worktree is set for a linked worktree (git worktree add); Repo is
	// then the main checkout's folder.
	Worktree bool   `json:"worktree,omitempty"`
	Repo     string `json:"repo,omitempty"`
}

// readGit is the checkout dir is in: the nearest .git above it, a folder
// or (a worktree's, a submodule's) a file naming one.
func readGit(dir string) Git {
	if !filepath.IsAbs(dir) {
		return Git{}
	}
	for d := filepath.Clean(dir); ; {
		dotgit := filepath.Join(d, ".git")
		if fi, err := os.Stat(dotgit); err == nil {
			return checkout(d, dotgit, fi.IsDir())
		}
		up := filepath.Dir(d)
		if up == d {
			return Git{}
		}
		d = up
	}
}

// checkout reads the checkout at root, whose .git is dotgit.
func checkout(root, dotgit string, isDir bool) Git {
	gitdir := dotgit
	if !isDir {
		b, err := os.ReadFile(dotgit)
		s, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
		if err != nil || !ok {
			return Git{}
		}
		gitdir = abs(root, strings.TrimSpace(s))
	}
	head, err := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	if err != nil {
		return Git{}
	}
	g := Git{Root: root, Branch: branchOf(string(head))}
	// A linked worktree's git folder names the repository's (a
	// submodule's does not).
	if c, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		common := abs(gitdir, strings.TrimSpace(string(c)))
		g.Worktree, g.Repo = true, filepath.Dir(common)
	}
	return g
}

// abs is path, relative to dir when it is not absolute.
func abs(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(dir, path)
}

// branchOf is what a HEAD file says: the branch, else the commit, short.
func branchOf(head string) string {
	head = strings.TrimSpace(head)
	if ref, ok := strings.CutPrefix(head, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		if b, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			return b
		}
		return strings.TrimPrefix(ref, "refs/")
	}
	if len(head) > 7 {
		return head[:7]
	}
	return head
}

// lookGit reads the pane's checkout again and tells the clients when it
// changed. p.mu is held.
func (p *pane) lookGit() {
	if g := readGit(p.folder()); g != p.git {
		p.git = g
		p.host.tell(Message{Type: MsgEvent, Pane: p.id, Git: &g})
	}
}

// gitInfo is the pane's checkout for a PaneInfo, nil for none.
func (p *pane) gitInfo() *Git {
	if p.git.Root == "" {
		return nil
	}
	g := p.git
	return &g
}
