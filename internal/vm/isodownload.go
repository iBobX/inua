package vm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Downloading an install image is the second half of what GNOME Boxes does
// with osinfo-db's catalog, and the half inua writes itself: Boxes streams the
// URL into ~/Downloads and trusts what arrives. inua keeps the parts of that
// which hold - one HTTP client, a temporary name until the transfer is done,
// Content-Length progress, an existing file treated as finished - and adds the
// parts Boxes does not have: a resume from a `.part` file, a published SHA-256
// when the distribution publishes one, and a refusal to start when the
// destination has no room.
//
// See docs/iso-download.md, sections 4 and 7.3.

const (
	// isoUserAgent is what every request says it is. Some mirrors redirect or
	// refuse a request whose agent they do not know, and GNOME Boxes sends a
	// command-line tool's agent for exactly that reason; inua sends the same
	// one, which is the agent the document found to work.
	isoUserAgent = "Wget/1.0"

	// isoFreeSpaceMargin is the room an image needs beside itself: 512 MiB, or
	// a twentieth of the image when that is more. A transfer that fills the
	// filesystem to its last byte leaves the rest of the session with nothing,
	// and the guest's own disk is written next to the image.
	isoFreeSpaceMargin = 512 << 20

	// isoProgressEvery is how often progress is reported: the window redraws a
	// bar, and a callback per read would cost more than the download.
	isoProgressEvery = 250 * time.Millisecond

	// isoReadBuffer is the size of one read from the body: large enough that
	// the syscalls do not matter, small enough that a cancellation is noticed
	// at once.
	isoReadBuffer = 64 << 10

	// isoProbeTimeout is how long a small side request may take: the HEAD that
	// asks an unknown size, and the fetch of a checksum list beside the image.
	// A mirror that does not answer either must not hold up the image itself.
	isoProbeTimeout = 20 * time.Second

	// isoChecksumLimit is how much of a checksum list is read. A large
	// distribution's is a few tens of kilobytes; a URL that answers with
	// something else must not be read into memory in full.
	isoChecksumLimit = 1 << 20
)

// ISOChecksum is how the distribution's published checksum of a download
// turned out. Most media have no checksum that can be fetched, and that is not
// a failure: the file is trusted because it came from the URL the catalog
// gave, over HTTPS, which is how Boxes trusts it.
type ISOChecksum string

const (
	// ISOChecksumVerified: a published SHA-256 for this image was fetched and
	// the file matches it.
	ISOChecksumVerified ISOChecksum = "verified"
	// ISOChecksumNotPublished: no published checksum could be fetched - a 404
	// beside the image, which is the usual answer, or a checksum list that
	// could not be read. The download is finished and the window says the
	// image was not verified.
	ISOChecksumNotPublished ISOChecksum = "not_published"
	// ISOChecksumNotChecked: the image was already in the cache, so nothing was
	// downloaded and nothing verified.
	ISOChecksumNotChecked ISOChecksum = "not_checked"
)

// ISODownloader streams installation images from their URLs into a cache.
//
// It holds one HTTP client - Boxes' own habit, and what keeps the mirrors'
// connections to one - and nothing else that outlives a download except what
// it has to remember between calls: which URLs are being downloaded right now,
// and how their checksums turned out.
type ISODownloader struct {
	// Dir is where the images go: DefaultISODir when empty. It is made, 0700,
	// on the first download.
	Dir string
	// Client is the HTTP client every request goes through. A plain one is
	// right; a proxy is set on it, not here.
	Client *http.Client
	// UserAgent is what requests say they are; isoUserAgent when empty.
	UserAgent string
	// FreeSpace says how many bytes are free on the filesystem holding a
	// directory, and is injectable so that a test does not have to fill a
	// disk to see a download refused. The real one is statfs. A probe that
	// fails does not block a download: the write itself reports a filesystem
	// with no room.
	FreeSpace func(dir string) (int64, error)
	// ProgressEvery is how often progress is reported, isoProgressEvery when
	// zero.
	ProgressEvery time.Duration

	mu        sync.Mutex
	inflight  map[string]*isoTransfer
	checksums map[string]ISOChecksum
}

// isoTransfer is one download of one URL that other callers wait for.
type isoTransfer struct {
	done chan struct{}
	path string
	err  error
}

// NewISODownloader builds a downloader for a cache directory. An empty
// directory means DefaultISODir, so the shell can pass what its configuration
// says and get the usual cache when it says nothing.
func NewISODownloader(dir string) *ISODownloader {
	if dir == "" {
		dir = DefaultISODir()
	}
	return &ISODownloader{
		Dir:           dir,
		Client:        &http.Client{},
		UserAgent:     isoUserAgent,
		FreeSpace:     localFreeSpace,
		ProgressEvery: isoProgressEvery,
	}
}

