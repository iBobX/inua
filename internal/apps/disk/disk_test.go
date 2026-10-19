package disk

import (
	"context"
	"errors"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tree makes files under dir: a path ending in / is a folder, else a file
// of the size given.
func tree(t *testing.T, dir string, files map[string]int) {
	t.Helper()
	for p, size := range files {
		full := filepath.Join(dir, p)
		if strings.HasSuffix(p, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanSizesFoldsAndRules(t *testing.T) {
	home := t.TempDir()
	tree(t, home, map[string]int{
		"proj/Cargo.toml":                  10,
		"proj/target/debug/big":            3 << 20,
		"proj/src/main.rs":                 100,
		"web/package.json":                 10,
		"web/node_modules/a/index.js":      2 << 20,
		"web/node_modules/a/node_modules/": 0,
		"notes/a.txt":                      100,
		"notes/b.txt":                      100,
		"lone/target/x":                    100, // no Cargo.toml: not Rust's
		"shared-target/CACHEDIR.TAG":       43,  // CARGO_TARGET_DIR elsewhere
		"proj/target/debug/CACHEDIR.TAG":   43,  // inside a known folder: its own
		".cargo/registry/cache/x":          100,
		".claude/projects/p/1.jsonl":       2 << 20,
		".claude/settings.json":            10,
		".cache/go-build/00/x":             100,
		".cache/thumbnails/y":              100,
	})
	root, err := Scan(context.Background(), home, Options{Home: home}, nil)
	if err != nil {
		t.Fatal(err)
	}
	proj := root.Find(filepath.Join(home, "proj"))
	target := root.Find(filepath.Join(home, "proj/target"))
	if proj == nil || target == nil {
		t.Fatal("proj/target not found")
	}
	if target.Rule == nil || target.Rule.What != "Rust build" {
		t.Errorf("target's rule = %v, want Rust build", target.Rule)
	}
	if target.Size < 3<<20 || proj.Size < target.Size {
		t.Errorf("sizes: proj %d, target %d", proj.Size, target.Size)
	}
	if proj.Kids[0] != target {
		t.Errorf("the largest is not first: %s", proj.Kids[0].Name)
	}
	if r := root.Find(filepath.Join(home, "lone/target")).Rule; r != nil {
		t.Errorf("a target without Cargo.toml has rule %q", r.What)
	}
	if r := root.Find(filepath.Join(home, "shared-target")).Rule; r == nil || r.Kind != KindCache || r.Verdict != Safe {
		t.Errorf("a folder holding CACHEDIR.TAG: rule %v", r)
	}
	if r := root.Find(filepath.Join(home, "proj/target/debug")).Rule; r != nil {
		t.Errorf("a tagged folder inside Rust's build has its own rule %q", r.What)
	}
	notes := root.Find(filepath.Join(home, "notes"))
	if len(notes.Kids) != 1 || !notes.Kids[0].Rest || notes.Kids[0].Files != 2 {
		t.Errorf("small files are not counted together: %+v", notes.Kids)
	}
	if notes.Kids[0].Path() != notes.Path() {
		t.Errorf("the small files' path = %s", notes.Kids[0].Path())
	}
	if r := root.Find(filepath.Join(home, ".cache/go-build")).Rule; r == nil || r.What != "Go build cache" {
		t.Errorf(".cache/go-build's rule = %v", r)
	}
	if r := root.Find(filepath.Join(home, ".cache/thumbnails")).Rule; r == nil || r.Home != ".cache/*" {
		t.Errorf(".cache/thumbnails's rule = %v, want a program's cache", r)
	}
	if root.Files != 15 {
		t.Errorf("files = %d, want 15", root.Files)
	}

	// The developer's: the outermost known folders, each once.
	var dev []string
	for _, n := range Found(root, KindDev) {
		dev = append(dev, n.Path())
	}
	want := []string{filepath.Join(home, "proj/target"), filepath.Join(home, "web/node_modules"), filepath.Join(home, ".cache/go-build"), filepath.Join(home, ".cargo/registry")}
	if !slices.Equal(dev, want) {
		t.Errorf("developer's = %v, want %v", dev, want)
	}
	// The agents': Claude Code's conversations, not .claude itself.
	agent := Found(root, KindAgent)
	if len(agent) != 1 || agent[0].Path() != filepath.Join(home, ".claude/projects") {
		t.Errorf("agents' = %v", agent)
	}

	// Removing takes the size from every folder it was in.
	before, size := root.Size, target.Size
	target.Remove()
	if root.Size != before-size || root.Find(target.Path()) != nil {
		t.Errorf("after Remove: root %d, want %d", root.Size, before-size)
	}
}

func TestScanReportsTops(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]int{"a/f": 2 << 20, "b/f": 3 << 20, "Cargo.toml": 1, "target/x": 1 << 20})
	var p Progress
	if _, err := Scan(context.Background(), dir, Options{}, &p); err != nil {
		t.Fatal(err)
	}
	tops := p.Tops()
	if len(tops) != 3 || tops[0].Name != "b" || tops[1].Name != "a" || tops[2].Kind != KindDev {
		t.Errorf("tops = %+v", tops)
	}
	arcs := Scanning(tops, p.Bytes.Load(), 1, [4]color.NRGBA{}, rgba(1, 1, 1), rgba(2, 2, 2))
	rings := map[int]int{}
	for _, a := range arcs {
		rings[a.Ring]++
	}
	if rings[1] != 3 || rings[2] != 7 {
		t.Errorf("scanning arcs by ring = %v", rings)
	}
	if u := Used(250, 1000, rgba(3, 3, 3), rgba(4, 4, 4)); len(u) != 2 || math.Abs(u[0].A1-math.Pi/2) > 1e-9 {
		t.Errorf("used = %+v", u)
	}
}

func TestScanCountsHardLinksOnce(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]int{"a/f": 2 << 20})
	os.MkdirAll(filepath.Join(dir, "b"), 0o755)
	if err := os.Link(filepath.Join(dir, "a/f"), filepath.Join(dir, "b/f")); err != nil {
		t.Skip(err)
	}
	root, err := Scan(context.Background(), dir, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b := root.Find(filepath.Join(dir, "a")), root.Find(filepath.Join(dir, "b"))
	if a.Size+b.Size > 3<<20 {
		t.Errorf("a hard link counted twice: %d + %d", a.Size, b.Size)
	}
}

func TestScanStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Scan(ctx, t.TempDir(), Options{}, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want canceled", err)
	}
}

