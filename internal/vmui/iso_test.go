package vmui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/vm"
)

// ── the install images ──────────────────────────────────────────────────────

// isoService is a service whose install images are the test's own: a downloader
// over a cache in a temporary folder, and a catalog the test names. None of it
// touches osinfo-db, the user's own cache or the network.
func isoService(t *testing.T, media ...vm.ISOMedia) (*vmService, vm.Store, string) {
	t.Helper()
	root := t.TempDir()
	store := vm.Store{Root: filepath.Join(root, "data"), Runtime: filepath.Join(root, "run")}
	dir := filepath.Join(root, "iso")
	cat := vm.ISOCatalog{Source: vm.ISOSourceOsinfo, Media: media}
	cache := vm.NewISODownloader(dir)
	// How much room a download needs is the test's own answer: a machine's real
	// filesystem must not decide whether a test passes.
	cache.FreeSpace = func(string) (int64, error) { return 100 << 30, nil }
	s := &vmService{
		store:      store,
		platform:   readyHost(),
		secrets:    fakeSecrets{},
		backends:   map[vm.BackendKind]vm.Backend{vm.BackendQEMU: &fakeBackend{}},
		seen:       map[string]observed{},
		busy:       map[string]bool{},
		isoCache:   cache,
		isoCatalog: &cat,
	}
	return s, store, dir
}

// aQEMUMachine is a machine that installs from an image, on disk.
func aQEMUMachine(t *testing.T, store vm.Store, name string) vm.Machine {
	t.Helper()
	m := fromTemplate(t, "linux", name, vm.Profile{MemoryMiB: 4096, CPUs: 2, DiskGiB: 40})
	write(t, store, m, false)
	return m
}

// isoMirror is a mirror of one small image. It serves the file; it can cut a
// transfer short, so that a part file is left behind; it can hold the rest of
// one until the test says so, which is the transfer a user's Cancel
// interrupts; and it can refuse altogether. Anything but the image's own name -
// the checksum lists included - is a 404, which is their usual answer.
type isoMirror struct {
	*httptest.Server
	body    []byte
	digest  string // a published SHA-256 for the image, when the test sets one
	mode    atomic.Int32
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	ranges  []string // the Range header of every request for the image
}

const (
	isoServe int32 = iota
	isoCut
	isoSlow
	isoFail
)

func newISOMirror(t *testing.T, body []byte) *isoMirror {
	t.Helper()
	m := &isoMirror{body: body, started: make(chan struct{}), release: make(chan struct{})}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(func() {
		// Let a held transfer finish before the server stops waiting for it.
		close(m.release)
		m.Server.Close()
	})
	return m
}

func (m *isoMirror) url() string { return m.Server.URL + "/test.iso" }