// DefaultISODir is where downloaded images are kept: $XDG_CACHE_HOME/inua/iso,
// or ~/.cache/inua/iso when XDG_CACHE_HOME is unset. It is a cache and may be
// deleted at any time; an image deleted from it is downloaded again.
func DefaultISODir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "inua", "iso")
}

// Download streams a medium's image into the cache and returns the path it is
// at. progress is called as the body arrives, with how much has been written
// and how large the file is - zero when the server sends no length; it is
// called at most a few times a second, and once more when the transfer ends.
// It may be nil.
//
// Cancelling ctx stops the download and Download returns the context's error;
// the `.part` file stays, so the next call resumes it. A transfer that fails
// leaves the `.part` file for the same reason, and one that arrives but does
// not match a published checksum removes it instead: those bytes are known
// bad. Nothing but a finished, verified image is ever left under the final
// name.
//
// There is no retry inside: a retry is the user's act, and the window's retry
// button is that act. A second Download for the same URL waits for the first
// and is given its answer, so the same file is never fetched twice at once.
func (d *ISODownloader) Download(ctx context.Context, media ISOMedia, progress func(done, total int64)) (string, error) {
	u, err := url.Parse(strings.TrimSpace(media.URL))
	if err != nil {
		return "", fmt.Errorf("%s is not a URL: %w", media.URL, err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return "", fmt.Errorf("%s: an install image is streamed from http or https, not %q", media.URL, u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%s: the URL names no host", media.URL)
	}
	if d.targetName(media) == "" {
		return "", fmt.Errorf("%s: the URL names no file", media.URL)
	}

	d.mu.Lock()
	if d.inflight == nil {
		d.inflight = map[string]*isoTransfer{}
	}
	if t, ok := d.inflight[media.URL]; ok {
		// Somebody else is downloading this file: wait for them rather than
		// fetch a second copy.
		d.mu.Unlock()
		select {
		case <-t.done:
			return t.path, t.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	t := &isoTransfer{done: make(chan struct{})}
	d.inflight[media.URL] = t
	d.mu.Unlock()

	file, err := d.transfer(ctx, media, progress)

	d.mu.Lock()
	delete(d.inflight, media.URL)
	d.mu.Unlock()
	t.path, t.err = file, err
	close(t.done)
	return file, err
}

// Checksum is how the last finished download of a URL turned out: verified
// against a published SHA-256, or not published. It is a separate call because
// Download answers with the path and the error, and "the distribution
// publishes no checksum" is neither - the window says it beside the file. A
// URL that has not been downloaded here has no answer, and neither has one
// whose download failed.
func (d *ISODownloader) Checksum(url string) (ISOChecksum, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.checksums[url]
	return c, ok
}

// TargetPath is where a medium's image is kept: the cache directory and the
// file name the URL itself carries, which is what the distribution calls it.
func (d *ISODownloader) TargetPath(media ISOMedia) string {
	return filepath.Join(d.dir(), d.targetName(media))
}

// CachedPath is a medium's image that is already in the cache, and whether
// there is one. An image that is there is finished and is not downloaded
// again, which is Boxes' own rule and what makes the download page usable
// twice; the window offers the file rather than the download.
func (d *ISODownloader) CachedPath(media ISOMedia) (string, bool) {
	return existingFile(d.TargetPath(media))
}

// PartialPath is the file an unfinished download is in, and whether there is
// one. The window words its button "Resume" when there is, because Download
// continues from it rather than starting again.
func (d *ISODownloader) PartialPath(media ISOMedia) (string, bool) {
	return existingFile(d.partPath(media))
}

// existingFile is a path that is a regular file with something in it. An empty
// file is not a finished image, and not something to resume either.
func existingFile(p string) (string, bool) {
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return "", false
	}
	return p, true
}

// transfer is one download, from the space check to the rename.
func (d *ISODownloader) transfer(ctx context.Context, media ISOMedia, progress func(done, total int64)) (string, error) {
	target := d.TargetPath(media)
	if _, ok := d.CachedPath(media); ok {
		d.setChecksum(media.URL, ISOChecksumNotChecked)
		return target, nil
	}
	if err := os.MkdirAll(d.dir(), 0700); err != nil {
		return "", fmt.Errorf("make the image cache %s: %w", d.dir(), err)
	}
	part := d.partPath(media)
	var resume int64
	if fi, err := os.Stat(part); err == nil && fi.Mode().IsRegular() {
		resume = fi.Size()
	}

	// The room the transfer still needs. The catalog's size is what the
	// database knows; when it knows none, the server is asked, and when the
	// server does not say either, the margin alone is the check.
	size := media.VolumeSize
	if size <= 0 {
		size = d.remoteSize(ctx, media)
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	remaining := size - resume
	if remaining < 0 {
		remaining = 0
	}
	if err := d.checkFreeSpace(remaining); err != nil {
		return "", err
	}

	img, err := d.openImage(ctx, media, resume)
	if err != nil {
		return "", err
	}
	defer img.body.Close()

	flags := os.O_CREATE | os.O_WRONLY
	if img.offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0600)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", part, err)
	}
	// The answer's validator is kept beside the part file before anything is
	// written, so that an attempt cut short here or later can ask the server
	// whether those bytes are still this file's beginning.
	d.writeValidator(media, img.header)
	_, err = copyISO(ctx, f, img.body, img.offset, img.total, progress, d.progressEvery())
	closeErr := f.Close()
	if err != nil {
		// The part file and its validator stay: that is what the next attempt
		// resumes from, and a retry is the caller's to make.
		return "", err
	}
	if closeErr != nil {
		return "", fmt.Errorf("write %s: %w", part, closeErr)
	}

	// Only now is the file the file. The checksum is checked while it still
	// has the temporary name, so a reader never finds a half image or a
	// corrupt one under the final name.
	outcome, err := d.verify(ctx, media, part)
	if err != nil {
		// Those bytes are known bad: there is nothing to resume from.
		_ = os.Remove(part)
		d.removeValidator(media)
		return "", err
	}
	if err := os.Rename(part, target); err != nil {
		return "", fmt.Errorf("move %s into place: %w", part, err)
	}
	d.removeValidator(media)
	d.setChecksum(media.URL, outcome)
	return target, nil
}

// isoBody is an answered request the download writes from: the body, where in
// the file its first byte belongs, the file's whole size when the server says
// one, and the headers the validator comes from.
type isoBody struct {
	body   io.ReadCloser
	offset int64
	total  int64
	header http.Header
}

// openImage asks for the image, from where the part file stops when there is
// one.
//
// An existing part file is resumed with a range request; the validator kept
// beside it goes in If-Range, so the server itself decides whether the bytes
// already written are still this file's beginning. A server that answers 206
// appends, one that answers 200 - it has no ranges, or the file changed under
// the validator - starts again from zero. A 416, a range past the end of a
// file that is now shorter, is the same thing: the part file is from another
// image under the same name, and the transfer starts again once.
func (d *ISODownloader) openImage(ctx context.Context, media ISOMedia, resume int64) (*isoBody, error) {
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, media.URL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", d.userAgent())
		if resume > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resume))
			if v := d.readValidator(media); v != "" {
				req.Header.Set("If-Range", v)
			}
		}
		resp, err := d.httpClient().Do(req)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return nil, cerr
			}
			return nil, fmt.Errorf("fetch %s: %w", media.URL, err)
		}
		switch resp.StatusCode {
		case http.StatusOK:
			return &isoBody{body: resp.Body, offset: 0, total: resp.ContentLength, header: resp.Header}, nil
		case http.StatusPartialContent:
			if resume == 0 {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("%s answered 206 to a request for the whole file", media.URL)
			}
			return &isoBody{body: resp.Body, offset: resume, total: totalFrom(resume, resp), header: resp.Header}, nil
		case http.StatusRequestedRangeNotSatisfiable:
			_ = resp.Body.Close()
			if resume == 0 {
				return nil, fmt.Errorf("%s answered %s", media.URL, resp.Status)
			}
			_ = os.Remove(d.partPath(media))
			d.removeValidator(media)
			resume = 0
			continue
		default:
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%s answered %s", media.URL, resp.Status)
		}
	}
}

