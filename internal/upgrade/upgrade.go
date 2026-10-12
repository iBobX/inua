// Package upgrade finds inua's newest release on GitHub and installs it in
// place of the running programs, the way scripts/install.sh does: the
// archive for this machine, checked against the release's checksums, its
// programs into the directory inua runs from and the light apps' launchers
// and icons into the data directory. The login session and the
// configuration are left alone.
package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Repo is where releases are published.
const Repo = "https://github.com/lyricat/inua"

// maxDownload bounds what is read from GitHub (a release is ~15 MB).
const maxDownload = 256 << 20

var errNotFound = errors.New("not found")

// Source fetches releases from Base (Repo) with Client, and their notes
// from API (APIBase when empty).
type Source struct {
	Base   string
	API    string
	Client *http.Client
}

// GitHub is the source the releases come from.
func GitHub() Source { return Source{Base: Repo, Client: http.DefaultClient} }

// Latest is the tag of the newest release, such as "v0.1.2". It reads
// where releases/latest redirects to, which, unlike GitHub's API, has no
// rate limit.
func (s Source) Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, s.Base+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	c := *s.Client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	tag := path.Base(loc)
	if resp.StatusCode/100 != 3 || !strings.Contains(loc, "/releases/tag/") || tag == "" {
		return "", fmt.Errorf("no release found at %s/releases (%s)", s.Base, resp.Status)
	}
	return tag, nil
}

// ReleaseURL is the page of a release.
func (s Source) ReleaseURL(tag string) string { return s.Base + "/releases/tag/" + tag }

// ArchiveName is the release archive for this machine, without ".tar.gz".
func ArchiveName() (string, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return "inua_linux_" + runtime.GOARCH, nil
	}
	return "", fmt.Errorf("no release for %s; build from source: %s", runtime.GOARCH, Repo)
}

// Download fetches a release's archive and checks it against the
// release's checksums.
func (s Source) Download(ctx context.Context, tag, name string) ([]byte, error) {
	base := s.Base + "/releases/download/" + tag + "/"
	sums, err := s.get(ctx, base+"checksums.txt")
	if errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("no release %s at %s/releases", tag, s.Base)
	}
	if err != nil {
		return nil, err
	}
	archive, err := s.get(ctx, base+name+".tar.gz")
	if err != nil {
		return nil, err
	}
	if err := Verify(archive, sums, name+".tar.gz"); err != nil {
		return nil, err
	}
	return archive, nil
}

func (s Source) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("downloading %s: %w", url, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	if len(b) > maxDownload {
		return nil, fmt.Errorf("downloading %s: larger than %d MB", url, maxDownload>>20)
	}
	return b, nil
}

// Verify checks data against its line in a sha256sum list.
func Verify(data, sums []byte, name string) error {
	for line := range strings.Lines(string(sums)) {
		sum, file, ok := strings.Cut(strings.TrimSpace(line), "  ")
		if !ok || strings.TrimPrefix(file, "*") != name {
			continue
		}
		got := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(got[:]), sum) {
			return fmt.Errorf("%s does not match its checksum", name)
		}
		return nil
	}
	return fmt.Errorf("no checksum for %s", name)
}

// Dev is the version of a build from source.
const Dev = "dev"

// IsRelease reports whether v is a release's version, not a build from
// source.
func IsRelease(v string) bool {
	_, _, ok := parse(v)
	return ok
}

// Compare orders two versions, "v0.1.2" or "0.1.2", with or without a
// "-suffix" (which comes before the release it names). Dev, a build from
// source, comes before every release. ok is false if either is something
// else.
func Compare(a, b string) (cmp int, ok bool) {
	pa, sa, okA := parse(a)
	pb, sb, okB := parse(b)
	switch {
	case a == Dev && b == Dev:
		return 0, true
	case a == Dev && okB:
		return -1, true
	case b == Dev && okA:
		return 1, true
	case !okA || !okB:
		return 0, false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, true
			}
			return 1, true
		}
	}
	switch {
	case sa == sb:
		return 0, true
	case sa == "":
		return 1, true
	case sb == "":
		return -1, true
	case sa < sb:
		return -1, true
	}
	return 1, true
}

func parse(v string) (nums [3]int, suffix string, ok bool) {
	v, suffix, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nums, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nums, "", false
		}
		nums[i] = n
	}
	return nums, suffix, true
}

// file is one file to install from the archive.
type file struct {
	dir, name string
	mode      os.FileMode
	data      []byte
}

