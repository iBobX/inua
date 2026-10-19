package vm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// isoTestImage is what the tests download: a body large enough to be streamed
// in more than one read, with an ETag, so a resume has a validator to send.
func isoTestImage(n int) []byte {
	image := make([]byte, n)
	for i := range image {
		image[i] = byte(i * 7)
	}
	return image
}

func isoTestMedia(server *httptest.Server, name string) ISOMedia {
	return ISOMedia{
		OSID: "test1", OSName: "Test 1", Release: "1", Variant: "netinst",
		Arch: "x86_64", URL: server.URL + "/" + name, Installer: true,
	}
}

// isoTestServer serves one image and nothing else: no checksum beside it,
// which is what most distributions' media look like.
func isoTestServer(t *testing.T, image []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".iso") {
			w.Header().Set("ETag", `"test-image"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(image)))
			_, _ = w.Write(image)
			return
		}
		http.NotFound(w, r)
	}))
}

// A whole download: the bytes arrive, the file is 0600, the final name appears
// only at the end, and the .part file and its validator are gone when it is
// there.
func TestISODownloadStreamsAnImage(t *testing.T) {
	image := isoTestImage(256 << 10)
	server := isoTestServer(t, image)
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "iso")
	d := NewISODownloader(dir)
	media := isoTestMedia(server, "test-1-amd64-netinst.iso")
	target := d.TargetPath(media)

	var reports int
	var sawFinalName bool
	path, err := d.Download(context.Background(), media, func(done, total int64) {
		reports++
		if _, err := os.Stat(target); err == nil {
			sawFinalName = true
		}
		if total != 0 && total != int64(len(image)) {
			t.Errorf("progress total = %d, want %d", total, len(image))
		}
		if done > int64(len(image)) {
			t.Errorf("progress done = %d, past the image", done)
		}
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if path != target {
		t.Errorf("path = %q, want %q", path, target)
	}
	if sawFinalName {
		t.Error("the final name appeared while the transfer was still running")
	}
	if reports == 0 {
		t.Error("progress was never reported")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, image) {
		t.Errorf("the file is %d bytes, want %d, and does not match", len(got), len(image))
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	// The cache is the user's own business, so it is not readable by others
	// either. The directory is made 0700 on the first download.
	if di, err := os.Stat(dir); err != nil || di.Mode().Perm() != 0700 {
		t.Errorf("cache directory = %v, %v, want 0700", di, err)
	}
	for _, left := range []string{d.partPath(media), d.validatorPath(media)} {
		if _, err := os.Stat(left); err == nil {
			t.Errorf("%s was left behind", left)
		}
	}
	// An image that is there is finished: the same medium is not downloaded
	// again, and the window can offer the file.
	if cached, ok := d.CachedPath(media); !ok || cached != target {
		t.Errorf("CachedPath = %q, %v", cached, ok)
	}
	if outcome, ok := d.Checksum(media.URL); !ok || outcome != ISOChecksumNotPublished {
		t.Errorf("checksum = %q, %v, want %q", outcome, ok, ISOChecksumNotPublished)
	}
}

// A download interrupted by an error keeps its .part file, and the next one
// resumes it: it asks for the rest with a Range header, and the server's 206
// is appended to what is already there. If-Range carries the validator kept
// beside the part file, so the server, not inua, decides whether those bytes
// are still the file's beginning.
func TestISODownloadResumesAPartialFile(t *testing.T) {
	image := isoTestImage(64 << 10)
	const first = 16 << 10

	var mu sync.Mutex
	var ranges, ifRanges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := image[first:]
		if r.URL.Path != "/test-1-amd64-netinst.iso" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		ifRanges = append(ifRanges, r.Header.Get("If-Range"))
		mu.Unlock()
		w.Header().Set("ETag", `"test-image"`)
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, len(image)-1, len(image)))
			w.Header().Set("Content-Length", strconv.Itoa(len(rest)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(rest)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(image)))
		_, _ = w.Write(image)
	}))
	defer server.Close()

	d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
	media := isoTestMedia(server, "test-1-amd64-netinst.iso")
	if err := os.MkdirAll(d.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.partPath(media), image[:first], 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.validatorPath(media), []byte("\"test-image\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// The catalog knows a medium's size. With one, the transfer resumes from
	// the part file with a single ranged request instead of asking the server
	// for the image's size first.
	media.VolumeSize = int64(len(image))
	if _, ok := d.PartialPath(media); !ok {
		t.Fatal("the part file should be there to resume from")
	}

	path, err := d.Download(context.Background(), media, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, image) {
		t.Errorf("the resumed file is %d bytes and does not match the %d byte image", len(got), len(image))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ranges) != 1 || ranges[0] != fmt.Sprintf("bytes=%d-", first) {
		t.Errorf("ranges = %v, want one bytes=%d-", ranges, first)
	}
	if len(ifRanges) != 1 || ifRanges[0] != `"test-image"` {
		t.Errorf("If-Range = %v, want the validator kept beside the part file", ifRanges)
	}
}

// A server that answers a range request with the whole file (200) is taken at
// its word: the transfer starts again from zero rather than appending.
func TestISODownloadStartsAgainOnAWholeAnswer(t *testing.T) {
	image := isoTestImage(32 << 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(image)))
		_, _ = w.Write(image)
	}))
	defer server.Close()

	d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
	media := isoTestMedia(server, "test-1-amd64-netinst.iso")
	if err := os.MkdirAll(d.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	// A part file that is not this image's beginning, and no validator to tell
	// the server: the 200 that comes back replaces it.
	if err := os.WriteFile(d.partPath(media), bytes.Repeat([]byte("x"), 8<<10), 0600); err != nil {
		t.Fatal(err)
	}
	path, err := d.Download(context.Background(), media, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, image) {
		t.Errorf("the file is %d bytes and does not match the %d byte image", len(got), len(image))
	}
}

// A published checksum is verified before the rename, and the answer says so.
// Fedora's own form of the file - <name>-CHECKSUM with "SHA256 (name) = hash"
// lines - is read as well as Debian's and Ubuntu's SHA256SUMS.
func TestISODownloadVerifiesAPublishedChecksum(t *testing.T) {
	image := isoTestImage(8 << 10)
	sum := sha256.Sum256(image)
	checksum := hex.EncodeToString(sum[:])

	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{"fedora", "test-1-amd64-netinst.iso-CHECKSUM",
			"# test-1-amd64-netinst.iso: 8192 bytes\nSHA256 (test-1-amd64-netinst.iso) = " + checksum + "\n"},
		{"debian and ubuntu", "SHA256SUMS",
			"7d865e959b2466918c9863afca942d0fb89d7c9ac0c99bafc3749504ded97730  other.iso\n" +
				checksum + " *test-1-amd64-netinst.iso\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/test-1-amd64-netinst.iso":
					w.Header().Set("Content-Length", strconv.Itoa(len(image)))
					_, _ = w.Write(image)
				case "/" + tc.path:
					_, _ = io.WriteString(w, tc.body)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
			media := isoTestMedia(server, "test-1-amd64-netinst.iso")
			path, err := d.Download(context.Background(), media, nil)
			if err != nil {
				t.Fatalf("download: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("no image at %s: %v", path, err)
			}
			if outcome, ok := d.Checksum(media.URL); !ok || outcome != ISOChecksumVerified {
				t.Errorf("checksum = %q, %v, want %q", outcome, ok, ISOChecksumVerified)
			}
		})
	}
}

// A checksum that does not match is an error and nothing is kept: the bytes
// are known bad, so a retry has nothing to resume from.
func TestISODownloadRefusesAMismatchedChecksum(t *testing.T) {
	image := isoTestImage(8 << 10)
	other := isoTestImage(16 << 10)
	sum := sha256.Sum256(other)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test-1-amd64-netinst.iso":
			w.Header().Set("Content-Length", strconv.Itoa(len(image)))
			_, _ = w.Write(image)
		case "/SHA256SUMS":
			_, _ = io.WriteString(w, hex.EncodeToString(sum[:])+"  test-1-amd64-netinst.iso\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
	media := isoTestMedia(server, "test-1-amd64-netinst.iso")

	path, err := d.Download(context.Background(), media, nil)
	if err == nil {
		t.Fatalf("a mismatched checksum should be refused, got %s", path)
	}
	if !strings.Contains(err.Error(), "does not match the checksum") {
		t.Errorf("error = %v", err)
	}
	if _, err := os.Stat(d.TargetPath(media)); err == nil {
		t.Error("a corrupt image was left under the final name")
	}
	for _, left := range []string{d.partPath(media), d.validatorPath(media)} {
		if _, err := os.Stat(left); err == nil {
			t.Errorf("%s was left behind after a bad checksum", left)
		}
	}
}

// A distribution that publishes no checksum is the common case, and it is not
// an error: the image is finished and the result says it was not verified.
func TestISODownloadProceedsWithoutAPublishedChecksum(t *testing.T) {
	image := isoTestImage(4 << 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test-1-amd64-netinst.iso" {
			w.Header().Set("Content-Length", strconv.Itoa(len(image)))
			_, _ = w.Write(image)
			return
		}
		http.NotFound(w, r) // both checksum candidates, as most media have
	}))
	defer server.Close()
	d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
	media := isoTestMedia(server, "test-1-amd64-netinst.iso")

	path, err := d.Download(context.Background(), media, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, image) {
		t.Errorf("the image is not there: %v", err)
	}
	if outcome, ok := d.Checksum(media.URL); !ok || outcome != ISOChecksumNotPublished {
		t.Errorf("checksum = %q, %v, want %q", outcome, ok, ISOChecksumNotPublished)
	}
}

// A destination with no room is refused before anything is written: the body
// is never asked for, and the error says how much is missing. The probe is
// injected, so the test does not have to fill a disk.
func TestISODownloadRefusesWithoutFreeSpace(t *testing.T) {
	image := isoTestImage(1 << 10)
	var mu sync.Mutex
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Length", strconv.Itoa(len(image)))
		_, _ = w.Write(image)
	}))
	defer server.Close()

	d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
	media := isoTestMedia(server, "test-1-amd64-netinst.iso")
	// The catalog knows the size, so nothing has to be asked of the server to
	// find it out.
	media.VolumeSize = 4 << 30
	free := int64(1 << 30)
	d.FreeSpace = func(string) (int64, error) { return free, nil }

	path, err := d.Download(context.Background(), media, nil)
	if err == nil {
		t.Fatalf("a download without room should be refused, got %s", path)
	}
	if !strings.Contains(err.Error(), "not enough room") || !strings.Contains(err.Error(), isoBytes(4<<30+isoFreeSpaceMargin-free)) {
		t.Errorf("error should say how much is missing: %v", err)
	}
	mu.Lock()
	asked := append([]string(nil), requested...)
	mu.Unlock()
	for _, req := range asked {
		if strings.HasPrefix(req, "GET ") {
			t.Errorf("the body was asked for anyway: %v", asked)
		}
	}
	if _, err := os.Stat(d.partPath(media)); err == nil {
		t.Error("a refused download left a part file")
	}

	// The same medium, with room for it, is downloaded: the check refuses a
	// download, it does not refuse downloads.
	free = int64(8 << 30)
	path, err = d.Download(context.Background(), media, nil)
	if err != nil {
		t.Fatalf("download with room: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("no image at %s: %v", path, err)
	}
}

// Cancelling a download stops it and returns the context's error, and leaves
// no file under the final name. The part file stays, which is what the next
// attempt resumes.
func TestISODownloadStopsWhenCancelled(t *testing.T) {
	image := isoTestImage(1 << 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".iso") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(image)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(image[:32<<10])
		w.(http.Flusher).Flush()
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
	media := isoTestMedia(server, "test-1-amd64-netinst.iso")
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	path, err := d.Download(ctx, media, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the context's error", err)
	}
	if path != "" {
		t.Errorf("path = %q, want none", path)
	}
	if _, err := os.Stat(d.TargetPath(media)); err == nil {
		t.Error("a cancelled download was left under the final name")
	}
}

// Only http and https are streamed: the catalog's URLs are those, and anything
// else - a container image's docker:// URL among them - is an error rather
// than a file.
func TestISODownloadRefusesOtherSchemes(t *testing.T) {
	d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
	for _, raw := range []string{"ftp://example.org/x.iso", "docker://quay.io/x:1", "file:///etc/os-release", "/tmp/x.iso"} {
		media := ISOMedia{OSID: "test1", URL: raw}
		if path, err := d.Download(context.Background(), media, nil); err == nil {
			t.Errorf("%s should be refused, got %s", raw, path)
		}
		if path, _ := d.CachedPath(media); path != "" {
			t.Errorf("%s should not be a cached image", raw)
		}
	}
}

// The same file is never fetched twice at once: a second caller for a URL that
// is already being downloaded waits and is given the first one's answer.
func TestISODownloadWaitsForTheSameURL(t *testing.T) {
	image := isoTestImage(64 << 10)
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".iso") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		gets++
		first := gets == 1
		mu.Unlock()
		if first {
			close(started)
			<-release
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(image)))
		_, _ = w.Write(image)
	}))
	defer server.Close()

	d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
	media := isoTestMedia(server, "test-1-amd64-netinst.iso")
	// A medium whose size the catalog knows is fetched once: the second call
	// waits for the first rather than asking the server again.
	media.VolumeSize = int64(len(image))

	type result struct {
		path string
		err  error
	}
	first := make(chan result, 1)
	go func() {
		p, err := d.Download(context.Background(), media, nil)
		first <- result{p, err}
	}()
	<-started
	second := make(chan result, 1)
	go func() {
		p, err := d.Download(context.Background(), media, nil)
		second <- result{p, err}
	}()
	// Give the second caller time to arrive at the download that is in flight.
	time.Sleep(100 * time.Millisecond)
	close(release)

	a, b := <-first, <-second
	if a.err != nil || b.err != nil {
		t.Fatalf("downloads: %v, %v", a.err, b.err)
	}
	if a.path != b.path {
		t.Errorf("paths = %q and %q, want the same file", a.path, b.path)
	}
	mu.Lock()
	defer mu.Unlock()
	if gets != 1 {
		t.Errorf("the image was fetched %d times, want once", gets)
	}
}

// A URL that is not a file - no scheme, no host, no name - is refused before
// anything is looked up.
func TestISODownloadRefusesUnusableURLs(t *testing.T) {
	d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
	for _, raw := range []string{"", "https://example.org/", "http:///x.iso"} {
		if path, err := d.Download(context.Background(), ISOMedia{URL: raw}, nil); err == nil {
			t.Errorf("%q should be refused, got %s", raw, path)
		}
	}
}

// The checksum URLs are derived from the image's URL, as the distributions
// publish them: beside the image, under its own name with -CHECKSUM appended
// (Fedora), and beside it in SHA256SUMS (Debian and Ubuntu).
func TestISOChecksumURLs(t *testing.T) {
	got := isoChecksumURLs("https://download.example.org/pub/fedora/linux/releases/42/Server/x86_64/iso/Fedora-Server-netinst-x86_64-42-1.1.iso")
	want := []string{
		"https://download.example.org/pub/fedora/linux/releases/42/Server/x86_64/iso/Fedora-Server-netinst-x86_64-42-1.1.iso-CHECKSUM",
		"https://download.example.org/pub/fedora/linux/releases/42/Server/x86_64/iso/SHA256SUMS",
	}
	if !slices.Equal(got, want) {
		t.Errorf("checksum URLs = %v, want %v", got, want)
	}
	// A query is not part of where the file lives: a mirror's token must not
	// end up in the checksum URL.
	got = isoChecksumURLs("https://releases.example.org/noble/ubuntu-24.04-latest-live-server-amd64.iso?token=abc")
	want = []string{
		"https://releases.example.org/noble/ubuntu-24.04-latest-live-server-amd64.iso-CHECKSUM",
		"https://releases.example.org/noble/SHA256SUMS",
	}
	if !slices.Equal(got, want) {
		t.Errorf("checksum URLs = %v, want %v", got, want)
	}
	if got := isoChecksumURLs("not a url"); len(got) != 0 {
		t.Errorf("checksum URLs of a bad URL = %v, want none", got)
	}
}

// Both published forms of a checksum list are read, and a list that names some
// other file says nothing about this one.
func TestChecksumFor(t *testing.T) {
	const sum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		what string
		file string
		text string
		want string
	}{
		{
			what: "fedora",
			file: "Fedora-Server-netinst-x86_64-42-1.1.iso",
			text: "# Fedora-Server-netinst-x86_64-42-1.1.iso: 1234 bytes\n" +
				"SHA256 (Fedora-Server-netinst-x86_64-42-1.1.iso) = " + strings.ToUpper(sum) + "\n",
			want: sum,
		},
		{
			what: "debian",
			file: "debian-12.11.0-amd64-netinst.iso",
			text: sum + "  debian-12.11.0-amd64-netinst.iso\n",
			want: sum,
		},
		{
			what: "ubuntu, with a dot slash",
			file: "ubuntu-24.04-latest-live-server-amd64.iso",
			text: sum + " *./ubuntu-24.04-latest-live-server-amd64.iso\n",
			want: sum,
		},
		{
			what: "a list of other images",
			file: "Fedora-Server-netinst-x86_64-42-1.1.iso",
			text: sum + "  some-other.iso\n",
		},
		{what: "no list at all", file: "anything.iso"},
	} {
		got, ok := checksumFor(tc.text, tc.file)
		if tc.want == "" {
			if ok {
				t.Errorf("%s: found %q, want nothing", tc.what, got)
			}
			continue
		}
		if !ok || got != tc.want {
			t.Errorf("%s: checksum = %q, %v, want %q", tc.what, got, ok, tc.want)
		}
	}
}

// A real download, run only when a URL is given for it: it is the one test
// here that touches the network, and it is skipped by default.
//
//	INUA_ISO_DOWNLOAD_TEST_URL=https://…/netboot.xyz.iso go test ./internal/vm -run RealDownload -v
//
// The URL is downloaded into a temporary cache with the real client, which is
// what makes it worth running: the server, the mirrors' own agent checks and
// the redirects are all real. The image it was written for is netboot.xyz's,
// a couple of megabytes rather than a couple of gigabytes.
func TestISODownloadRealImage(t *testing.T) {
	raw := os.Getenv("INUA_ISO_DOWNLOAD_TEST_URL")
	if raw == "" {
		t.Skip("set INUA_ISO_DOWNLOAD_TEST_URL to download a real image")
	}
	d := NewISODownloader(filepath.Join(t.TempDir(), "iso"))
	media := ISOMedia{OSID: "real", OSName: "Real", Arch: "x86_64", URL: raw}
	var last, total int64
	path, err := d.Download(context.Background(), media, func(done, t int64) {
		last, total = done, t
	})
	if err != nil {
		t.Fatalf("download %s: %v", raw, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("no image at %s: %v", path, err)
	}
	if fi.Size() == 0 || fi.Size() != last {
		t.Errorf("the file is %d bytes, progress ended at %d", fi.Size(), last)
	}
	if total != 0 && total != fi.Size() {
		t.Errorf("progress total = %d, the file is %d bytes", total, fi.Size())
	}
	t.Logf("downloaded %d bytes to %s (checksum: %v)", fi.Size(), path, checksumOutcome(t, d, raw))
}

// checksumOutcome is what the real download's checksum turned out to be, for
// the log line: an image with no published checksum is the usual answer.
func checksumOutcome(t *testing.T, d *ISODownloader, raw string) ISOChecksum {
	t.Helper()
	outcome, _ := d.Checksum(raw)
	return outcome
}
