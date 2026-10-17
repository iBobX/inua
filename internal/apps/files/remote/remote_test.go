package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/keyring"
)

func TestParseReadsEachKindOfAddress(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Address
	}{
		{"sftp://ann@box:2222/srv/data/", Address{Scheme: "sftp", Kind: SFTP, User: "ann", Host: "box", Port: "2222", Path: "srv/data"}},
		{"smb://nas/media", Address{Scheme: "smb", Kind: SMB, Host: "nas", Path: "media"}},
		{"ftps://files.example.org", Address{Scheme: "ftps", Kind: FTP, Host: "files.example.org"}},
		{"davs://ann@cloud.example.org/remote.php/dav", Address{Scheme: "davs", Kind: WebDAV, User: "ann", Host: "cloud.example.org", Path: "remote.php/dav"}},
		{"s3://AKID@s3.eu-west-1.amazonaws.com/photos/2026", Address{Scheme: "s3", Kind: S3, User: "AKID", Host: "s3.eu-west-1.amazonaws.com", Path: "photos/2026"}},
		{" R2://KEY@0123abcd/backups ", Address{Scheme: "r2", Kind: R2, User: "KEY", Host: "0123abcd", Path: "backups"}},
	} {
		got, err := Parse(c.in)
		if err != nil || got != c.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
}

func TestParseRefusesWhatIsNotARemoteDisk(t *testing.T) {
	for in, want := range map[string]error{
		"sftp://ann:hunter2@box/": ErrPassword,
		"box/srv":                 ErrNoScheme,
		"smb://nas":               ErrNoShare,
		"sftp:///srv":             ErrNoServer,
		"sftp://box/a%0Ab":        ErrLineBreak,
	} {
		if _, err := Parse(in); !errors.Is(err, want) {
			t.Errorf("Parse(%q) = %v, want %v", in, err, want)
		}
	}
	var se *SchemeError
	if _, err := Parse("mtp://phone/"); !errors.As(err, &se) || se.Scheme != "mtp" {
		t.Errorf("mtp:// = %v", err)
	}
}

func TestAddressIsWrittenAsItIsRead(t *testing.T) {
	for _, in := range []string{"sftp://ann@box:2222/srv/data", "smb://nas/media", "r2://KEY@acct/backups", "ftp://host"} {
		a, err := Parse(in)
		if err != nil || a.String() != in {
			t.Errorf("%q came back as %q (%v)", in, a.String(), err)
		}
	}
	a, _ := Parse("smb://nas/media")
	if got := a.WithUser("ann").String(); got != "smb://ann@nas/media" {
		t.Errorf("with a user: %q", got)
	}
}

func TestNameIsTheShareOrTheBucketElseTheServer(t *testing.T) {
	for in, want := range map[string]string{
		"smb://nas/media/films":    "media",
		"s3://K@minio.lan/photos":  "photos",
		"r2://K@acct":              "acct",
		"sftp://ann@box/srv/data":  "box",
		"davs://cloud.example.org": "cloud.example.org",
	} {
		a, err := Parse(in)
		if err != nil || a.Name() != want {
			t.Errorf("%q is named %q (%v), want %q", in, a.Name(), err, want)
		}
	}
}

func TestNamesAreOneFolderAndNotTaken(t *testing.T) {
	for _, bad := range []string{"", " ", ".hidden", "a/b", strings.Repeat("x", 65)} {
		if CheckName(bad) == nil {
			t.Errorf("%q was taken as a name", bad)
		}
	}
	disks := []Disk{{Name: "nas"}, {Name: "nas 2"}}
	if got := FreeName("nas", disks); got != "nas 3" {
		t.Errorf("FreeName = %q", got)
	}
	if got := FreeName("box", disks); got != "box" {
		t.Errorf("FreeName = %q", got)
	}
	disks[0].Address = "sftp://ann@nas/srv"
	if d, ok := Find(disks, " sftp://ann@nas/srv/ "); !ok || d.Name != "nas" {
		t.Errorf("Find = %+v, %v", d, ok)
	}
	if _, ok := Find(disks, "sftp://bob@nas/srv"); ok {
		t.Error("another user's address was found")
	}
}