// totalFrom is the whole file's size as a partial answer tells it: the offset
// plus what the server is sending, or the number after the slash of
// Content-Range when it sends the rest with no length of its own.
func totalFrom(offset int64, resp *http.Response) int64 {
	if resp.ContentLength > 0 {
		return offset + resp.ContentLength
	}
	if _, after, ok := strings.Cut(resp.Header.Get("Content-Range"), "/"); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(after), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// copyISO streams a body into the file, reporting progress as it goes. A
// cancelled context stops it, and the context's own error is returned rather
// than the one the transport wraps the cancellation in.
func copyISO(ctx context.Context, dst *os.File, src io.Reader, offset, total int64, progress func(done, total int64), every time.Duration) (int64, error) {
	done := offset
	var last time.Time
	report := func(force bool) {
		if progress == nil {
			return
		}
		now := time.Now()
		if !force && now.Sub(last) < every {
			return
		}
		last = now
		progress(done, total)
	}
	report(true)
	buf := make([]byte, isoReadBuffer)
	for {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return done, fmt.Errorf("write the image: %w", werr)
			}
			done += int64(n)
			report(false)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if cerr := ctx.Err(); cerr != nil {
				return done, cerr
			}
			return done, fmt.Errorf("read the image: %w", rerr)
		}
	}
	report(true)
	return done, nil
}