func TestFoundGroupWithoutKnownParts(t *testing.T) {
	home := t.TempDir()
	tree(t, home, map[string]int{".codex/config.toml": 10})
	root, err := Scan(context.Background(), home, Options{Home: home}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := Found(root, KindAgent)
	if len(got) != 1 || got[0].Name != ".codex" {
		t.Errorf("found = %v, want .codex itself", got)
	}
}

// fakeGit answers as a repository at repo would, tracking tracked and
// ignoring ignored (paths relative to it).
func fakeGit(t *testing.T, repo string, tracked, ignored []string) {
	old := Git
	t.Cleanup(func() { Git = old })
	Git = func(_ context.Context, dir string, args ...string) (string, error) {
		if !strings.HasPrefix(dir+"/", repo+"/") {
			return "", errors.New("not a git repository")
		}
		rel := args[len(args)-1]
		switch args[0] {
		case "rev-parse":
			return repo + "\n", nil
		case "ls-files":
			if slices.Contains(tracked, rel) {
				return rel + "\x00", nil
			}
			return "", nil
		case "check-ignore":
			if slices.Contains(ignored, rel) {
				return "", nil
			}
			return "", errors.New("exit 1")
		case "status":
			return " M main.go\n", nil
		case "log", "remote":
			return "", nil
		}
		return "", errors.New("unexpected " + args[0])
	}
}

func TestDecide(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "proj")
	tree(t, home, map[string]int{
		"proj/package.json":      10,
		"proj/node_modules/x":    10,
		"proj/dist/app.js":       10,
		"proj/scratch/a":         10,
		"proj/out/a":             10,
		"downloads-old/file.iso": 10,
		".claude/projects/p/a":   10,
	})
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	fakeGit(t, repo, []string{"dist"}, []string{"node_modules", "out"})
	ctx := context.Background()
	for _, c := range []struct {
		path string
		want Verdict
		has  string // in the last step's finding
	}{
		{"/usr/lib", Keep, "system"},
		{home, Keep, "home"},
		{filepath.Join(home, ".ssh"), Keep, ""},
		{filepath.Join(home, ".config"), Keep, ""},
		{filepath.Join(repo, "node_modules"), Safe, "npm packages"},
		{filepath.Join(repo, "dist"), Keep, "part of the project"},
		{filepath.Join(repo, ".git"), Keep, "history"},
		{filepath.Join(repo, "out"), Unknown, "No rule"},
		{filepath.Join(repo, "scratch"), Unknown, "No rule"},
		{repo, Unknown, "No rule"},
		{filepath.Join(home, "downloads-old"), Unknown, "No rule"},
		{filepath.Join(home, ".claude/projects"), Review, "Claude Code"},
	} {
		r := Decide(ctx, c.path, home)
		last := r.Steps[len(r.Steps)-1]
		if r.Verdict != c.want || !strings.Contains(last.Found, c.has) {
			t.Errorf("%s: %v (%q), want %v with %q", c.path, r.Verdict, last.Found, c.want, c.has)
		}
	}
	// What the route learnt goes to the assistant.
	r := Decide(ctx, filepath.Join(repo, "out"), home)
	if !r.Ignored || r.Repo != repo {
		t.Errorf("out: repo %q ignored %v", r.Repo, r.Ignored)
	}
	r = Decide(ctx, repo, home)
	if !strings.Contains(r.Steps[2].Found, "with 1 change not committed: they") {
		t.Errorf("the repository's state: %q", r.Steps[2].Found)
	}
}