func TestUnitNameIsEscapedAsSystemdEscapeDoes(t *testing.T) {
	for in, want := range map[string]string{
		"nas":    "inua-remote-nas.service",
		"my nas": `inua-remote-my\x20nas.service`,
		"a-b.c":  `inua-remote-a\x2db.c.service`,
		"写真":     `inua-remote-\xe5\x86\x99\xe7\x9c\x9f.service`,
	} {
		if got := unitName(in); got != want {
			t.Errorf("unitName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigSaysWhatEachKindNeeds(t *testing.T) {
	conf := func(addr, secret string) string {
		a, err := Parse(addr)
		if err != nil {
			t.Fatal(err)
		}
		return string(config(a, secret, "/home/ann"))
	}
	for _, c := range []struct {
		addr, secret string
		want         []string
		not          []string
	}{
		{"sftp://ann@box:2222/srv", "OBSCURED", []string{"type = sftp", "host = box", "user = ann", "port = 2222", "pass = OBSCURED", "known_hosts_file = /home/ann/.ssh/known_hosts"}, nil},
		{"sftp://box/srv", "", []string{"type = sftp"}, []string{"pass", "user"}},
		{"ftp://host", "", []string{"user = anonymous"}, []string{"tls"}},
		{"ftps://ann@host", "X", []string{"user = ann", "tls = true", "pass = X"}, nil},
		{"davs://ann@cloud.org:8443/dav", "X", []string{"type = webdav", "url = https://cloud.org:8443/dav", "vendor = other"}, nil},
		{"dav://host/dav", "", []string{"url = http://host/dav"}, nil},
		{"s3://AKID@s3.eu-west-1.amazonaws.com/b", "SECRET", []string{"provider = AWS", "region = eu-west-1", "access_key_id = AKID", "secret_access_key = SECRET", "env_auth = false"}, []string{"endpoint"}},
		{"s3://AKID@s3.amazonaws.com/b", "S", []string{"region = us-east-1"}, nil},
		{"s3://AKID@minio.lan:9000/b", "S", []string{"provider = Other", "endpoint = https://minio.lan:9000", "no_check_bucket = true"}, nil},
		{"r2://KEY@0123abcd/backups", "S", []string{"provider = Cloudflare", "endpoint = https://0123abcd.r2.cloudflarestorage.com", "region = auto", "secret_access_key = S"}, nil},
	} {
		got := conf(c.addr, c.secret)
		if !strings.HasPrefix(got, "[disk]\n") {
			t.Errorf("%s: no section:\n%s", c.addr, got)
		}
		for _, w := range c.want {
			if !strings.Contains(got, w+"\n") {
				t.Errorf("%s: no %q in\n%s", c.addr, w, got)
			}
		}
		for _, n := range c.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: %q in\n%s", c.addr, n, got)
			}
		}
	}
	for addr, want := range map[string]string{"smb://nas/media/films": "disk:media/films", "davs://h/dav": "disk:", "r2://K@a/b": "disk:b"} {
		a, _ := Parse(addr)
		if got := target(a); got != want {
			t.Errorf("target(%s) = %q, want %q", addr, got, want)
		}
	}
}

func TestClassifyReadsWhyRcloneFailed(t *testing.T) {
	for log, want := range map[string]Reason{
		"Failed to create file system: NewFs: couldn't connect SSH: ssh: handshake failed: knownhosts: key is unknown": HostKey,
		"couldn't connect SSH: ssh: handshake failed: ssh: unable to authenticate, attempted methods [none]":           Refused,
		"failed to authenticate: response error: STATUS_LOGON_FAILURE":                                                 Refused,
		"InvalidAccessKeyId: The AWS Access Key Id you provided does not exist":                                        Refused,
		"dial tcp: lookup nas.lan: no such host":                                                                       Unreachable,
		"NoSuchBucket: The specified bucket does not exist":                                                            NotFound,
		`didn't find backend called "smb"`:                                                                             Old,
		"something else":                                                                                               Other,
	} {
		if got := Classify(log); got != want {
			t.Errorf("Classify(%q) = %v, want %v", log, got, want)
		}
	}
}

// fake is a session: programs that answer as rclone, systemd and FUSE
// would, a mount table, and what was run.
type fake struct {
	t       *testing.T
	e       *Engine
	mounts  []string
	calls   [][]string
	stdins  []string
	conf    string // the config file's text when rclone was started
	probe   error
	failRun bool // the unit fails at once
	log     string
	missing string
}

func newFake(t *testing.T) *fake {
	root := t.TempDir()
	f := &fake{t: t}
	f.e = &Engine{
		Home: filepath.Join(root, "home"), Runtime: filepath.Join(root, "run"), Cache: filepath.Join(root, "cache"),
		Dir: filepath.Join(root, "home", "Remote"), Poll: time.Millisecond,
		Mounts: func() ([]string, error) { return f.mounts, nil },
		LookPath: func(bin string) (string, error) {
			if bin == f.missing {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + bin, nil
		},
		Getenv: func(k string) string {
			return map[string]string{"SSH_AUTH_SOCK": "/run/user/1000/ssh-agent", "PATH": "/usr/bin"}[k]
		},
		Probe: func(ctx context.Context, name string, args ...string) error {
			f.calls = append(f.calls, append([]string{name}, args...))
			return f.probe
		},
		Run: func(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
			f.calls = append(f.calls, append([]string{name}, args...))
			f.stdins = append(f.stdins, stdin)
			switch {
			case strings.HasSuffix(name, "rclone") && args[0] == "obscure":
				return []byte("obscured(" + stdin + ")\n"), nil
			case name == "systemd-run":
				conf := args[slices.Index(args, "--config")+1]
				data, err := os.ReadFile(conf)
				if err != nil {
					t.Fatalf("no config when rclone starts: %v", err)
				}
				if st, _ := os.Stat(conf); st.Mode().Perm() != 0600 {
					t.Errorf("the config is %v", st.Mode().Perm())
				}
				f.conf = string(data)
				if f.failRun {
					logf := args[slices.Index(args, "--log-file")+1]
					os.WriteFile(logf, []byte(f.log), 0600)
					return nil, nil
				}
				point := args[slices.Index(args, "mount")+2]
				f.mounts = append(f.mounts, point)
			case name == "systemctl" && args[1] == "is-active":
				if f.failRun {
					return []byte("failed\n"), errors.New("exit status 3")
				}
				return []byte("active\n"), nil
			case name == "systemctl" && args[1] == "stop":
				f.mounts = nil
			}
			return nil, nil
		},
	}
	return f
}

func (f *fake) ran(name string) []string {
	for _, c := range f.calls {
		if c[0] == name || strings.HasSuffix(c[0], "/"+name) {
			return c
		}
	}
	return nil
}

func TestConnectKeepsTheSecretOffTheCommandLine(t *testing.T) {
	f := newFake(t)
	d := Disk{Name: "box", Address: "sftp://ann@box/srv"}
	if err := f.e.Connect(t.Context(), d, "hunter2"); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), "hunter2") {
			t.Errorf("the password is in %q", c)
		}
	}
	if f.stdins[0] != "hunter2" || f.calls[0][1] != "obscure" {
		t.Errorf("obscure was run as %q with %q", f.calls[0], f.stdins[0])
	}
	if !strings.Contains(f.conf, "pass = obscured(hunter2)\n") {
		t.Errorf("config:\n%s", f.conf)
	}
	if es, _ := os.ReadDir(f.e.Runtime); len(es) != 0 {
		t.Errorf("the config outlived the start: %v", es)
	}
	run := f.ran("systemd-run")
	for _, w := range []string{"--user", "--unit=inua-remote-box.service", "--setenv=SSH_AUTH_SOCK=/run/user/1000/ssh-agent", "disk:srv", f.e.Point(d), "--vfs-cache-mode"} {
		if !slices.Contains(run, w) {
			t.Errorf("no %q in %q", w, run)
		}
	}
	if !f.e.Mounted(d) {
		t.Error("not mounted")
	}
}