// Install puts a release archive's programs (its top-level files named
// inua*) in binDir and the light apps' launchers and icons
// (config/apps/*.desktop, *.svg) under dataDir, as install.sh does. It
// writes everything next to its target first and then renames each into
// place, so a failure part way leaves the old programs, and running ones
// keep running. It returns the programs installed.
func Install(archive []byte, binDir, dataDir string) ([]string, error) {
	files, err := unpack(archive, binDir, dataDir)
	if err != nil {
		return nil, err
	}
	var programs []string
	for _, f := range files {
		if f.dir == binDir {
			programs = append(programs, f.name)
		}
	}
	if len(programs) == 0 {
		return nil, errors.New("the archive has no programs in it")
	}

	var staged []string
	defer func() {
		for _, tmp := range staged {
			os.Remove(tmp)
		}
	}()
	for _, f := range files {
		if err := os.MkdirAll(f.dir, 0o755); err != nil {
			return nil, err
		}
		tmp, err := os.CreateTemp(f.dir, "."+f.name+".upgrade-*")
		if err != nil {
			return nil, err
		}
		staged = append(staged, tmp.Name())
		_, err = tmp.Write(f.data)
		if err == nil {
			err = tmp.Chmod(f.mode)
		}
		if err == nil {
			err = tmp.Sync()
		}
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
	}
	for i, f := range files {
		if err := os.Rename(staged[i], filepath.Join(f.dir, f.name)); err != nil {
			return nil, err
		}
	}
	staged = nil
	return programs, nil
}

// unpack reads the files Install puts in place from the archive, which
// holds them under one directory (inua_linux_amd64/).
func unpack(archive []byte, binDir, dataDir string) ([]file, error) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(zr)
	var files []file
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		clean := path.Clean(h.Name)
		if !filepath.IsLocal(clean) {
			continue
		}
		_, rel, _ := strings.Cut(clean, "/")
		dir, name := path.Split(rel)
		var f file
		switch {
		case dir == "" && strings.HasPrefix(name, "inua"):
			f = file{dir: binDir, name: name, mode: 0o755}
		case dir == "config/apps/" && strings.HasSuffix(name, ".desktop"):
			f = file{dir: filepath.Join(dataDir, "applications"), name: name, mode: 0o644}
		case dir == "config/apps/" && strings.HasSuffix(name, ".svg"):
			f = file{dir: filepath.Join(dataDir, "icons", "hicolor", "scalable", "apps"), name: name, mode: 0o644}
		default:
			continue
		}
		if f.data, err = io.ReadAll(tr); err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		files = append(files, f)
	}
	return files, nil
}

// StillOld lists the programs among installed that still run from the
// binaries Install replaced in binDir (the kernel marks them "(deleted)"),
// leaving out those named in except and this process. It looks only at the user's own
// processes; others' cannot be read.
func StillOld(binDir string, installed []string, except ...string) []string {
	procs, _ := filepath.Glob("/proc/[0-9]*/exe")
	seen := map[string]bool{}
	var out []string
	self := fmt.Sprintf("/proc/%d/exe", os.Getpid()) // inua upgrade itself
	for _, p := range procs {
		exe, err := os.Readlink(p)
		if err != nil || p == self {
			continue
		}
		path, deleted := strings.CutSuffix(exe, " (deleted)")
		dir, name := filepath.Split(path)
		if !deleted || filepath.Clean(dir) != filepath.Clean(binDir) || seen[name] ||
			!slices.Contains(installed, name) || slices.Contains(except, name) {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// CheckEvery is how often the shell looks for a new release.
const CheckEvery = 24 * time.Hour

// State is what the shell's daily check remembers between runs.
type State struct {
	Checked  time.Time `json:"checked"`  // the last check that reached GitHub
	Notified string    `json:"notified"` // the release last announced
	Running  string    `json:"running"`  // the version the shell last ran
}

// Decide says whether to install release tag over version have. force
// installs it anyway; pinned is a release named on purpose (-version),
// taken even when older. why says why there is nothing to do; err
// refuses a version that cannot be compared.
func Decide(have, tag string, force, pinned bool) (install bool, why string, err error) {
	order, known := Compare(have, tag)
	switch {
	case force:
	case !known:
		return false, "", fmt.Errorf("cannot compare inua %s with release %s: install it anyway with inua upgrade -force", have, tag)
	case order == 0:
		return false, fmt.Sprintf("inua %s is already installed.", strings.TrimPrefix(have, "v")), nil
	case order > 0 && !pinned:
		return false, fmt.Sprintf("inua %s is newer than the latest release (%s); nothing to do.", strings.TrimPrefix(have, "v"), tag), nil
	}
	return true, "", nil
}

// Due reports whether it is time to check again: a day after the last
// check, or at once if the clock went back.
func (st State) Due(now time.Time) bool {
	return !now.Before(st.Checked.Add(CheckEvery)) || now.Before(st.Checked)
}

// Announce reports whether to tell someone running version about release
// tag: when it is newer and has not been announced before. A build from
// source is not told.
func (st State) Announce(version, tag string) bool {
	order, ok := Compare(version, tag)
	return IsRelease(version) && ok && order < 0 && st.Notified != tag
}