func TestDecideNotYours(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root may delete anything")
	}
	dir := t.TempDir()
	tree(t, dir, map[string]int{"ro/x/f": 1})
	os.Chmod(filepath.Join(dir, "ro"), 0o555)
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "ro"), 0o755) })
	fakeGit(t, "/nowhere", nil, nil)
	r := Decide(context.Background(), filepath.Join(dir, "ro/x"), dir)
	if r.Verdict != Keep || r.Steps[len(r.Steps)-1].Check != "Yours to delete?" {
		t.Errorf("a folder in one not the user's: %v %+v", r.Verdict, r.Steps)
	}
}

func TestFactsAndPrompts(t *testing.T) {
	home := "/home/u"
	n := &Node{Name: "cache", Dir: true, Size: 3_000_000_000, Files: 120, path: "/home/u/.local/cache"}
	n.Kids = []*Node{
		{Name: "models", Dir: true, Size: 2_000_000_000, Parent: n},
		{Rest: true, Size: 1000, Files: 40, Parent: n},
	}
	r := Route{Steps: []Step{{Check: "Kept by git?", Found: "No."}}}
	f := Facts(n, r, home)
	for _, want := range []string{"path: ~/.local/cache", "size on disk: 3 GB", "models/  2 GB", "(40 smaller files)", "check: Kept by git? No."} {
		if !strings.Contains(f, want) {
			t.Errorf("facts lack %q:\n%s", want, f)
		}
	}
	if p := ExplainPrompt(f, "ja"); !strings.Contains(p, `"ja"`) || !strings.HasSuffix(p, f) {
		t.Errorf("prompt: %s", p)
	}
	if p := DecidePrompt(f, ""); !strings.Contains(p, "SAFE, REVIEW or KEEP") || !strings.Contains(p, `"en"`) {
		t.Errorf("decide prompt: %s", p)
	}
	for text, want := range map[string]Verdict{
		"SAFE\nGo makes it again.": Safe, "**Review**\nIt holds notes.": Review, "keep.": Keep, "Maybe": Unknown,
	} {
		v, _, ok := ParseDecision(text)
		if v != want || ok != (want != Unknown) {
			t.Errorf("ParseDecision(%q) = %v %v, want %v", text, v, ok, want)
		}
	}
	if _, why, _ := ParseDecision("SAFE\n  Go makes it again.  "); why != "Go makes it again." {
		t.Errorf("reason = %q", why)
	}
	if len(ChooseOptions) != len(Verdicts) {
		t.Error("an option without its verdict")
	}
}