func TestConnectTakesS3sSecretAsItIs(t *testing.T) {
	f := newFake(t)
	if err := f.e.Connect(t.Context(), Disk{Name: "backups", Address: "r2://KEY@acct/backups"}, "SECRET"); err != nil {
		t.Fatal(err)
	}
	if c := f.ran("rclone"); c != nil && c[1] == "obscure" {
		t.Error("R2's secret was obscured")
	}
	if !strings.Contains(f.conf, "secret_access_key = SECRET\n") {
		t.Errorf("config:\n%s", f.conf)
	}
}

func TestConnectSaysWhyTheAddressWasRefusedBeforeMounting(t *testing.T) {
	f := newFake(t)
	f.probe = &Error{Reason: Refused, Detail: "unable to authenticate"}
	err := f.e.Connect(t.Context(), Disk{Name: "box", Address: "sftp://ann@box/"}, "")
	if e, ok := AsError(err); !ok || e.Reason != Refused {
		t.Fatalf("err = %v", err)
	}
	if f.ran("systemd-run") != nil {
		t.Error("rclone was started for an address that refused")
	}
	if _, err := os.Stat(f.e.Dir + "/box"); !os.IsNotExist(err) {
		t.Errorf("the folder made for it stayed: %v", err)
	}
}

func TestConnectReadsWhyTheMountStopped(t *testing.T) {
	f := newFake(t)
	f.failRun = true
	f.log = "2026/10/17 NOTICE: starting\n2026/10/17 ERROR : knownhosts: key mismatch\n"
	err := f.e.Connect(t.Context(), Disk{Name: "box", Address: "sftp://box/"}, "")
	e, ok := AsError(err)
	if !ok || e.Reason != HostKey || !strings.Contains(e.Detail, "key mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func TestConnectNamesWhatIsNotInstalled(t *testing.T) {
	f := newFake(t)
	f.missing = "fusermount3"
	err := f.e.Connect(t.Context(), Disk{Name: "box", Address: "sftp://box/"}, "")
	if e, ok := AsError(err); !ok || e.Reason != Missing || e.Detail != "fuse3" {
		t.Fatalf("err = %v", err)
	}
}

func TestConnectLeavesAFolderWithFilesInItAlone(t *testing.T) {
	f := newFake(t)
	d := Disk{Name: "box", Address: "sftp://box/"}
	os.MkdirAll(f.e.Point(d), 0700)
	os.WriteFile(filepath.Join(f.e.Point(d), "mine.txt"), nil, 0600)
	if e, ok := AsError(f.e.Connect(t.Context(), d, "")); !ok || e.Reason != Occupied {
		t.Fatalf("err = %v", e)
	}
	if f.ran("systemd-run") != nil {
		t.Error("mounted over the user's files")
	}
}

func TestDisconnectStopsTheUnitAndTakesTheEmptyFolder(t *testing.T) {
	f := newFake(t)
	d := Disk{Name: "box", Address: "sftp://box/"}
	if err := f.e.Connect(t.Context(), d, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.e.Disconnect(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	if c := f.ran("systemctl"); c == nil || !slices.Contains(c, "inua-remote-box.service") {
		t.Errorf("systemctl: %q", c)
	}
	if _, err := os.Stat(f.e.Point(d)); !os.IsNotExist(err) {
		t.Errorf("the folder stayed: %v", err)
	}
	if f.ran("fusermount3") != nil {
		t.Error("fusermount3 for a mount the unit took away")
	}
}

func TestDisconnectTakesAwayAMountLeftBehind(t *testing.T) {
	f := newFake(t)
	d := Disk{Name: "box", Address: "sftp://box/"}
	f.mounts = []string{f.e.Point(d)}
	run := f.e.Run
	f.e.Run = func(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
		if name == "systemctl" {
			f.calls = append(f.calls, append([]string{name}, args...))
			return nil, errors.New("Unit inua-remote-box.service not loaded.")
		}
		return run(ctx, stdin, name, args...)
	}
	f.e.Disconnect(t.Context(), d)
	if c := f.ran("fusermount3"); !slices.Equal(c, []string{"fusermount3", "-u", f.e.Point(d)}) {
		t.Errorf("fusermount3: %q", c)
	}
}

func TestTheCacheIsTheAddresssNotTheNames(t *testing.T) {
	f := newFake(t)
	nas := Disk{Name: "nas", Address: "smb://nas/media"}
	other := Disk{Name: "nas", Address: "smb://other/media"}
	if f.e.cacheDir(nas) == f.e.cacheDir(other) {
		t.Error("two servers share a cache")
	}
	if !strings.HasPrefix(f.e.cacheDir(nas), f.e.Cache+"/") {
		t.Errorf("cache = %s", f.e.cacheDir(nas))
	}
	if err := f.e.Connect(t.Context(), nas, ""); err != nil {
		t.Fatal(err)
	}
	if run := f.ran("systemd-run"); !slices.Contains(run, f.e.cacheDir(nas)) {
		t.Errorf("no cache dir in %q", run)
	}
	if e, ok := AsError(f.e.Forget(nas)); !ok || e.Reason != Busy {
		t.Errorf("forgot a mounted disk: %v", e)
	}
}

// TestSecretsAreKeptByAddress keeps a secret in a Secret Service
// (inua-keyring's, on a private bus) and finds it by the disk's address.
func TestSecretsAreKeptByAddress(t *testing.T) {
	addr := dbustest.Start(t)
	store, err := keyring.Open(filepath.Join(t.TempDir(), "index.json"), keyring.NewMemoryBackend())
	if err != nil {
		t.Fatal(err)
	}
	go keyring.NewService(dbustest.Conn(t, addr), store).Serve(t.Context())
	s := Secrets{Dial: func() (*dbus.Conn, error) { return dbus.Connect(addr) }}
	for range 100 {
		if s.Available() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	d := Disk{Name: "backups", Address: "r2://KEY@acct/backups"}
	if err := s.Store(t.Context(), d, "SECRET"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Lookup(t.Context(), d.Address); err != nil || !ok || v != "SECRET" {
		t.Fatalf("Lookup = %q, %v, %v", v, ok, err)
	}
	if _, ok, _ := s.Lookup(t.Context(), "r2://OTHER@acct/backups"); ok {
		t.Error("another address's secret")
	}
	if err := s.Delete(d.Address); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Lookup(t.Context(), d.Address); ok {
		t.Error("deleted, and still there")
	}
}

// TestProbeStopsAtTheFirstName runs stand-ins for rclone: one that lists
// forever (a long bucket), one that lists nothing (an empty folder), and
// one that is refused.
func TestProbeStopsAtTheFirstName(t *testing.T) {
	dir := t.TempDir()
	script := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := probe(ctx, script("long", "while :; do echo name; done")); err != nil {
		t.Errorf("a long listing: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("the long listing was not stopped")
	}
	if err := probe(ctx, script("empty", "exit 0")); err != nil {
		t.Errorf("an empty folder: %v", err)
	}
	err := probe(ctx, script("refused", `echo "NOTICE: x" >&2; echo "Failed to create file system: STATUS_LOGON_FAILURE" >&2; exit 1`))
	if e, ok := AsError(err); !ok || e.Reason != Refused || !strings.Contains(e.Detail, "STATUS_LOGON_FAILURE") {
		t.Errorf("refused: %v", err)
	}
}

func TestTheSetupsFieldsMakeTheAddress(t *testing.T) {
	for _, c := range []struct {
		f    Form
		want string
	}{
		{Form{Kind: SFTP, Server: "box", User: "ann", Folder: "/srv/data/"}, "sftp://ann@box/srv/data"},
		{Form{Kind: SFTP, Server: "box:2222"}, "sftp://box:2222"},
		{Form{Kind: SMB, Server: "nas", Share: "media", Folder: "films"}, "smb://nas/media/films"},
		{Form{Kind: WebDAV, URL: "https://cloud.example.org/remote.php/dav/files/ann/", User: "ann"}, "davs://ann@cloud.example.org/remote.php/dav/files/ann"},
		{Form{Kind: WebDAV, URL: "http://nas:8080/dav"}, "dav://nas:8080/dav"},
		{Form{Kind: FTP, Server: "files.org", TLS: true}, "ftps://files.org"},
		{Form{Kind: S3, Server: "https://s3.eu-west-1.amazonaws.com/", User: "AKID", Bucket: "photos", Folder: "2026"}, "s3://AKID@s3.eu-west-1.amazonaws.com/photos/2026"},
		{Form{Kind: R2, Server: "0123abcd", User: "KEY", Bucket: "backups"}, "r2://KEY@0123abcd/backups"},
	} {
		a, err := c.f.Address()
		if err != nil || a.String() != c.want {
			t.Errorf("%+v = %q, %v; want %q", c.f, a.String(), err, c.want)
			continue
		}
		if back, _ := FormOf(a).Address(); back != a {
			t.Errorf("%q came back from its fields as %q", c.want, back.String())
		}
	}
	for _, c := range []struct {
		f    Form
		want error
	}{
		{Form{Kind: SMB, Server: "nas"}, ErrNoShare},
		{Form{Kind: R2, Server: "acct", User: "K"}, ErrNoBucket},
		{Form{Kind: WebDAV, URL: "cloud.example.org"}, ErrNoURL},
		{Form{Kind: SFTP, Server: "box", Port: "99999"}, ErrPort},
		{Form{Kind: SFTP}, ErrNoServer},
	} {
		if _, err := c.f.Address(); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, want %v", c.f, err, c.want)
		}
	}
}

func TestTestListsAndMountsNothing(t *testing.T) {
	f := newFake(t)
	d := Disk{Name: "backups", Address: "r2://KEY@acct/backups"}
	if err := f.e.Test(t.Context(), d, "SECRET"); err != nil {
		t.Fatal(err)
	}
	if c := f.ran("rclone"); c == nil || c[1] != "lsf" || c[len(c)-1] != "disk:backups" {
		t.Errorf("listed with %q", c)
	}
	if f.ran("systemd-run") != nil || f.e.Mounted(d) {
		t.Error("a test mounted it")
	}
	if es, _ := os.ReadDir(f.e.Runtime); len(es) != 0 {
		t.Errorf("the config outlived the test: %v", es)
	}
	if _, err := os.Stat(f.e.Point(d)); !os.IsNotExist(err) {
		t.Errorf("a test made the disk's folder: %v", err)
	}
	f.probe = &Error{Reason: Refused}
	if e, ok := AsError(f.e.Test(t.Context(), d, "WRONG")); !ok || e.Reason != Refused {
		t.Errorf("a refused test = %v", e)
	}
}