// checkFreeSpace refuses a download that cannot fit, before anything is
// written, and says how much is missing. size is what the transfer still has
// to write; the margin goes on top of it, so an image whose size is unknown
// still has to fit in the margin's room.
func (d *ISODownloader) checkFreeSpace(size int64) error {
	margin := size / 20
	if margin < isoFreeSpaceMargin {
		margin = isoFreeSpaceMargin
	}
	need := size + margin
	free, err := d.freeSpace(d.dir())
	if err != nil {
		return nil
	}
	if free >= need {
		return nil
	}
	return fmt.Errorf("not enough room in %s for the image: %s free, %s needed (%s short)", d.dir(), isoBytes(free), isoBytes(need), isoBytes(need-free))
}

// remoteSize asks the server how large the file is, for the space check of a
// medium whose size the catalog does not know. It is best effort: a server
// that does not answer a HEAD, or answers without a length, leaves the size
// unknown and the margin alone is what the check goes on.
func (d *ISODownloader) remoteSize(ctx context.Context, media ISOMedia) int64 {
	ctx, cancel := context.WithTimeout(ctx, isoProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, media.URL, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", d.userAgent())
	resp, err := d.httpClient().Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1))
	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 {
		return 0
	}
	return resp.ContentLength
}

// sha256Of is a file's SHA-256, read in a stream: an install image is
// gigabytes and is never held in memory.
func sha256Of(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", file, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read %s: %w", file, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verify checks a finished download against the distribution's published
// SHA-256, when there is one. The image is not an error without one, and the
// answer says which of the two happened; a file that is published and does not
// match is an error, and the caller removes it.
func (d *ISODownloader) verify(ctx context.Context, media ISOMedia, file string) (ISOChecksum, error) {
	want, published := d.publishedChecksum(ctx, media)
	if !published {
		return ISOChecksumNotPublished, nil
	}
	got, err := sha256Of(file)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(got, want) {
		return "", fmt.Errorf("the image downloaded from %s does not match the checksum published for it: the file is SHA-256 %s, the published one is %s", media.URL, got, want)
	}
	return ISOChecksumVerified, nil
}

// isoChecksumURLs is where a distribution's published checksums for an image
// are looked for, in order. The osinfo schema has no field for a digest - the
// document says why - so the image's URL is the only thing to go on and the
// rules are the distributions' published habits:
//
//   - beside the image, under the image's own name with "-CHECKSUM" appended,
//     which is Fedora's: Fedora-Server-netinst-x86_64-42-1.1-CHECKSUM;
//   - beside the image, in a file called SHA256SUMS, which is Debian's and
//     Ubuntu's.
//
// Both are a plain text file naming the images in the directory and their
// SHA-256. The first that answers 200 is used.
func isoChecksumURLs(imageURL string) []string {
	u, err := url.Parse(imageURL)
	if err != nil || u.Host == "" {
		return nil
	}
	name := path.Base(u.Path)
	if name == "" || name == "." || name == "/" {
		return nil
	}
	dir := *u
	dir.RawQuery, dir.Fragment = "", ""
	dir.Path = path.Dir(u.Path)
	dir.RawPath = ""
	beside := dir
	beside.Path = path.Join(dir.Path, name+"-CHECKSUM")
	sums := dir
	sums.Path = path.Join(dir.Path, "SHA256SUMS")
	return []string{beside.String(), sums.String()}
}

// publishedChecksum fetches the checksum list a distribution publishes for an
// image and returns the SHA-256 it names for it; the second value says whether
// one was found at all.
//
// Fetching it is best effort and its failures are not the download's: a 404 is
// the usual answer, because most media have no published checksum, and any
// other answer or a network failure is the same thing here - no checksum could
// be fetched, so none is verified, and the result says so.
func (d *ISODownloader) publishedChecksum(ctx context.Context, media ISOMedia) (string, bool) {
	name := d.targetName(media)
	if name == "" {
		return "", false
	}
	for _, candidate := range isoChecksumURLs(media.URL) {
		if sum, ok := d.fetchChecksums(ctx, candidate, name); ok {
			return sum, true
		}
	}
	return "", false
}

// fetchChecksums reads one checksum list and looks for one file in it.
func (d *ISODownloader) fetchChecksums(ctx context.Context, checksumURL, name string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, isoProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", d.userAgent())
	resp, err := d.httpClient().Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	text, err := io.ReadAll(io.LimitReader(resp.Body, isoChecksumLimit))
	if err != nil {
		return "", false
	}
	return checksumFor(string(text), name)
}

// checksumFor is the SHA-256 a checksum list names for one file. Two forms are
// read:
//
//	SHA256 (Fedora-Server-netinst-x86_64-42-1.1.iso) = 0123…   Fedora's
//	0123…  Fedora-Server-netinst-x86_64-42-1.1.iso             Debian's, Ubuntu's
//
// The second may have an asterisk before the name, which says the file was
// read in binary mode and means nothing here. A line that names some other
// image, a comment, and a hash that is not a SHA-256 are passed over.
func checksumFor(text, name string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		sum := ""
		for _, f := range fields {
			if isSHA256(f) {
				sum = strings.ToLower(f)
			}
		}
		if sum == "" {
			continue
		}
		file := ""
		if open := strings.Index(line, "("); open >= 0 {
			if close := strings.Index(line[open:], ")"); close > 0 {
				file = strings.TrimSpace(line[open+1 : open+close])
			}
		}
		if file == "" {
			file = strings.TrimPrefix(fields[len(fields)-1], "*")
		}
		if file == name || file == "./"+name {
			return sum, true
		}
	}
	return "", false
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// The validator of an unfinished download is kept beside the part file, in
// <part>.validator: one line, the ETag the server sent or its Last-Modified
// when it sent no ETag. A part file with no validator beside it is still
// resumed, with a range request and no If-Range, and it is then the server
// that decides whether to honour the range.
func (d *ISODownloader) validatorPath(media ISOMedia) string {
	return d.partPath(media) + ".validator"
}

func (d *ISODownloader) writeValidator(media ISOMedia, header http.Header) {
	v := header.Get("ETag")
	if v == "" {
		v = header.Get("Last-Modified")
	}
	if v == "" {
		d.removeValidator(media)
		return
	}
	_ = os.WriteFile(d.validatorPath(media), []byte(v+"\n"), 0600)
}

func (d *ISODownloader) readValidator(media ISOMedia) string {
	data, err := os.ReadFile(d.validatorPath(media))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (d *ISODownloader) removeValidator(media ISOMedia) {
	_ = os.Remove(d.validatorPath(media))
}

// partPath is the file a download streams into: the target's own name with
// ".part" appended, in the same directory, so that the rename into place is
// atomic.
func (d *ISODownloader) partPath(media ISOMedia) string {
	return d.TargetPath(media) + ".part"
}

// targetName is what an image is called: the last part of the URL's path,
// which is the name the distribution gave the file.
func (d *ISODownloader) targetName(media ISOMedia) string {
	u, err := url.Parse(strings.TrimSpace(media.URL))
	if err != nil {
		return ""
	}
	name := path.Base(u.Path)
	if name == "" || name == "." || name == "/" {
		return ""
	}
	return name
}

func (d *ISODownloader) dir() string {
	if d.Dir != "" {
		return d.Dir
	}
	return DefaultISODir()
}

func (d *ISODownloader) httpClient() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return http.DefaultClient
}

func (d *ISODownloader) userAgent() string {
	if d.UserAgent != "" {
		return d.UserAgent
	}
	return isoUserAgent
}

func (d *ISODownloader) progressEvery() time.Duration {
	if d.ProgressEvery > 0 {
		return d.ProgressEvery
	}
	return isoProgressEvery
}

func (d *ISODownloader) freeSpace(dir string) (int64, error) {
	if d.FreeSpace != nil {
		return d.FreeSpace(dir)
	}
	return localFreeSpace(dir)
}

func (d *ISODownloader) setChecksum(url string, outcome ISOChecksum) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.checksums == nil {
		d.checksums = map[string]ISOChecksum{}
	}
	d.checksums[url] = outcome
}

// localFreeSpace is the real free-space probe: how much an unprivileged writer
// could still put on the filesystem holding a directory.
func localFreeSpace(dir string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// isoBytes is a byte count as a message says it: gigabytes for an image,
// mebibytes for the room beside it.
func isoBytes(n int64) string {
	switch {
	case n >= 1<<40:
		return fmt.Sprintf("%.1f TiB", float64(n)/(1<<40))
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	default:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	}
}