func TestLayoutAndHit(t *testing.T) {
	root := &Node{Name: "r", Dir: true, Size: 100}
	a := &Node{Name: "a", Dir: true, Size: 75, Parent: root}
	b := &Node{Name: "b", Size: 25, Parent: root}
	a1 := &Node{Name: "a1", Size: 75, Parent: a}
	a.Kids, root.Kids = []*Node{a1}, []*Node{a, b}
	arcs := Layout(root)
	if len(arcs) != 3 {
		t.Fatalf("arcs = %d, want 3", len(arcs))
	}
	if arcs[0].Node != a || math.Abs(arcs[0].A1-1.5*math.Pi) > 1e-9 || arcs[1].Node != a1 || arcs[1].Ring != 2 || arcs[1].Top != a {
		t.Errorf("layout: %+v", arcs)
	}
	g := Measure(200, 200)
	// Right of the centre, in the first ring: a quarter turn, in a.
	if i, mid := Hit(arcs, g, g.CX+g.Middle+g.Ring/2, g.CY); mid || i < 0 || arcs[i].Node != a {
		t.Errorf("hit right = %d %v", i, mid)
	}
	// Left of it: three quarters and more, in b.
	if i, _ := Hit(arcs, g, g.CX-g.Middle-g.Ring/2, g.CY-1); i < 0 || arcs[i].Node != b {
		t.Errorf("hit left = %d", i)
	}
	if _, mid := Hit(arcs, g, g.CX, g.CY); !mid {
		t.Error("the middle is not hit")
	}
	img := Render(arcs, 200, 200, 1, Paint{Plain: rgba(1, 2, 3), Middle: rgba(9, 9, 9)})
	if c := img.NRGBAAt(int(g.CX+g.Middle+g.Ring/2), int(g.CY)); c != rgba(1, 2, 3) {
		t.Errorf("a slice's pixel = %v", c)
	}
	if c := img.NRGBAAt(0, 0); c.A != 0 {
		t.Errorf("a corner is painted: %v", c)
	}
}

func TestSizeAndMounts(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 999: "999 B", 1500: "1.5 kB", 32_000_000_000: "32 GB", 12_300_000: "12 MB"} {
		if got := Size(n); got != want {
			t.Errorf("Size(%d) = %q, want %q", n, got, want)
		}
	}
	if got := unescapeMount(`/run/media/u/My\040Disk`); got != "/run/media/u/My Disk" {
		t.Errorf("unescape = %q", got)
	}
	if Tilde("/home/u/x", "/home/u") != "~/x" || Tilde("/home/uv", "/home/u") != "/home/uv" {
		t.Error("Tilde")
	}
}

func rgba(r, g, b uint8) color.NRGBA { return color.NRGBA{r, g, b, 255} }