func (m *isoMirror) handle(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)
	if name == "test.iso-CHECKSUM" && m.digest != "" {
		fmt.Fprintf(w, "%s  test.iso\n", m.digest)
		return
	}
	if name != "test.iso" {
		http.NotFound(w, r)
		return
	}
	m.mu.Lock()
	m.ranges = append(m.ranges, r.Header.Get("Range"))
	m.mu.Unlock()
	switch m.mode.Load() {
	case isoFail:
		http.Error(w, "the mirror is down", http.StatusInternalServerError)
	case isoCut:
		// The mirror promises the whole file and drops the connection after
		// half of it: the transfer a retry and a resume are there for.
		w.Header().Set("Content-Length", strconv.Itoa(len(m.body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(m.body[:len(m.body)/2])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	case isoSlow:
		w.Header().Set("Content-Length", strconv.Itoa(len(m.body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(m.body[:len(m.body)/2])
		w.(http.Flusher).Flush()
		close(m.started)
		<-m.release
		_, _ = w.Write(m.body[len(m.body)/2:])
	default:
		http.ServeContent(w, r, "test.iso", time.Time{}, bytes.NewReader(m.body))
	}
}

// lastRange is the Range header of the latest request for the image, which is
// how a resume is seen.
func (m *isoMirror) lastRange() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.ranges) == 0 {
		return ""
	}
	return m.ranges[len(m.ranges)-1]
}

// waitFor waits until what the test is looking for has happened, or gives up.
// A transfer runs in its own goroutine, so the state it is in is not known the
// moment the mirror starts answering.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waiting for %s: it did not happen", what)
}

// The list the window shows is the catalog plus what stands beside it on disk:
// an image already downloaded, one left half-downloaded, and neither.
func TestISORowsFollowTheCache(t *testing.T) {
	live := vm.ISOMedia{
		OSID: "fedora42", OSName: "Fedora Linux 42", Variant: "Fedora Workstation 42",
		Arch: "x86_64", Live: true, URL: "https://example.org/fedora/Fedora-Workstation-42.iso",
		VolumeSize: 2 << 30,
	}
	netinst := vm.ISOMedia{
		OSID: "debian12", OSName: "Debian 12", Variant: "netinst",
		Arch: "x86_64", Installer: true,
		URL: "https://example.org/debian/debian-12.11.0-amd64-netinst.iso",
	}
	s, _, _ := isoService(t, live, netinst)
	cache := s.isoDownloader()
	for _, p := range []string{cache.TargetPath(live), cache.TargetPath(netinst)} {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(cache.TargetPath(live), []byte("an image"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache.TargetPath(netinst)+".part", []byte("half an image"), 0600); err != nil {
		t.Fatal(err)
	}

	rows := isoRowsOf(vm.ISOCatalog{Source: vm.ISOSourceOsinfo, Media: []vm.ISOMedia{live, netinst}}, s.isoDownloader(), nil)
	if len(rows) != 2 {
		t.Fatalf("%d rows, want one per medium", len(rows))
	}
	if rows[0].Media.URL != live.URL || rows[1].Media.URL != netinst.URL {
		t.Errorf("the rows are not in the catalog's order: %v", rows)
	}
	if !rows[0].Cached || rows[0].Partial || rows[0].Path != cache.TargetPath(live) {
		t.Errorf("the image in the cache: %+v", rows[0])
	}
	if rows[1].Cached || !rows[1].Partial {
		t.Errorf("the unfinished download: %+v", rows[1])
	}

	// What each row says, and what its button does about it.
	if got := isoActionText(rows[0]); got != "Use this image" {
		t.Errorf("a cached image's button = %q", got)
	}
	if got := isoActionText(rows[1]); got != "Resume" {
		t.Errorf("an unfinished download's button = %q", got)
	}
	if got := isoActionText(ISORow{Media: live}); got != "Download" {
		t.Errorf("an image that is not here yet = %q", got)
	}
	if got := isoActionText(ISORow{Media: live, Err: errors.New("no room")}); got != "Retry" {
		t.Errorf("a failed download = %q", got)
	}
	if got := isoMediaTitle(live); got != "Fedora Linux 42 · Fedora Workstation 42" {
		t.Errorf("title = %q", got)
	}
	if got := isoMediaTitle(netinst); got != "Debian 12 · netinst" {
		t.Errorf("title = %q", got)
	}
	if got := isoMediaTitle(vm.ISOMedia{OSName: "Ubuntu 24.04"}); got != "Ubuntu 24.04" {
		t.Errorf("a medium with no variant = %q", got)
	}
	for _, want := range []string{"x86_64", "Live image", "2.0 GiB", "Already in the cache"} {
		if !strings.Contains(isoMediaSub(rows[0]), want) {
			t.Errorf("a live image's line %q does not say %q", isoMediaSub(rows[0]), want)
		}
	}
	// An image that was used as it was - nothing downloaded, nothing verified -
	// still says how it stands, and says it once.
	used := rows[0]
	used.Checksum = vm.ISOChecksumNotChecked
	if got := isoMediaSub(used); strings.Count(got, "Already in the cache") != 1 || strings.HasSuffix(got, "·") {
		t.Errorf("an image that was already here reads %q", got)
	}
	verified := rows[0]
	verified.Checksum = vm.ISOChecksumVerified
	if got := isoMediaSub(verified); !strings.Contains(got, "Checksum verified") || strings.Contains(got, "Already in the cache") {
		t.Errorf("a verified image reads %q", got)
	}
	for _, want := range []string{"x86_64", "Installer", "Unfinished download"} {
		if !strings.Contains(isoMediaSub(rows[1]), want) {
			t.Errorf("a netinst's line %q does not say %q", isoMediaSub(rows[1]), want)
		}
	}
}

// The install images are for the machines that install from one, and for the
// page that has not installed anything yet: a machine that cannot use them is
// not offered them, and a list that has done its work is let go.
func TestTheInstallListIsOnlyForAMachinesOwnSetup(t *testing.T) {
	s, store, _ := isoService(t, vm.ISOMedia{OSID: "t", OSName: "Test", Arch: "x86_64", Installer: true, URL: "https://example.org/test.iso"})
	if s.isoView(vm.DockurTemplate("windows")) != nil {
		t.Error("a Windows container was offered install images")
	}
	if s.isoView(vm.Machine{Name: "android", Backend: vm.BackendWaydroid}) != nil {
		t.Error("Waydroid was offered install images")
	}
	m := aQEMUMachine(t, store, "fedora")
	if v := s.view(m); v.ISO == nil || len(v.ISO.Rows) != 1 {
		t.Fatalf("a machine that has installed nothing has no install list: %+v", v.ISO)
	}
	s.mu.Lock()
	s.seen["fedora"] = observed{prepared: true, guest: true}
	s.mu.Unlock()
	if v := s.view(m); v.ISO != nil {
		t.Error("an installed machine still carries the download list")
	}
}

// A download lands in the cache under the distribution's own name and becomes
// the machine's install media: the install flow needs nothing else.
func TestFetchISOMakesTheImageTheInstallMedia(t *testing.T) {
	body := bytes.Repeat([]byte("inua-iso-"), 512)
	mirror := newISOMirror(t, body)
	media := vm.ISOMedia{
		OSID: "testos", OSName: "Test OS", Arch: "x86_64", Installer: true,
		URL: mirror.url(), VolumeSize: int64(len(body)),
	}
	s, store, _ := isoService(t, media)
	m := aQEMUMachine(t, store, "fedora")

	if err := s.FetchISO(m.Name, media); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	target := s.isoDownloader().TargetPath(media)
	data, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(data, body) {
		t.Fatalf("the image in the cache is not the one served: %v", err)
	}
	if _, ok := s.isoDownloader().PartialPath(media); ok {
		t.Error("a finished download left its part file behind")
	}
	got, err := store.Load(m.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.Boot.ISO != target {
		t.Errorf("boot.iso = %q, want %q", got.Boot.ISO, target)
	}
	view := s.isoView(got)
	if view.Downloading || view.Done != int64(len(body)) || view.Total != int64(len(body)) {
		t.Errorf("the transfer is not shown as finished: %+v", view)
	}
	if len(view.Rows) != 1 || !view.Rows[0].Cached {
		t.Fatalf("the row does not show the image is here: %+v", view.Rows)
	}
	if view.Rows[0].Checksum != vm.ISOChecksumNotPublished {
		t.Errorf("checksum = %q, want the usual 'no checksum published'", view.Rows[0].Checksum)
	}
}

// A failure is carried on the row it belongs to - in the downloader's own
// words, which say what went wrong - and the row's button offers to try again.
func TestFetchISOFailureIsShownAndCanBeRetried(t *testing.T) {
	body := bytes.Repeat([]byte("retry-"), 256)
	mirror := newISOMirror(t, body)
	mirror.mode.Store(isoFail)
	media := vm.ISOMedia{
		OSID: "testos", OSName: "Test OS", Arch: "x86_64", Installer: true,
		URL: mirror.url(), VolumeSize: int64(len(body)),
	}
	s, store, _ := isoService(t, media)
	m := aQEMUMachine(t, store, "fedora")

	err := s.FetchISO(m.Name, media)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want the server's own answer", err)
	}
	view := s.isoView(m)
	if len(view.Rows) != 1 || view.Rows[0].Err == nil {
		t.Fatalf("the row does not carry the failure: %+v", view.Rows)
	}
	if got := isoActionText(view.Rows[0]); got != "Retry" {
		t.Errorf("a failed row's button = %q", got)
	}
	if view.Downloading {
		t.Error("a download that returned is still shown as running")
	}
	if got, err := store.Load(m.Name); err != nil || got.Boot.ISO != "" {
		t.Errorf("a failed download became the install media: %q %v", got.Boot.ISO, err)
	}

	// The retry is the user's own act, and it fetches the file again.
	mirror.mode.Store(isoServe)
	if err := s.FetchISO(m.Name, media); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if data, err := os.ReadFile(s.isoDownloader().TargetPath(media)); err != nil || !bytes.Equal(data, body) {
		t.Fatalf("the retried image is not the one served: %v", err)
	}
	if got, _ := store.Load(m.Name); got.Boot.ISO != s.isoDownloader().TargetPath(media) {
		t.Errorf("boot.iso = %q after the retry", got.Boot.ISO)
	}
}

// A stop is the user's own act: the transfer returns where it is, the part file
// stays, and the row then offers to resume it rather than to start again.
func TestCancelStopsTheDownloadAndKeepsThePartFile(t *testing.T) {
	body := bytes.Repeat([]byte("cancel-"), 1024)
	mirror := newISOMirror(t, body)
	mirror.mode.Store(isoSlow)
	media := vm.ISOMedia{
		OSID: "testos", OSName: "Test OS", Arch: "x86_64", Installer: true,
		URL: mirror.url(), VolumeSize: int64(len(body)),
	}
	s, store, _ := isoService(t, media)
	m := aQEMUMachine(t, store, "fedora")
	part := s.isoDownloader().TargetPath(media) + ".part"

	done := make(chan error, 1)
	go func() { done <- s.FetchISO(m.Name, media) }()
	<-mirror.started
	// While it runs, the view says so and knows which medium it is; the row's
	// own button is the one thing not to use again.
	view := s.isoView(m)
	if !view.Downloading || view.Media.URL != media.URL || !view.Rows[0].Downloading {
		t.Errorf("the view does not show the transfer in flight: %+v", view)
	}
	waitFor(t, "the first of the file on disk", func() bool {
		fi, err := os.Stat(part)
		return err == nil && fi.Size() > 0
	})

	s.CancelISO(m.Name)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("a stopped download returned %v, want context.Canceled", err)
	}
	if fi, err := os.Stat(part); err != nil || fi.Size() == 0 {
		t.Fatalf("the part file should be there to resume: %v", err)
	}
	if _, err := os.Stat(s.isoDownloader().TargetPath(media)); !os.IsNotExist(err) {
		t.Error("a stopped download left a file under the image's own name")
	}
	if got, _ := store.Load(m.Name); got.Boot.ISO != "" {
		t.Errorf("a stopped download became the install media: %q", got.Boot.ISO)
	}
	after := s.isoView(m)
	if after.Downloading || !after.Rows[0].Partial {
		t.Errorf("after the stop the row should offer to resume: %+v", after.Rows[0])
	}
	// Stopping a machine that is downloading nothing is not an error.
	s.CancelISO("nothing")
	s.CancelISO(m.Name)
}

// A part file - from a stopped or a failed download - is resumed with a range
// request rather than downloaded again, which is what inua keeps from the ways
// GNOME Boxes does not have.
func TestFetchISOResumesFromThePartFile(t *testing.T) {
	body := bytes.Repeat([]byte("resume-"), 2048)
	mirror := newISOMirror(t, body)
	mirror.mode.Store(isoCut)
	media := vm.ISOMedia{
		OSID: "testos", OSName: "Test OS", Arch: "x86_64", Installer: true,
		URL: mirror.url(), VolumeSize: int64(len(body)),
	}
	s, store, _ := isoService(t, media)
	m := aQEMUMachine(t, store, "fedora")

	if err := s.FetchISO(m.Name, media); err == nil {
		t.Fatal("a transfer the mirror cut short should fail")
	}
	part := s.isoDownloader().TargetPath(media) + ".part"
	fi, err := os.Stat(part)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("the part file should be kept for the next attempt: %v", err)
	}
	half := fi.Size()

	mirror.mode.Store(isoServe)
	if err := s.FetchISO(m.Name, media); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if want := fmt.Sprintf("bytes=%d-", half); mirror.lastRange() != want {
		t.Errorf("the resume asked for %q, want %q", mirror.lastRange(), want)
	}
	data, err := os.ReadFile(s.isoDownloader().TargetPath(media))
	if err != nil || !bytes.Equal(data, body) {
		t.Fatalf("the resumed image is %d bytes, want the whole %d: %v", len(data), len(body), err)
	}
}

// A download that cannot fit is refused before anything is written, and what
// the user is shown is the sentence saying how much room is missing.
func TestFetchISOIsRefusedWhenThereIsNoRoom(t *testing.T) {
	mirror := newISOMirror(t, bytes.Repeat([]byte("x"), 1<<16))
	media := vm.ISOMedia{
		OSID: "big", OSName: "Big OS", Arch: "x86_64", Installer: true,
		URL: mirror.url(), VolumeSize: 8 << 30,
	}
	s, store, dir := isoService(t, media)
	m := aQEMUMachine(t, store, "fedora")
	s.isoDownloader().FreeSpace = func(string) (int64, error) { return 1 << 30, nil }

	err := s.FetchISO(m.Name, media)
	if err == nil || !strings.Contains(err.Error(), "not enough room") {
		t.Fatalf("err = %v, want the missing room explained", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("a refused download wrote something: %v", entries)
	}
	if got, _ := store.Load(m.Name); got.Boot.ISO != "" {
		t.Errorf("a refused download became the install media: %q", got.Boot.ISO)
	}
	view := s.isoView(m)
	if len(view.Rows) != 1 || view.Rows[0].Err == nil || !strings.Contains(view.Rows[0].Err.Error(), "not enough room") {
		t.Errorf("the row does not carry the reason: %+v", view.Rows)
	}
}

// A published checksum is verified while the file still has its temporary name:
// a file that matches is kept and said to be verified, and one that does not is
// removed rather than offered as an install image.
func TestFetchISOVerifiesAPublishedChecksum(t *testing.T) {
	body := bytes.Repeat([]byte("sum-"), 1024)
	mirror := newISOMirror(t, body)
	sum := sha256.Sum256(body)
	mirror.digest = hex.EncodeToString(sum[:])
	media := vm.ISOMedia{
		OSID: "testos", OSName: "Test OS", Arch: "x86_64", Installer: true,
		URL: mirror.url(), VolumeSize: int64(len(body)),
	}

	s, store, _ := isoService(t, media)
	m := aQEMUMachine(t, store, "fedora")
	if err := s.FetchISO(m.Name, media); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := s.isoView(m).Rows[0].Checksum; got != vm.ISOChecksumVerified {
		t.Errorf("checksum = %q, want verified", got)
	}

	// The same image from a mirror whose published sum is another file's.
	mirror.digest = strings.Repeat("0", 64)
	other, otherStore, _ := isoService(t, media)
	otherMachine := aQEMUMachine(t, otherStore, "fedora")
	if err := other.FetchISO(otherMachine.Name, media); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("err = %v, want the published sum named", err)
	}
	for _, leftover := range []string{other.isoDownloader().TargetPath(media), other.isoDownloader().TargetPath(media) + ".part"} {
		if _, err := os.Stat(leftover); !os.IsNotExist(err) {
			t.Errorf("a file that failed its checksum was left as %s", leftover)
		}
	}
	if got, _ := otherStore.Load(m.Name); got.Boot.ISO != "" {
		t.Errorf("a file that failed its checksum became the install media: %q", got.Boot.ISO)
	}
	if _, err := os.Stat(s.isoDownloader().TargetPath(media)); err != nil {
		t.Errorf("the verified image is gone: %v", err)
	}
}
