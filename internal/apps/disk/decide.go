package disk

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/lyricat/inua/internal/i18n"
)

// Step is one check on the route to a verdict: what was asked, what was
// found, and the verdict when this check settles it.
type Step struct {
	Check   string
	Found   string
	Verdict Verdict // Unknown: the route goes on
}

// Route is how Decide came to its verdict, check by check. A route that
// ends Unknown is for the assistant to settle (Choose).
type Route struct {
	Steps   []Step
	Verdict Verdict
	Rule    *Rule  // the rule that knows the folder, if one does
	Clean   string // the proper way to clean it, when there is one
	Repo    string // the git work tree it is in, if any
	Ignored bool   // ignored by that work tree
}

func (r *Route) add(check, found string, v Verdict) bool {
	r.Steps = append(r.Steps, Step{check, found, v})
	if v != Unknown {
		r.Verdict = v
		return true
	}
	return false
}

// Git runs git in dir and returns what it printed; a test replaces it.
var Git = func(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	out, err := cmd.Output()
	return string(out), err
}

// Decide finds whether path can be deleted, from the rules alone: the
// system's places and the user's own are kept; what the user cannot delete
// is kept; what git keeps is kept; then a rule that knows the folder
// settles it. When none does, the route ends Unknown, with what was
// learnt on the way for the assistant (Facts).
func Decide(ctx context.Context, path, home string) Route {
	path = filepath.Clean(path)
	var r Route
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	tilde := Tilde(path, home)

	// 1. The system's, or the user's own.
	if why := protected(path, home); why != "" {
		r.add(i18n.T("Part of the system, or one of your main folders?"), why, Keep)
		return r
	}
	r.add(i18n.T("Part of the system, or one of your main folders?"), i18n.T("No."), Unknown)

	r.Rule = Match(path, home)
	if r.Rule != nil {
		r.Clean = strings.ReplaceAll(r.Rule.Clean, "PATH", tilde)
	}

	// 2. Whether this user can delete it at all.
	if unix.Access(filepath.Dir(path), unix.W_OK) != nil {
		found := i18n.T("No: the folder it is in is not yours, so only root can delete it.")
		if r.Clean != "" {
			found += " " + i18n.T("Clean it with the command below instead.")
		}
		r.add(i18n.T("Yours to delete?"), found, Keep)
		return r
	}
	r.add(i18n.T("Yours to delete?"), i18n.T("Yes."), Unknown)

	// 3. Git: a project's history, its tracked files, a repository.
	if filepath.Base(path) == ".git" {
		r.add(i18n.T("Kept by git?"), i18n.T("It is a project's git history: what is not pushed exists only here."), Keep)
		return r
	}
	if top, err := Git(ctx, gitDir(path), "rev-parse", "--show-toplevel"); err == nil {
		r.Repo = strings.TrimSpace(top)
	}
	switch {
	case r.Repo != "" && sameFile(r.Repo, path):
		r.add(i18n.T("Kept by git?"), repoState(ctx, path), Unknown)
	case r.Repo != "":
		rel, _ := filepath.Rel(r.Repo, path)
		if out, err := Git(ctx, r.Repo, "ls-files", "-z", "--", rel); err == nil && out != "" {
			r.add(i18n.T("Kept by git?"), i18n.Tf("Its files are part of the project in %s: delete them with git, so the project knows.", Tilde(r.Repo, home)), Keep)
			return r
		}
		if _, err := Git(ctx, r.Repo, "check-ignore", "-q", "--", rel); err == nil {
			r.Ignored = true
			r.add(i18n.T("Kept by git?"), i18n.Tf("No: the project in %s ignores it, which is usual for what is built or downloaded.", Tilde(r.Repo, home)), Unknown)
		} else {
			r.add(i18n.T("Kept by git?"), i18n.Tf("Not yet: it is new in the project in %s, not committed.", Tilde(r.Repo, home)), Unknown)
		}
	default:
		r.add(i18n.T("Kept by git?"), i18n.T("No: it is in no git project."), Unknown)
	}

	// 4. A rule that knows it.
	if r.Rule != nil {
		r.add(i18n.T("A kind inua knows?"), i18n.T(r.Rule.What)+": "+i18n.T(r.Rule.Why), r.Rule.Verdict)
		return r
	}
	r.add(i18n.T("A kind inua knows?"), i18n.T("No rule knows it."), Unknown)
	return r
}