func TestCleanCommand(t *testing.T) {
	old := LookPath
	t.Cleanup(func() { LookPath = old })
	installed := map[string]bool{"go": true, "paccache": true, "pkexec": true, "rm": true}
	LookPath = func(p string) (string, error) {
		if installed[p] {
			return "/usr/bin/" + p, nil
		}
		return "", errors.New("not found")
	}
	home := "/home/u"
	for _, c := range []struct {
		clean, path string
		terminal    bool
		args        string
		root        bool
		not         string
	}{
		{"go clean -cache", "/home/u/.cache/go-build", false, "go clean -cache", false, ""},
		{"sudo paccache -r", "/var/cache/pacman/pkg", false, "pkexec paccache -r", true, ""},
		{"rm -rf ~/.npm/_npx", "/home/u/.npm/_npx", false, "rm -rf /home/u/.npm/_npx", false, ""},
		{"git worktree remove PATH/NAME", "/home/u/p/.claude/worktrees", false, "git worktree remove /home/u/p/.claude/worktrees/NAME", false, "name"},
		{"ollama rm MODEL", "/home/u/.ollama/models", false, "ollama rm MODEL", false, "name"},
		{"docker system prune", "/var/lib/docker", true, "docker system prune", false, "terminal"},
		{"cargo clean", "/home/u/p/target", false, "cargo clean", false, "cargo is not installed"},
	} {
		got := CleanCommand(&Rule{Clean: c.clean, Terminal: c.terminal}, c.path, home)
		if strings.Join(got.Args, " ") != c.args || got.Root != c.root || got.Dir != filepath.Dir(c.path) {
			t.Errorf("%s: %+v", c.clean, got)
		}
		if (got.Not == "") != (c.not == "") || c.not != "" && c.not != "name" && c.not != "terminal" && !strings.Contains(got.Not, c.not) {
			t.Errorf("%s: why not %q, want %q", c.clean, got.Not, c.not)
		}
	}
	if got := CleanCommand(&Rule{Clean: "git worktree remove PATH/NAME"}, "/home/u/p/.claude/worktrees", home); got.Text != "git worktree remove ~/p/.claude/worktrees/NAME" {
		t.Errorf("text = %q", got.Text)
	}
	if got := CleanCommand(&Rule{}, "/x", home); got.Text != "" || got.Args != nil {
		t.Errorf("no command: %+v", got)
	}
}

func TestRunAndReplace(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]int{"cache/a": 2 << 20, "cache/b": 2 << 20, "keep/x": 2 << 20})
	root, err := Scan(context.Background(), dir, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cache := root.Find(filepath.Join(dir, "cache"))
	out, err := Run(context.Background(), Command{Args: []string{"sh", "-c", "rm a; echo cleaned"}, Dir: cache.Path()})
	if err != nil || out != "cleaned" {
		t.Fatalf("run: %q %v", out, err)
	}
	before := root.Size
	fresh, err := Rescan(context.Background(), cache, "")
	if err != nil {
		t.Fatal(err)
	}
	freed := cache.Size - fresh.Size
	cache.Replace(fresh)
	if freed < 2<<20 || root.Size != before-freed || cache.Kids[0].Parent != cache {
		t.Errorf("after the command: freed %d, root %d from %d", freed, root.Size, before)
	}
	if _, err := Run(context.Background(), Command{Args: []string{"sh", "-c", "echo no >&2; exit 3"}, Dir: dir}); err == nil {
		t.Error("a failing command reports no error")
	}
	if _, err := Run(context.Background(), Command{Args: []string{"x"}, Not: "cannot"}); err == nil || err.Error() != "cannot" {
		t.Errorf("a command inua cannot run: %v", err)
	}
	os.RemoveAll(cache.Path())
	fresh, err = Rescan(context.Background(), cache, "")
	if fresh != nil || err != nil {
		t.Fatalf("a folder gone: %v %v", fresh, err)
	}
	cache.Replace(nil)
	if root.Find(filepath.Join(dir, "cache")) != nil {
		t.Error("a folder gone is still in the tree")
	}
}