// gitDir is the folder git is asked in: path, or the one it is in when it
// is a file or gone.
func gitDir(path string) string {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func sameFile(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// repoState says what would be lost with a repository: changes not
// committed, commits not pushed.
func repoState(ctx context.Context, dir string) string {
	status, err := Git(ctx, dir, "status", "--porcelain")
	if err != nil {
		return i18n.T("It is a git project; git could not say what is not committed.")
	}
	changes := 0
	for line := range strings.SplitSeq(status, "\n") {
		if strings.TrimSpace(line) != "" {
			changes++
		}
	}
	unpushed := 0
	if out, err := Git(ctx, dir, "log", "--branches", "--not", "--remotes", "--oneline"); err == nil {
		for line := range strings.SplitSeq(out, "\n") {
			if strings.TrimSpace(line) != "" {
				unpushed++
			}
		}
	}
	remotes, _ := Git(ctx, dir, "remote")
	var lost []string
	if changes > 0 {
		lost = append(lost, i18n.Nf("%d change not committed", "%d changes not committed", changes, changes))
	}
	if unpushed > 0 {
		lost = append(lost, i18n.Nf("%d commit not pushed", "%d commits not pushed", unpushed, unpushed))
	}
	switch {
	case len(lost) > 0:
		return i18n.Tf("It is a git project with %s: they exist only here.", strings.Join(lost, i18n.T(" and ")))
	case strings.TrimSpace(remotes) == "":
		return i18n.T("It is a git project with no remote: its history exists only here.")
	}
	return i18n.T("It is a git project, all committed and pushed: it can be cloned again.")
}

// protected says why path is the system's or one of the user's main
// folders, or "".
func protected(path, home string) string {
	switch path {
	case "/", "/usr", "/etc", "/boot", "/bin", "/sbin", "/lib", "/lib64", "/opt", "/var", "/srv", "/root",
		"/home", "/proc", "/sys", "/dev", "/run", "/tmp", "/nix", "/nix/store", "/mnt", "/media":
		return i18n.T("It is part of the system.")
	}
	for _, p := range []string{"/usr/", "/etc/", "/boot/", "/bin/", "/sbin/", "/lib/", "/lib64/", "/proc/", "/sys/", "/dev/", "/run/", "/nix/store/"} {
		if strings.HasPrefix(path, p) {
			return i18n.T("It is part of the system.")
		}
	}
	if strings.HasPrefix(path, "/var/") && !strings.HasPrefix(path, "/var/cache/") && !strings.HasPrefix(path, "/var/tmp/") &&
		!strings.HasPrefix(path, "/var/lib/docker") && !strings.HasPrefix(path, "/var/lib/flatpak") && !strings.HasPrefix(path, "/var/log/journal") {
		return i18n.T("It is part of the system: programs keep their state there.")
	}
	if home == "" {
		return ""
	}
	if path == home {
		return i18n.T("It is your home folder.")
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	switch rel {
	case ".config", ".local", ".local/share", ".local/state", ".local/bin", ".cache", ".ssh", ".gnupg", ".password-store",
		".local/share/keyrings", ".pki", ".mozilla", ".thunderbird":
		return i18n.T("Your settings, keys or programs' data are kept there.")
	}
	for _, p := range []string{".ssh/", ".gnupg/", ".password-store/", ".local/share/keyrings/", ".pki/"} {
		if strings.HasPrefix(rel, p) {
			return i18n.T("Your keys and passwords are kept there.")
		}
	}
	if !strings.Contains(rel, "/") {
		for _, d := range xdgDirs(home) {
			if path == d {
				return i18n.T("It is one of your main folders: look inside it instead.")
			}
		}
	}
	return ""
}

// xdgDirs are the user's main folders: Documents, Pictures and the rest,
// as user-dirs.dirs names them, else their English names.
func xdgDirs(home string) []string {
	names := map[string]string{
		"XDG_DESKTOP_DIR": "Desktop", "XDG_DOCUMENTS_DIR": "Documents", "XDG_DOWNLOAD_DIR": "Downloads",
		"XDG_MUSIC_DIR": "Music", "XDG_PICTURES_DIR": "Pictures", "XDG_VIDEOS_DIR": "Videos",
		"XDG_PUBLICSHARE_DIR": "Public", "XDG_TEMPLATES_DIR": "Templates",
	}
	dirs := map[string]string{}
	for k, v := range names {
		dirs[k] = filepath.Join(home, v)
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	if b, err := os.ReadFile(filepath.Join(config, "user-dirs.dirs")); err == nil {
		for line := range strings.SplitSeq(string(b), "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok || names[k] == "" {
				continue
			}
			v = strings.Trim(v, `"`)
			dirs[k] = filepath.Clean(strings.Replace(v, "$HOME", home, 1))
		}
	}
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, d)
	}
	return out
}

// Tilde writes path with the home as ~.
func Tilde(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+"/"); ok {
		return "~/" + rest
	}
	return path
}
